package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	loginLockKeyPrefix = "loginLock:"
	// 说明：此处未走 i18n 词条，避免为一条锁定提示改动全部语言包。
	loginLockMessage = "登录失败次数过多，账号已被临时锁定，请稍后再试。/ Too many failed login attempts; this account is temporarily locked. Please try again later."
)

// usernameAccount 生成账号维度的锁定标识（用户名不区分大小写）。
// 与 userIDAccount 保持一致的字符串形态，确保"检查"和"记录"落在同一个键上。
func usernameAccount(username string) string {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return ""
	}
	return "user:" + username
}

func userIDAccount(userId int) string {
	if userId <= 0 {
		return ""
	}
	return "uid:" + strconv.Itoa(userId)
}

// accountLockKey 对账号标识做散列，避免把用户名明文写入限流存储。
func accountLockKey(account string) string {
	sum := sha256.Sum256([]byte(account))
	return loginLockKeyPrefix + hex.EncodeToString(sum[:16])
}

// loginLockExceeded 只读地判断账号当前是否处于锁定状态。
// 存储不可用时一律返回 false（不阻断登录），避免因为限流组件故障导致无法登录。
func loginLockExceeded(account string) bool {
	if account == "" || common.LoginRateLimitNum <= 0 {
		return false
	}
	key := accountLockKey(account)
	if common.RedisEnabled {
		count, err := common.RDB.Get(context.Background(), key).Int()
		if err != nil {
			return false
		}
		return count >= common.LoginRateLimitNum
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return !inMemoryRateLimiter.Peek(key, common.LoginRateLimitNum, common.LoginRateLimitDuration)
}

// RecordLoginFailure 记录一次账号维度的登录失败。
//
// 这是账号级锁定与按 IP 限流的本质区别：计数绑定在账号上，攻击者换 IP
// （或伪造 X-Forwarded-For）都无法重置计数。调用方应在**认证失败**时调用。
func RecordLoginFailure(username string) {
	recordAccountFailure(usernameAccount(username))
}

// RecordLoginFailureByUserID 记录一次 2FA 步骤的失败（此时会话里只有待验证用户 ID）。
func RecordLoginFailureByUserID(userId int) {
	recordAccountFailure(userIDAccount(userId))
}

func recordAccountFailure(account string) {
	if account == "" || !common.LoginRateLimitEnable || common.LoginRateLimitNum <= 0 {
		return
	}
	key := accountLockKey(account)
	if common.RedisEnabled {
		ctx := context.Background()
		count, err := common.RDB.Incr(ctx, key).Result()
		if err != nil {
			common.SysLog("login lockout redis error: " + err.Error())
			return
		}
		if count == 1 {
			_ = common.RDB.Expire(ctx, key, time.Duration(common.LoginRateLimitDuration)*time.Second).Err()
		}
		return
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	inMemoryRateLimiter.Request(key, common.LoginRateLimitNum, common.LoginRateLimitDuration)
}

// ClearLoginFailures 在认证成功后清空该账号的失败计数。
func ClearLoginFailures(username string) {
	account := usernameAccount(username)
	if account == "" {
		return
	}
	clearAccountFailures(account)
}

// ClearLoginFailuresByUserID 在 2FA 验证成功后清空失败计数。
func ClearLoginFailuresByUserID(userId int) {
	clearAccountFailures(userIDAccount(userId))
}

func clearAccountFailures(account string) {
	if account == "" {
		return
	}
	key := accountLockKey(account)
	if common.RedisEnabled {
		if err := common.RDB.Del(context.Background(), key).Err(); err != nil {
			common.SysLog("login lockout redis delete error: " + err.Error())
		}
		return
	}
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	inMemoryRateLimiter.Delete(key)
}

// LoginLockoutAccount 从当前请求解析出用于锁定计数的账号标识。
// 登录接口从 JSON body 取 username；2FA 接口用会话里的 pending_user_id。
func LoginLockoutAccount(c *gin.Context) string {
	if strings.HasSuffix(c.Request.URL.Path, "/login/2fa") {
		session := sessions.Default(c)
		if id, ok := session.Get("pending_user_id").(int); ok {
			return userIDAccount(id)
		}
		return ""
	}
	if c.Request.Method != http.MethodPost {
		return ""
	}
	// 已认证的接口（例如 /api/verify 的二次验证步进）直接按用户 ID 计数。
	if id := c.GetInt("id"); id > 0 {
		return userIDAccount(id)
	}
	var req struct {
		Username string `json:"username"`
	}
	// 复用可回绕 body 读取，不会影响后续处理器的绑定。
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return ""
	}
	return usernameAccount(req.Username)
}

// LoginRateLimit 在进入登录处理器之前检查账号是否已被锁定。
//
// 与 CriticalRateLimit（按客户端 IP，可被 X-Forwarded-For 伪造绕过）互为补充：
// 本中间件按账号计数，是 H8 修复后"与 IP 无关"的那一层防护。
func LoginRateLimit() gin.HandlerFunc {
	if !common.LoginRateLimitEnable || common.LoginRateLimitNum <= 0 {
		return defNext
	}
	return func(c *gin.Context) {
		if loginLockExceeded(LoginLockoutAccount(c)) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": loginLockMessage,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
