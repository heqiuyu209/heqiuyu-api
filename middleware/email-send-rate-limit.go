package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/service"
)

const (
	// emailSendBurstMark / emailSendBurstMaxRequests / emailSendBurstDuration
	// 是原 EmailVerificationRateLimit 的"单 IP 短窗"语义：30 秒内最多 2 次。
	// 该语义并入本中间件，作为高峰期的第一道快速拦截。
	emailSendBurstMark        = "EV"
	emailSendBurstMaxRequests = 2
	emailSendBurstDuration    = 30
)

// emailSendPurposeFromPath 根据路由推导验证码用途。
//
// 计划 2.6：/api/verification → "v"（注册/绑定邮箱验证码），
// /api/reset_password → "r"（密码重置）。用途参与邮箱维度冷却/日上限的键。
func emailSendPurposeFromPath(c *gin.Context) string {
	if strings.HasSuffix(c.FullPath(), "/reset_password") {
		return common.PasswordResetPurpose
	}
	return common.EmailVerificationPurpose
}

// emailSendBurstAllow 实现原 EmailVerificationRateLimit 的 per-IP 短窗限流。
func emailSendBurstAllow(c *gin.Context) (allowed bool, retryAfter int) {
	ip := c.ClientIP()
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		key := "emailVerification:" + emailSendBurstMark + ":" + ip
		burstTTL := time.Duration(emailSendBurstDuration) * time.Second
		count, err := common.RDB.Incr(ctx, key).Result()
		if err != nil {
			common.SysLog("[registration-guard] email send burst counter redis error, falling back to memory: " + err.Error())
		} else {
			// 保证键一定带 TTL，避免旧实现"仅 count==1 时 EXPIRE、之后崩溃/失败"
			// 留下永不过期的计数键。只在 TTL 缺失时补设，不刷新已有 TTL，
			// 保持固定 30 秒窗口语义（不放大成滑动窗口）。
			repairTTL := count == 1
			if !repairTTL {
				if ttl, ttlErr := common.RDB.TTL(ctx, key).Result(); ttlErr == nil && ttl < 0 {
					repairTTL = true
				}
			}
			if repairTTL {
				if expireErr := common.RDB.Expire(ctx, key, burstTTL).Err(); expireErr != nil {
					common.SysLog("[registration-guard] email send burst counter expire failed, deleting key: " + expireErr.Error())
					_ = common.RDB.Del(ctx, key).Err()
				}
			}
			if count <= int64(emailSendBurstMaxRequests) {
				return true, 0
			}
			waitSeconds := int64(emailSendBurstDuration)
			if ttl, ttlErr := common.RDB.TTL(ctx, key).Result(); ttlErr == nil && ttl > 0 {
				waitSeconds = int64(ttl.Seconds())
			}
			if waitSeconds < 1 {
				waitSeconds = 1
			}
			return false, int(waitSeconds)
		}
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	key := emailSendBurstMark + ":" + ip
	if !inMemoryRateLimiter.Request(key, emailSendBurstMaxRequests, emailSendBurstDuration) {
		return false, emailSendBurstDuration
	}
	return true, 0
}

func abortEmailSendRateLimited(c *gin.Context, retryAfter int) {
	if retryAfter <= 0 {
		retryAfter = 60
	}
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.JSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": i18n.T(c, i18n.MsgUserEmailSendRateLimited),
	})
	c.Abort()
}

// EmailSendRateLimit 保护发码/重置邮件接口。
//
// 检查顺序：
//  1. per-IP 30 秒 2 次短窗（原 EmailVerificationRateLimit 语义，抵御突发）；
//  2. registration_setting 配置的单邮箱冷却/日上限、单 IP 每小时上限、全站每小时预算。
func EmailSendRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if allowed, retryAfter := emailSendBurstAllow(c); !allowed {
			common.SysLog(fmt.Sprintf("[registration-guard] email send burst limited ip=%s", c.ClientIP()))
			abortEmailSendRateLimited(c, retryAfter)
			return
		}

		email := strings.ToLower(strings.TrimSpace(c.Query("email")))
		if email == "" {
			// 参数由 controller 统一校验；这里仍放行以便返回既有参数错误文案。
			c.Next()
			return
		}

		decision := service.CheckEmailSend(c.ClientIP(), email, emailSendPurposeFromPath(c))
		if decision.Allowed {
			c.Next()
			return
		}
		common.SysLog(fmt.Sprintf("[registration-guard] email send limited ip=%s reason=%s", c.ClientIP(), decision.Reason))
		abortEmailSendRateLimited(c, decision.RetryAfterSeconds)
	}
}
