package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/service"
)

const (
	// inviteCodeAttemptMaxFailures / inviteCodeAttemptWindowSeconds 是邀请码枚举防护：
	// 同一 IP 在 10 分钟内最多 20 次无效/缺失邀请码尝试，超过返回 429。
	// 硬编码（非配置项）：只用于注册发码用途，避免新增配置面。
	inviteCodeAttemptMaxFailures   = 20
	inviteCodeAttemptWindowSeconds = 600
	// inviteCodeAttemptMark 是失败计数 key 前缀。
	inviteCodeAttemptMark = "inviteAttempts"
)

var (
	inviteAttemptMu      sync.Mutex
	inviteAttemptLimiter *common.InMemoryRateLimiter
)

// inviteAttemptsLimiter 返回邀请码失败计数限流器（懒初始化、Init 幂等）。
func inviteAttemptsLimiter() *common.InMemoryRateLimiter {
	inviteAttemptMu.Lock()
	defer inviteAttemptMu.Unlock()
	if inviteAttemptLimiter == nil {
		limiter := &common.InMemoryRateLimiter{}
		limiter.Init(common.RateLimitKeyExpirationDuration)
		inviteAttemptLimiter = limiter
	}
	return inviteAttemptLimiter
}

// ResetRegistrationInviteAttemptsForTests 重置邀请码失败计数（仅测试使用）。
// 先 Close 旧实例（停止其清理 goroutine）再新建，幂等且不泄漏 goroutine。
func ResetRegistrationInviteAttemptsForTests() {
	inviteAttemptMu.Lock()
	if inviteAttemptLimiter != nil {
		inviteAttemptLimiter.Close()
	}
	limiter := &common.InMemoryRateLimiter{}
	limiter.Init(common.RateLimitKeyExpirationDuration)
	inviteAttemptLimiter = limiter
	inviteAttemptMu.Unlock()
}

// recordInviteCodeFailure 记录一次邀请码失败并返回是否仍在限额内（false 表示超限）。
func recordInviteCodeFailure(ip string) bool {
	return inviteAttemptsLimiter().Request(
		inviteCodeAttemptMark+":"+ip,
		inviteCodeAttemptMaxFailures,
		inviteCodeAttemptWindowSeconds,
	)
}

// RegistrationInviteGuard 在"仅限邀请码注册"开启时校验注册发码请求的邀请码。
//
// 只拦截 purpose=register 的发码请求（普通邮箱绑定等用途不受影响）：
//   - 校验成功：放行且不计失败次数；
//   - 校验失败（缺失/无效）：per-IP 失败计数，未超限返回 200 success=false；
//     超限返回 429 + Retry-After（防止邀请码枚举）。
//
// 位于 EmailSendRateLimit 之前，保证无效邀请码不消耗邮箱配额、不发送邮件。
// 校验逻辑统一调用 service.ValidateInviteCode（精确匹配、区分大小写、不记明文）。
func RegistrationInviteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !service.InviteCodeRequired() || strings.TrimSpace(c.Query("purpose")) != "register" {
			c.Next()
			return
		}
		_, err := service.ValidateInviteCode(c.Query("invite_code"))
		if err == nil {
			c.Next()
			return
		}
		if !recordInviteCodeFailure(c.ClientIP()) {
			c.Header("Retry-After", strconv.Itoa(inviteCodeAttemptWindowSeconds))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgUserInviteCodeAttemptsLimit),
			})
			c.Abort()
			return
		}
		messageKey := i18n.MsgUserInviteCodeInvalid
		if errors.Is(err, service.ErrInviteCodeRequired) {
			messageKey = i18n.MsgUserInviteCodeRequired
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.T(c, messageKey),
		})
		c.Abort()
	}
}
