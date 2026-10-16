package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// legacyUserWithoutAuthVersion 模拟升级前的 users 表：没有 auth_version 列。
// 真实事故：model.User 曾经也没有 AuthVersion 字段，于是 AutoMigrate 永远建不出
// 这一列，而 Edit/UpdateProfile/ResetUserPassword 都会写 `auth_version + 1`，
// 导致「管理员改别人密码」「用户自己改密码」整体失败（SQL logic error:
// no such column: auth_version）。本测试锁定该回归。
type legacyUserWithoutAuthVersion struct {
	Id          int    `gorm:"primaryKey"`
	Username    string `gorm:"unique"`
	Password    string `gorm:"not null"`
	Role        int
	Status      int
	Group       string
	AccessToken *string `gorm:"type:char(32);uniqueIndex"`
	AffCode     string  `gorm:"type:varchar(32);uniqueIndex"`
	CreatedAt   int64
}

func (legacyUserWithoutAuthVersion) TableName() string { return "users" }

func TestAutoMigrateAddsAuthVersionColumnForLegacyUsersTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 1) 造一张"旧表"：确实没有 auth_version
	require.NoError(t, db.Migrator().CreateTable(&legacyUserWithoutAuthVersion{}))
	require.False(t,
		db.Migrator().HasColumn(&legacyUserWithoutAuthVersion{}, "auth_version"),
		"前置条件：旧表不应有 auth_version")
	require.NoError(t, db.Exec(
		`INSERT INTO users (id, username, password, role, status, "group", created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, "root", "hashed", 100, 1, "default", 0,
	).Error)

	// 2) 跑真实迁移：应当补出 auth_version 列
	require.NoError(t, db.AutoMigrate(&User{}))
	require.True(t,
		db.Migrator().HasColumn(&User{}, "auth_version"),
		"AutoMigrate 必须为旧库补出 users.auth_version 列")

	// 3) 自增表达式必须可用（改密的实际 SQL 形态）
	require.NoError(t, db.Model(&User{}).Where("id = ?", 1).
		Updates(map[string]interface{}{"auth_version": gorm.Expr("auth_version + 1")}).Error)

	var stored User
	require.NoError(t, db.Where("id = ?", 1).First(&stored).Error)
	require.Equal(t, 1, stored.AuthVersion, "auth_version 应自增为 1")

	// 4) 读取会话校验用的版本号也不该再报错（GetUserAuthVersion 走包级 DB）
	prevDB := DB
	DB = db
	t.Cleanup(func() { DB = prevDB })
	version, err := GetUserAuthVersion(1)
	require.NoError(t, err)
	require.Equal(t, 1, version)
}
