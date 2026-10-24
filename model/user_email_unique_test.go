package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 本文件覆盖 R2-BACKEND-2 的 DB 级 email 唯一索引（best-effort 迁移）。
//
// 产品实现：`EnsureUserEmailUniquenessBestEffort(db *gorm.DB) error`
//   - nil db → error，不 panic；
//   - 先按 LOWER(email) 分组检测重复（仅非空 email）：有重复 → SysLog + error + 跳过建索引、不删数据；
//   - SQLite/PostgreSQL：部分唯一索引 `WHERE email <> ''`（空 email 可多行共存）；
//   - 索引已存在时幂等返回 nil；建索引失败 → SysLog + error。
//
// 测试使用独立的内存 SQLite（不污染 model 包 TestMain 的共享 DB），
// 并固定单连接（`:memory:` 每条连接是独立数据库）。

// t23NewEmailIndexTestDB 创建一个只迁移 User 表的独立内存 SQLite。
func t23NewEmailIndexTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "open in-memory sqlite")

	sqlDB, err := db.DB()
	require.NoError(t, err, "get sql.DB")
	sqlDB.SetMaxOpenConns(1) // :memory: 每条连接独立库，必须固定单连接
	t.Cleanup(func() { _ = sqlDB.Close() })

	// 函数按 common.UsingMySQL 分支：测试固定走 SQLite/PG 部分唯一索引分支。
	prevSQLite, prevMySQL, prevPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	t.Cleanup(func() {
		common.UsingSQLite = prevSQLite
		common.UsingMySQL = prevMySQL
		common.UsingPostgreSQL = prevPostgres
	})

	require.NoError(t, db.AutoMigrate(&User{}))
	return db
}

func t23CreateUser(t *testing.T, db *gorm.DB, username, email, affCode string) *User {
	t.Helper()
	user := &User{
		Username: username,
		Password: "x",
		Email:    email,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  affCode,
	}
	require.NoError(t, db.Create(user).Error, "create user %s<%s>", username, email)
	return user
}

func t23EmailUniqueIndexExists(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_users_email_unique'",
	).Scan(&count).Error)
	return count > 0
}

func t23CountUsersByEmail(t *testing.T, db *gorm.DB, email string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&User{}).Where("email = ?", email).Count(&count).Error)
	return count
}

// TestEnsureUserEmailUniquenessBestEffortNilDB nil db 必须返回 error 且不 panic。
func TestEnsureUserEmailUniquenessBestEffortNilDB(t *testing.T) {
	require.NotPanics(t, func() {
		err := EnsureUserEmailUniquenessBestEffort(nil)
		require.Error(t, err)
	})
}

// TestEnsureUserEmailUniquenessBestEffortCreatesIndex 覆盖正常唯一数据：
// 首次调用成功建索引、二次幂等、空 email 可多行、完全重复 email 插入被 DB 拒绝。
func TestEnsureUserEmailUniquenessBestEffortCreatesIndex(t *testing.T) {
	db := t23NewEmailIndexTestDB(t)

	// 建索引前：唯一非空邮箱若干 + 多条空邮箱（空 email 不参与唯一约束）。
	t23CreateUser(t, db, "t23-u1", "alice@example.com", "T23A")
	t23CreateUser(t, db, "t23-u2", "", "T23B")
	t23CreateUser(t, db, "t23-u3", "", "T23C")
	require.False(t, t23EmailUniqueIndexExists(t, db), "建索引前不应存在 email 唯一索引")

	// 1) 首次调用：成功且索引存在。
	require.NoError(t, EnsureUserEmailUniquenessBestEffort(db))
	require.True(t, t23EmailUniqueIndexExists(t, db), "调用后应创建 idx_users_email_unique")

	// 2) 幂等：再次调用仍返回 nil。
	require.NoError(t, EnsureUserEmailUniquenessBestEffort(db))

	// 3) 索引存在后：更多空 email 仍可共存（部分唯一索引 WHERE email <> ''）。
	t23CreateUser(t, db, "t23-u4", "", "T23D")
	t23CreateUser(t, db, "t23-u5", "", "T23E")

	// 4) 完全重复的非空 email 被 DB 唯一索引拒绝。
	err := db.Create(&User{
		Username: "t23-dup", Password: "x", Email: "alice@example.com",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "T23F",
	}).Error
	require.Error(t, err, "重复的非空 email 插入应被唯一索引拒绝")
	require.True(t, strings.Contains(strings.ToLower(err.Error()), "unique") ||
		strings.Contains(strings.ToLower(err.Error()), "constraint"),
		"错误应来自唯一约束，实际: %v", err)

	// 5) 说明（按实现语义断言）：DB 索引在 SQLite 默认 BINARY 排序下大小写敏感，
	//    大小写变体可插入；大小写不敏感去重由应用层 LOWER(email) 查询保证。
	require.NoError(t, db.Create(&User{
		Username: "t23-case", Password: "x", Email: "Alice@Example.COM",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "T23G",
	}).Error, "DB 唯一索引大小写敏感；应用层 LOWER 查询负责大小写不敏感去重")
}

// TestEnsureUserEmailUniquenessBestEffortSkipsWhenDuplicatesExist 覆盖历史重复数据：
// 调用返回 error（不 panic）、不删数据、索引被跳过（不阻断启动的语义）。
func TestEnsureUserEmailUniquenessBestEffortSkipsWhenDuplicatesExist(t *testing.T) {
	t.Run("完全重复", func(t *testing.T) {
		db := t23NewEmailIndexTestDB(t)
		t23CreateUser(t, db, "t23-dup-a", "dup@example.com", "T23H")
		t23CreateUser(t, db, "t23-dup-b", "dup@example.com", "T23I")
		require.False(t, t23EmailUniqueIndexExists(t, db))

		var err error
		require.NotPanics(t, func() { err = EnsureUserEmailUniquenessBestEffort(db) })
		require.Error(t, err, "存在重复邮箱时应返回 error（best-effort 跳过）")
		require.False(t, t23EmailUniqueIndexExists(t, db), "存在重复数据时不应创建唯一索引")
		require.Equal(t, int64(2), t23CountUsersByEmail(t, db, "dup@example.com"), "不得删除历史重复数据")
	})

	t.Run("大小写变体重复", func(t *testing.T) {
		db := t23NewEmailIndexTestDB(t)
		t23CreateUser(t, db, "t23-dup-c", "Case@Example.com", "T23J")
		t23CreateUser(t, db, "t23-dup-d", "case@example.com", "T23K")

		var err error
		require.NotPanics(t, func() { err = EnsureUserEmailUniquenessBestEffort(db) })
		require.Error(t, err, "LOWER(email) 分组应识别大小写变体重复并返回 error")
		require.False(t, t23EmailUniqueIndexExists(t, db), "大小写变体重复时也不应创建唯一索引")
		require.Equal(t, int64(1), t23CountUsersByEmail(t, db, "Case@Example.com"))
		require.Equal(t, int64(1), t23CountUsersByEmail(t, db, "case@example.com"))
		require.Equal(t, int64(2), t23CountUsersByEmail(t, db, "case@example.com")+t23CountUsersByEmail(t, db, "Case@Example.com"))
	})
}
