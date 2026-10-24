package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// t30InviteTestDB 准备一个只含 users 表的内存 SQLite，并返回邀请人用户。
func t30InviteTestDB(t *testing.T) (*gorm.DB, *model.User) {
	t.Helper()

	prevDB, prevLogDB := model.DB, model.LOG_DB
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = prevDB, prevLogDB
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}))

	inviter := &model.User{
		Username: "t30-inviter",
		Password: "x",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "AbC123",
	}
	require.NoError(t, db.Create(inviter).Error)
	return db, inviter
}

// TestValidateInviteCode 覆盖 D1：邀请码只 trim、精确匹配且区分大小写。
func TestValidateInviteCode(t *testing.T) {
	_, inviter := t30InviteTestDB(t)

	cases := []struct {
		name      string
		raw       string
		wantID    int
		wantErrIs error
	}{
		{name: "exact match", raw: "AbC123", wantID: inviter.Id},
		{name: "trim works", raw: "  AbC123\t", wantID: inviter.Id},
		{name: "lowercase invalid", raw: "abc123", wantErrIs: ErrInviteCodeInvalid},
		{name: "uppercase invalid", raw: "ABC123", wantErrIs: ErrInviteCodeInvalid},
		{name: "unknown invalid", raw: "NoSuchCode", wantErrIs: ErrInviteCodeInvalid},
		{name: "empty required", raw: "   ", wantErrIs: ErrInviteCodeRequired},
		{name: "no code required", raw: "", wantErrIs: ErrInviteCodeRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ValidateInviteCode(tc.raw)
			if tc.wantErrIs != nil {
				require.ErrorIs(t, err, tc.wantErrIs)
				require.Zero(t, id)
				// 错误信息不得包含邀请码明文。
				require.False(t, strings.Contains(err.Error(), strings.TrimSpace(tc.raw)) && strings.TrimSpace(tc.raw) != "")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantID, id)
		})
	}
}

// TestValidateInviteCodeWithoutDB 覆盖 DB 未初始化时的安全失败（invalid，不 panic）。
func TestValidateInviteCodeWithoutDB(t *testing.T) {
	prevDB := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = prevDB })

	id, err := ValidateInviteCode("AbC123")
	require.ErrorIs(t, err, ErrInviteCodeInvalid)
	require.Zero(t, id)
}

// TestInviteCodeRequiredReadsSettingOnly 覆盖邀请码策略独立于 registration_setting.enabled。
func TestInviteCodeRequiredReadsSettingOnly(t *testing.T) {
	setting := operation_setting.GetRegistrationSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })

	setting.InviteCodeRequired = true
	setting.Enabled = false
	require.True(t, InviteCodeRequired(), "invite_code_required 不应受 enabled 影响")

	setting.InviteCodeRequired = false
	setting.Enabled = true
	require.False(t, InviteCodeRequired())
}

// TestValidateInviteCodeErrorTypeIsStable 锁定错误类型可被 controller 映射。
func TestValidateInviteCodeErrorTypeIsStable(t *testing.T) {
	_, _ = t30InviteTestDB(t)
	_, err := ValidateInviteCode("")
	require.True(t, errors.Is(err, ErrInviteCodeRequired))
	_, err = ValidateInviteCode("WRONG")
	require.True(t, errors.Is(err, ErrInviteCodeInvalid))
}
