package common

// GetTrustQuota 返回"信任额度"阈值：当账户余额高于该阈值时，请求可以跳过预扣费。
//
// 安全默认：TrustQuotaEnabled 为 false 时返回 0，调用方（service.BillingSession.shouldTrust）
// 会把 0 视为"永不信任"，即完全禁用该旁路。原因是该旁路在并发场景下允许同一账户
// 在结算前超额消费（N 个并发请求都读到"余额充足"，全部不预扣，最后一起结算）。
func GetTrustQuota() int {
	if !TrustQuotaEnabled {
		return 0
	}
	if TrustQuota > 0 {
		return TrustQuota
	}
	return int(10 * QuotaPerUnit)
}
