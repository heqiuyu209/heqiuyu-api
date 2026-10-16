package model

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestMigrationSmokeCriticalWrites 是"空库 AutoMigrate + 关键写操作冒烟"测试。
//
// 动机：`users.auth_version` 那次事故（代码写列 / 模型未声明 / 迁移未建列）编译器查不出来，
// 上线后表现为"改密码整体失败"。这里把「全部模型能从空库迁移出来」和「那些用原始表达式
// 写列的关键路径能真正执行」串成一个冒烟测试，任何同类缺列都会在这里直接失败。
func TestMigrationSmokeCriticalWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	prevDB, prevLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	prevSQLite := common.UsingSQLite
	common.UsingSQLite = true
	t.Cleanup(func() {
		DB, LOG_DB = prevDB, prevLogDB
		common.UsingSQLite = prevSQLite
	})

	// 1) 全部模型必须能从空库迁移出来（与 model/main.go 的 migrateDB 列表一致）
	require.NoError(t, db.AutoMigrate(
		&Channel{}, &Token{}, &User{}, &PasskeyCredential{}, &Option{}, &Redemption{},
		&Ability{}, &Log{}, &Midjourney{}, &QuotaData{}, &Task{}, &Model{}, &Vendor{},
		&PrefillGroup{}, &Setup{}, &TwoFA{}, &TwoFABackupCode{}, &Checkin{},
		&SubscriptionOrder{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{},
		&CustomOAuthProvider{}, &UserOAuthBinding{},
	))

	// 2) 种一个用户 + 一个令牌 + 一条 2FA
	hashed, err := common.Password2Hash("initial-pass-123")
	require.NoError(t, err)
	user := &User{
		Username: "smoke-user", Password: hashed, Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", AffCode: "SMOK",
	}
	require.NoError(t, db.Create(user).Error)

	token := &Token{
		UserId: user.Id, Key: "smokekey", Name: "smoke", Status: common.TokenStatusEnabled,
		ExpiredTime: -1, RemainQuota: 1000, UnlimitedQuota: false,
	}
	require.NoError(t, db.Create(token).Error)

	twoFA := &TwoFA{UserId: user.Id, Secret: "smoke-secret", IsEnabled: true}
	require.NoError(t, db.Create(twoFA).Error)

	// 3) 关键写路径必须全部成功（这些路径都写原始列表达式）
	t.Run("改密写入 auth_version", func(t *testing.T) {
		updated := &User{Id: user.Id, Username: user.Username, DisplayName: user.Username}
		updated.Password = "new-pass-123456"
		require.NoError(t, updated.Edit(true), "管理员改密的实际 SQL（含 auth_version + 1）")

		profile := &User{Id: user.Id, Username: user.Username, DisplayName: user.Username}
		profile.Password = "self-pass-123456"
		require.NoError(t, profile.UpdateProfile(true), "用户自己改密的实际 SQL")

		var reloaded User
		require.NoError(t, db.Where("id = ?", user.Id).First(&reloaded).Error)
		require.Equal(t, 2, reloaded.AuthVersion, "两次改密应各自自增 auth_version")
	})

	t.Run("额度与用量增减", func(t *testing.T) {
		require.NoError(t, IncreaseUserQuota(user.Id, 500, true))
		require.NoError(t, DecreaseUserQuota(user.Id, 200, true))
		UpdateUserUsedQuotaAndRequestCount(user.Id, 100)
		_, err := DecreaseUserQuotaGuarded(user.Id, 1)
		require.NoError(t, err)

		require.NoError(t, IncreaseTokenQuota(token.Id, token.Key, 300))
		require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 100))
	})

	t.Run("邀请额度自增", func(t *testing.T) {
		require.NoError(t, inviteUser(user.Id))
	})

	t.Run("2FA 失败次数自增", func(t *testing.T) {
		require.NoError(t, incrementFailedAttemptsUpdate(db, twoFA.Id, time.Now().Add(time.Minute)).Error)
	})

	t.Run("管理端字段更新白名单", func(t *testing.T) {
		require.NoError(t, updateColumns(user.Id, map[string]interface{}{
			"status":       common.UserStatusEnabled,
			"group":        "default",
			"remark":       "smoke",
			"display_name": "smoke-user",
		}))
	})
}
