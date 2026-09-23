package model

import (
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestIncrementFailedAttemptsLocksAtConfiguredThreshold(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TwoFA{}))

	twoFA := &TwoFA{UserId: 1, Secret: "test-secret", IsEnabled: true}
	require.NoError(t, db.Create(twoFA).Error)

	for attempt := 1; attempt < common.MaxFailAttempts; attempt++ {
		require.NoError(t, incrementFailedAttemptsUpdate(db, twoFA.Id, time.Now().Add(time.Minute)).Error)

		var stored TwoFA
		require.NoError(t, db.First(&stored, twoFA.Id).Error)
		require.Equal(t, attempt, stored.FailedAttempts)
		require.Nil(t, stored.LockedUntil, "attempt %d must not lock before the configured threshold", attempt)
	}

	require.NoError(t, incrementFailedAttemptsUpdate(db, twoFA.Id, time.Now().Add(time.Minute)).Error)
	var stored TwoFA
	require.NoError(t, db.First(&stored, twoFA.Id).Error)
	require.Equal(t, common.MaxFailAttempts, stored.FailedAttempts)
	require.NotNil(t, stored.LockedUntil)
}

func TestTwoFAUpdateDoesNotRecreateHardDeletedRecord(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TwoFA{}))

	twoFA := &TwoFA{UserId: 9_000_001, Secret: "deleted-secret", IsEnabled: true}
	require.NoError(t, DB.Create(twoFA).Error)
	t.Cleanup(func() {
		DB.Unscoped().Where("id = ?", twoFA.Id).Delete(&TwoFA{})
	})

	require.NoError(t, DB.Unscoped().Delete(twoFA).Error)
	twoFA.FailedAttempts = 1
	require.NoError(t, twoFA.Update())

	var count int64
	require.NoError(t, DB.Unscoped().Model(&TwoFA{}).Where("id = ?", twoFA.Id).Count(&count).Error)
	require.Zero(t, count)
}

func TestIncrementFailedAttemptsUsesMySQLCurrentValueSemantics(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(localhost:3306)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)

	result := incrementFailedAttemptsUpdate(db, 1, time.Unix(1_700_000_000, 0))
	require.NoError(t, result.Error)
	sql := result.Statement.SQL.String()

	require.True(t, strings.Contains(sql, "`failed_attempts`=failed_attempts + 1"), sql)
	require.True(t, strings.Contains(sql, "`locked_until`=CASE WHEN failed_attempts >= ?"), sql)
	require.False(t, strings.Contains(sql, "CASE WHEN failed_attempts + 1 >= ?"), sql)
}
