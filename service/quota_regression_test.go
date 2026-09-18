package service

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/heqiuyu/heqiuyu-api/model"
	relaycommon "github.com/heqiuyu/heqiuyu-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestUnlimitedTokenReservationAndSettlement(t *testing.T) {
	for _, actualQuota := range []int{0, 50, 100, 150} {
		t.Run(fmt.Sprint(actualQuota), func(t *testing.T) {
			truncate(t)
			seedUser(t, 1, 1000)
			seedToken(t, 1, 1, "unlimited-billing", 0)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Update("unlimited_quota", true).Error)
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "unlimited-billing", TokenUnlimited: true, ForcePreConsume: true}
			session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 1}}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			require.Nil(t, session.preConsume(ctx, 100))
			require.Equal(t, -100, getTokenRemainQuota(t, 1))
			require.Equal(t, 100, getTokenUsedQuota(t, 1))
			require.NoError(t, session.Settle(actualQuota))
			require.Equal(t, -actualQuota, getTokenRemainQuota(t, 1))
			require.Equal(t, actualQuota, getTokenUsedQuota(t, 1))
			require.Equal(t, 1000-actualQuota, getUserQuota(t, 1))
		})
	}
}

func TestUnlimitedTokenRollsBackWhenWalletReservationFails(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 50)
	seedToken(t, 1, 1, "unlimited-rollback", 0)
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "unlimited-rollback", TokenUnlimited: true, ForcePreConsume: true}
	session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 1}}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NotNil(t, session.preConsume(ctx, 100))
	require.Equal(t, 0, getTokenRemainQuota(t, 1))
	require.Equal(t, 0, getTokenUsedQuota(t, 1))
	require.Equal(t, 50, getUserQuota(t, 1))
}

func TestWalletAdditionalReservationCannotOverdraw(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 50)
	seedToken(t, 1, 1, "reserve-wallet", 500)
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "reserve-wallet", ForcePreConsume: true}
	session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 1}}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Nil(t, session.preConsume(ctx, 40))
	require.Error(t, session.Reserve(100))
	require.Equal(t, 10, getUserQuota(t, 1))
	require.Equal(t, 460, getTokenRemainQuota(t, 1))
	require.Equal(t, 40, session.GetPreConsumedQuota())
	require.NoError(t, session.Settle(40))
}
