package middleware

import (
	"net/http"
	"time"

	"github.com/heqiuyu/heqiuyu-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	// SensitiveActionLoginWindowSeconds 是"近期登录"窗口。在该窗口内登录过，等价于
	// 刚刚用主凭据完成认证，允许直接执行凭据变更类操作。
	SensitiveActionLoginWindowSeconds int64 = 600

	// SessionLoginAtKey 是会话中记录登录时刻的键，由 controller.setupLogin 写入。
	SessionLoginAtKey = "login_at"

	// 与 controller 中的常量保持一致（会话键与方法名）。两侧都用字面量，
	// 避免 middleware 反向依赖 controller 造成循环引用。
	sensitiveActionMessage = "为保障账号安全，请重新登录后再执行该操作。/ For your security, please sign in again before performing this action."
)

// SensitiveActionGuard 保护"登记或撤销登录凭据"这一类高风险操作：
// 2FA 的开启/关闭、重置备份码，Passkey 的注册/删除，以及管理用 access token 的签发。
//
// 威胁模型：会话 cookie 被窃取（XSS、共享浏览器、恶意软件）后，如果这些操作只要求
// "有一个有效会话"，攻击者就能给自己登记 Passkey 或 2FA，获得**改密也踢不掉**的持久
// 访问；或者反过来登记自己的验证方式，把受害者锁在账号之外（审计报告 M2）。
//
// 放行条件（满足其一即可）：
//  1. 会话是近期建立的 —— 用户刚刚用密码或第三方身份登录过；
//  2. 账号已存在第二因子（2FA 或 Passkey），且刚刚完成过与该因子匹配的安全验证。
//
// 这样设计的好处是无需前端改动：刚登录的用户体验完全不变，而长期挂着的会话在
// 变更凭据时会被要求重新登录。
func SensitiveActionGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未登录",
			})
			c.Abort()
			return
		}

		if hasRecentLogin(c) || hasFreshFactorVerification(c, userId) {
			c.Next()
			return
		}

		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": sensitiveActionMessage,
			"code":    "REAUTH_REQUIRED",
		})
		c.Abort()
	}
}

// hasRecentLogin 判断当前会话是否在近期建立。
func hasRecentLogin(c *gin.Context) bool {
	session := sessions.Default(c)
	loginAt, ok := session.Get(SessionLoginAtKey).(int64)
	if !ok {
		return false
	}
	return time.Now().Unix()-loginAt < SensitiveActionLoginWindowSeconds
}

// hasFreshFactorVerification 判断账号是否刚完成过与已启用因子匹配的安全验证。
func hasFreshFactorVerification(c *gin.Context, userId int) bool {
	session := sessions.Default(c)
	verifiedAt, ok := session.Get(SecureVerificationSessionKey).(int64)
	if !ok || time.Now().Unix()-verifiedAt >= SecureVerificationTimeout {
		return false
	}
	verifiedMethod, _ := session.Get(secureVerificationMethodSessionKey).(string)
	if verifiedMethod == "" {
		return false
	}

	if verifiedMethod == "2fa" {
		twoFA, err := model.GetTwoFAByUserId(userId)
		if err == nil && twoFA != nil && twoFA.IsEnabled {
			return true
		}
	}
	if verifiedMethod == "passkey" {
		if _, err := model.GetPasskeyByUserID(userId); err == nil {
			return true
		}
	}
	return false
}
