package model

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenQuotaReportsDatabaseFailure(t *testing.T) {
	truncateTables(t)
	token := &Token{Key: "quota-failure", RemainQuota: 500, UsedQuota: 100}
	require.NoError(t, DB.Create(token).Error)
	failure := errors.New("injected token update failure")
	const callback = "test:token_quota_failure"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(failure)
		}
	}))
	t.Cleanup(func() { DB.Callback().Update().Remove(callback) })

	require.ErrorIs(t, IncreaseTokenQuota(token.Id, token.Key, 20), failure)
	require.ErrorIs(t, DecreaseTokenQuota(token.Id, token.Key, 20), failure)
	var after Token
	require.NoError(t, DB.First(&after, token.Id).Error)
	require.Equal(t, 500, after.RemainQuota)
	require.Equal(t, 100, after.UsedQuota)
}

func TestUserListQueriesQuoteColumnsAndRestrictSensitiveFields(t *testing.T) {
	truncateTables(t)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		name := fmt.Sprintf("list_%d", role)
		secret := "secret_" + name
		require.NoError(t, DB.Create(&User{
			Username: name, Password: "password-hash", Role: role,
			AffCode: name, AccessToken: &secret, Group: "default", Quota: 500,
		}).Error)
	}
	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		wantCount := int64(1)
		if role == common.RoleRootUser {
			wantCount = 3
		}
		users, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 10}, role)
		require.NoError(t, err)
		require.Equal(t, wantCount, total)
		require.Len(t, users, int(wantCount))
		for _, user := range users {
			require.Empty(t, user.Password)
			require.Nil(t, user.AccessToken)
			require.Equal(t, "default", user.Group)
			require.Equal(t, 500, user.Quota)
		}
		users, total, err = SearchUsers("list_", "default", 0, 10, role)
		require.NoError(t, err)
		require.Equal(t, wantCount, total)
		require.Len(t, users, int(wantCount))
		for _, user := range users {
			require.Empty(t, user.Password)
			require.Nil(t, user.AccessToken)
		}
	}
}

func TestSubscriptionRefundIsAtomicAndIdempotent(t *testing.T) {
	truncateTables(t)
	sub := &UserSubscription{UserId: 1, AmountTotal: 1000, AmountUsed: 500, Status: "active"}
	require.NoError(t, DB.Create(sub).Error)
	record := &SubscriptionPreConsumeRecord{
		RequestId: "refund-atomic", UserId: 1, UserSubscriptionId: sub.Id,
		PreConsumed: 100, Status: "consumed",
	}
	require.NoError(t, DB.Create(record).Error)

	// A one-connection pool catches opening another transaction from the refund.
	originalDB := DB
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	DB = DB.WithContext(ctx)
	t.Cleanup(func() { DB = originalDB; cancel() })

	failure := errors.New("injected refund record failure")
	const callback = "test:subscription_refund_failure"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "subscription_pre_consume_records" {
			tx.AddError(failure)
		}
	}))
	t.Cleanup(func() { originalDB.Callback().Update().Remove(callback) })
	require.ErrorIs(t, RefundSubscriptionPreConsume(record.RequestId), failure)
	var after UserSubscription
	require.NoError(t, DB.First(&after, sub.Id).Error)
	require.Equal(t, int64(500), after.AmountUsed, "a failed refund must roll back its quota change")
	require.NoError(t, DB.First(record, record.Id).Error)
	require.Equal(t, "consumed", record.Status)

	require.NoError(t, DB.Callback().Update().Remove(callback))
	require.NoError(t, RefundSubscriptionPreConsume(record.RequestId))
	require.NoError(t, RefundSubscriptionPreConsume(record.RequestId))
	require.NoError(t, DB.First(&after, sub.Id).Error)
	require.Equal(t, int64(400), after.AmountUsed)
	require.NoError(t, DB.First(record, record.Id).Error)
	require.Equal(t, "refunded", record.Status)
}
