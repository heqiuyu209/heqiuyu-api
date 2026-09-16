package model

import (
	"testing"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/stretchr/testify/require"
)

// 回归测试（审计报告 H3）：部分更新绝不能把余额/状态/角色等列写回旧快照。
//
// 旧实现 model.User.Update() 会执行 newUser := *user; DB.First(&user, id); Updates(newUser)，
// 而 GORM 对结构体 Updates 会写入所有非零字段，因此调用方早先读到的 quota 会把并发发生的
// 计费扣减覆盖回去（一次设置更新即可“白嫖”一笔用量）。
func TestColumnScopedWritesDoNotClobberQuota(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "h3_test_user",
		Password: "x",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    1000,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)

	// 1) 模拟并发扣费：余额 1000 -> 400
	require.NoError(t, decreaseUserQuota(user.Id, 600))

	// 2) 调用方持有的是扣费之前的快照（quota=1000），用它执行设置更新
	stale := *user
	stale.Setting = `{"language":"zh"}`
	require.NoError(t, stale.UpdateSetting(stale.Setting))

	// 3) 余额必须保持 400，而不是被旧快照写回 1000
	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	require.Equal(t, 400, after.Quota, "UpdateSetting must not write back a stale quota snapshot")
	require.Equal(t, common.RoleCommonUser, after.Role)
	require.Equal(t, common.UserStatusEnabled, after.Status)
}

// UpdateAdminFields 只允许写入白名单列：配额类字段必须被忽略。
func TestUpdateAdminFieldsIsWhitelisted(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "admin_fields_user",
		Password: "x",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    5000,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, decreaseUserQuota(user.Id, 1000))

	// status 在白名单内应生效；quota / used_quota / request_count 必须被丢弃
	require.NoError(t, user.UpdateAdminFields(map[string]interface{}{
		"status":        common.UserStatusDisabled,
		"quota":         999999,
		"used_quota":    12345,
		"request_count": 7,
	}))

	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, after.Status, "status is whitelisted and should be applied")
	require.Equal(t, 4000, after.Quota, "quota must not be writable through UpdateAdminFields")
	require.Equal(t, 0, after.UsedQuota, "used_quota must not be writable through UpdateAdminFields")
	require.Equal(t, 0, after.RequestCount, "request_count must not be writable through UpdateAdminFields")
}
