package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/service"
)

// RegistrationRateLimit 是注册接口的 per-IP/全站限流中间件。
//
// 只负责"是否允许发起注册请求"（窗口尝试数、每小时/每天成功上限、全站成功上限、
// CIDR 白名单）；成功计数由 controller.Register 在入库成功后调用
// service.RecordRegistrationSuccess 回写。
func RegistrationRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		decision := service.CheckRegistrationAttempt(c.ClientIP())
		if decision.Allowed {
			c.Next()
			return
		}
		retryAfter := decision.RetryAfterSeconds
		if retryAfter <= 0 {
			retryAfter = 60
		}
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgUserRegistrationRateLimited),
		})
		c.Abort()
	}
}
