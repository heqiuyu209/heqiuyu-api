package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/constant"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/logger"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/ratio_setting"
	"github.com/heqiuyu/heqiuyu-api/types"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func validUserInfo(username string, role int) bool {
	// check username is empty
	if strings.TrimSpace(username) == "" {
		return false
	}
	if !common.IsValidateRole(role) {
		return false
	}
	return true
}

// resolvedIdentity 是鉴权成功后的权威身份信息。
type resolvedIdentity struct {
	Id             int
	Username       string
	Role           int
	Status         int
	Group          string
	UseAccessToken bool
}

// denyJSON 输出鉴权失败响应并中止请求。
func denyJSON(c *gin.Context, httpStatus int, messageKey string) {
	c.JSON(httpStatus, gin.H{
		"success": false,
		"message": common.TranslateMessage(c, messageKey),
	})
	c.Abort()
}

// sessionAuthVersionValid 校验会话 cookie 中记录的认证版本号是否与数据库当前值一致。
// 仅在会话中存有版本号时启用（历史会话无版本号则跳过，依赖 30 天 cookie 过期兜底）。
// 密码变更（ResetUserPasswordByEmail）会使 auth_version 自增，旧会话因此失效（审计报告 R1）。
func sessionAuthVersionValid(c *gin.Context, session sessions.Session, userId int) bool {
	v, ok := session.Get(SessionAuthVersionKey).(int)
	if !ok {
		return true
	}
	cur, err := model.GetUserAuthVersion(userId)
	if err != nil {
		// 版本查询失败不阻断请求（主鉴权路径已读库成功），保守放行避免误伤。
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysLog(fmt.Sprintf("GetUserAuthVersion error for user %d: %s", userId, err.Error()))
		}
		return true
	}
	return v == cur
}

// authorize 是唯一的鉴权入口。
//
// 它先从会话 cookie 或访问令牌解析出**用户 ID**，再从数据库（经 Redis 缓存）
// 读取权威的 status / role / group。
//
// 关键点：绝不信任会话里的 role / status / group。会话 cookie 只签名、不加密，
// 有效期 30 天（main.go），其中保存的只是登录当时的快照。旧实现直接采信这些快照，
// 导致封禁、降权、删除用户对已登录会话完全无效——被降权的管理员可以继续以管理员
// 身份操作最长 30 天（审计报告 H1）。
func authorize(c *gin.Context, minRole int) (*resolvedIdentity, bool) {
	session := sessions.Default(c)
	idRaw := session.Get("id")
	useAccessToken := false

	var userId int
	if idRaw != nil {
		id, ok := idRaw.(int)
		if !ok || id <= 0 {
			denyJSON(c, http.StatusUnauthorized, i18n.MsgAuthNotLoggedIn)
			return nil, false
		}
		userId = id
		// 密码已变更：旧会话版本号落后于数据库当前值，立即失效
		if !sessionAuthVersionValid(c, session, userId) {
			session.Clear()
			_ = session.Save()
			denyJSON(c, http.StatusOK, i18n.MsgAuthNotLoggedIn)
			return nil, false
		}
	} else {
		// 无会话：尝试访问令牌（access token）路径
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			denyJSON(c, http.StatusUnauthorized, i18n.MsgAuthNotLoggedIn)
			return nil, false
		}
		user, authErr := model.ValidateAccessToken(accessToken)
		if authErr != nil {
			if errors.Is(authErr, model.ErrDatabase) {
				common.SysLog("ValidateAccessToken database error: " + authErr.Error())
				denyJSON(c, http.StatusInternalServerError, i18n.MsgDatabaseError)
			} else {
				denyJSON(c, http.StatusOK, i18n.MsgAuthAccessTokenInvalid)
			}
			return nil, false
		}
		if user == nil || user.Username == "" || user.Id == 0 {
			denyJSON(c, http.StatusOK, i18n.MsgAuthAccessTokenInvalid)
			return nil, false
		}
		userId = user.Id
		useAccessToken = true
	}

	// get header Heqiuyu-Api-User
	apiUserIdStr := c.Request.Header.Get("Heqiuyu-Api-User")
	if apiUserIdStr == "" {
		denyJSON(c, http.StatusUnauthorized, i18n.MsgAuthUserIdNotProvided)
		return nil, false
	}
	apiUserId, err := strconv.Atoi(apiUserIdStr)
	if err != nil {
		denyJSON(c, http.StatusUnauthorized, i18n.MsgAuthUserIdFormatError)
		return nil, false
	}
	if userId != apiUserId {
		denyJSON(c, http.StatusUnauthorized, i18n.MsgAuthUserIdMismatch)
		return nil, false
	}

	// 权威身份：每次请求都从数据库（经缓存）读取，使封禁/降权/分组变更立即生效。
	status, role, group, username, err := model.GetUserAuthz(userId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 用户已被删除：其旧会话必须立即失效
			denyJSON(c, http.StatusUnauthorized, i18n.MsgAuthNotLoggedIn)
			return nil, false
		}
		common.SysLog(fmt.Sprintf("GetUserAuthz database error for user %d: %s", userId, err.Error()))
		denyJSON(c, http.StatusInternalServerError, i18n.MsgDatabaseError)
		return nil, false
	}
	if status == common.UserStatusDisabled {
		denyJSON(c, http.StatusOK, i18n.MsgAuthUserBanned)
		return nil, false
	}
	if role < minRole {
		denyJSON(c, http.StatusOK, i18n.MsgAuthInsufficientPrivilege)
		return nil, false
	}
	if !validUserInfo(username, role) {
		denyJSON(c, http.StatusOK, i18n.MsgAuthUserInfoInvalid)
		return nil, false
	}

	return &resolvedIdentity{
		Id:             userId,
		Username:       username,
		Role:           role,
		Status:         status,
		Group:          group,
		UseAccessToken: useAccessToken,
	}, true
}

func authHelper(c *gin.Context, minRole int) {
	identity, ok := authorize(c, minRole)
	if !ok {
		return
	}
	// 防止不同 heqiuyu 版本冲突，导致数据不通用
	c.Header("Auth-Version", "864b7076dbcd0a3c01b5520316720ebf")
	c.Set("username", identity.Username)
	c.Set("role", identity.Role)
	c.Set("id", identity.Id)
	c.Set("group", identity.Group)
	c.Set("user_group", identity.Group)
	c.Set("use_access_token", identity.UseAccessToken)

	c.Next()
}

// TryUserAuth 用于"登录与否都可访问"的接口（如定价页）。
// 仅在会话有效且用户未被封禁时注入权威身份，绝不阻断请求。
func TryUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		if id, ok := session.Get("id").(int); ok && id > 0 {
			status, role, group, username, err := model.GetUserAuthz(id)
			if err == nil && status == common.UserStatusEnabled && validUserInfo(username, role) {
				c.Set("id", id)
				c.Set("username", username)
				c.Set("role", role)
				c.Set("group", group)
				c.Set("user_group", group)
			}
		}
		c.Next()
	}
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCommonUser)
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleAdminUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleRootUser)
	}
}

// TokenOrUserAuth allows either session-based user auth or API token auth.
// Used for endpoints that need to be accessible from both the dashboard and API clients.
func TokenOrUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// Try session auth first (dashboard users), verified against the database
		// so that a disabled/deleted/demoted account cannot keep using a stale cookie.
		session := sessions.Default(c)
		if id, ok := session.Get("id").(int); ok && id > 0 && sessionAuthVersionValid(c, session, id) {
			status, role, group, username, err := model.GetUserAuthz(id)
			if err == nil && status == common.UserStatusEnabled && validUserInfo(username, role) {
				c.Set("id", id)
				c.Set("username", username)
				c.Set("role", role)
				c.Set("group", group)
				c.Set("user_group", group)
				c.Next()
				return
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				common.SysLog(fmt.Sprintf("TokenOrUserAuth GetUserAuthz error for user %d: %s", id, err.Error()))
			}
		}
		// Fall back to token auth (API clients)
		TokenAuth()(c)
	}
}

// TokenAuthReadOnly 精简版令牌认证中间件，用于只读查询接口。
// 与 TokenAuth 相比省略会话/用户密码等链路，仅按令牌 key 鉴权；
// 安全修复：已校验令牌启用状态（禁用/过期/额度耗尽均拒绝），并检查用户是否被封禁。
func TokenAuthReadOnly() func(c *gin.Context) {
	return func(c *gin.Context) {
		key := c.Request.Header.Get("Authorization")
		if key == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgTokenNotProvided),
			})
			c.Abort()
			return
		}
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key = strings.TrimPrefix(key, "sk-")
		parts := strings.Split(key, "-")
		key = parts[0]

		// 用与中继路径完全相同的校验（状态 + 过期时间 + 剩余额度），
		// 而不是只比对 status 列：否则过期或额度耗尽的令牌仍能用于查询接口。
		token, err := model.ValidateUserToken(key)
		if err != nil {
			if errors.Is(err, model.ErrDatabase) {
				common.SysLog("TokenAuthReadOnly ValidateUserToken database error: " + err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
				})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgTokenInvalid),
				})
			}
			c.Abort()
			return
		}

		// 令牌级 IP 白名单必须与中继路径一致，否则被限制的令牌可被异地用于查询。
		allowIps := token.GetIpLimits()
		if len(allowIps) > 0 {
			clientIp := c.ClientIP()
			ip := net.ParseIP(clientIp)
			if ip == nil || !common.IsIpInCIDRList(ip, allowIps) {
				c.JSON(http.StatusForbidden, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgTokenStatusUnavailable),
				})
				c.Abort()
				return
			}
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			common.SysLog(fmt.Sprintf("TokenAuthReadOnly GetUserCache error for user %d: %v", token.UserId, err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
			})
			c.Abort()
			return
		}
		if userCache.Status != common.UserStatusEnabled {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
			})
			c.Abort()
			return
		}

		c.Set("id", token.UserId)
		c.Set("token_id", token.Id)
		c.Set("token_key", token.Key)
		c.Next()
	}
}

func TokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 先检测是否为ws
		if c.Request.Header.Get("Sec-WebSocket-Protocol") != "" {
			// Sec-WebSocket-Protocol: realtime, openai-insecure-api-key.sk-xxx, openai-beta.realtime-v1
			// read sk from Sec-WebSocket-Protocol
			key := c.Request.Header.Get("Sec-WebSocket-Protocol")
			parts := strings.Split(key, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "openai-insecure-api-key") {
					key = strings.TrimPrefix(part, "openai-insecure-api-key.")
					break
				}
			}
			c.Request.Header.Set("Authorization", "Bearer "+key)
		}
		// 检查path包含/v1/messages 或 /v1/models
		if strings.Contains(c.Request.URL.Path, "/v1/messages") || strings.Contains(c.Request.URL.Path, "/v1/models") {
			anthropicKey := c.Request.Header.Get("x-api-key")
			if anthropicKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+anthropicKey)
			}
		}
		// gemini api 从query中获取key
		if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/openai/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
			skKey := c.Query("key")
			if skKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+skKey)
			}
			// 从x-goog-api-key header中获取key
			xGoogKey := c.Request.Header.Get("x-goog-api-key")
			if xGoogKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+xGoogKey)
			}
		}
		key := c.Request.Header.Get("Authorization")
		parts := make([]string, 0)
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		if key == "" || key == "midjourney-proxy" {
			key = c.Request.Header.Get("mj-api-secret")
			if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
				key = strings.TrimSpace(key[7:])
			}
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		} else {
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		}
		token, err := model.ValidateUserToken(key)
		if token != nil {
			id := c.GetInt("id")
			if id == 0 {
				c.Set("id", token.UserId)
			}
		}
		if err != nil {
			if errors.Is(err, model.ErrDatabase) {
				common.SysLog("TokenAuth ValidateUserToken database error: " + err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError,
					common.TranslateMessage(c, i18n.MsgDatabaseError))
			} else {
				abortWithOpenAiMessage(c, http.StatusUnauthorized,
					common.TranslateMessage(c, i18n.MsgTokenInvalid))
			}
			return
		}

		allowIps := token.GetIpLimits()
		if len(allowIps) > 0 {
			clientIp := c.ClientIP()
			logger.LogDebug(c, "Token has IP restrictions, checking client IP %s", clientIp)
			ip := net.ParseIP(clientIp)
			if ip == nil {
				abortWithOpenAiMessage(c, http.StatusForbidden, "无法解析客户端 IP 地址")
				return
			}
			if common.IsIpInCIDRList(ip, allowIps) == false {
				abortWithOpenAiMessage(c, http.StatusForbidden, "您的 IP 不在令牌允许访问的列表中", types.ErrorCodeAccessDenied)
				return
			}
			logger.LogDebug(c, "Client IP %s passed the token IP restrictions check", clientIp)
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			common.SysLog(fmt.Sprintf("TokenAuth GetUserCache error for user %d: %v", token.UserId, err))
			abortWithOpenAiMessage(c, http.StatusInternalServerError,
				common.TranslateMessage(c, i18n.MsgDatabaseError))
			return
		}
		userEnabled := userCache.Status == common.UserStatusEnabled
		if !userEnabled {
			abortWithOpenAiMessage(c, http.StatusForbidden, common.TranslateMessage(c, i18n.MsgAuthUserBanned))
			return
		}

		userCache.WriteContext(c)

		userGroup := userCache.Group
		tokenGroup := token.Group
		if tokenGroup != "" {
			// check common.UserUsableGroups[userGroup]
			if _, ok := service.GetUserUsableGroups(userGroup)[tokenGroup]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", tokenGroup))
				return
			}
			// check group in common.GroupRatio
			if !ratio_setting.ContainsGroupRatio(tokenGroup) {
				if tokenGroup != "auto" {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("分组 %s 已被弃用", tokenGroup))
					return
				}
			}
			userGroup = tokenGroup
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)

		err = SetupContextForToken(c, token, parts...)
		if err != nil {
			return
		}
		c.Next()
	}
}

func SetupContextForToken(c *gin.Context, token *model.Token, parts ...string) error {
	if token == nil {
		return fmt.Errorf("token is nil")
	}
	c.Set("id", token.UserId)
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("token_name", token.Name)
	c.Set("token_unlimited_quota", token.UnlimitedQuota)
	if !token.UnlimitedQuota {
		c.Set("token_quota", token.RemainQuota)
	}
	if token.ModelLimitsEnabled {
		c.Set("token_model_limit_enabled", true)
		c.Set("token_model_limit", token.GetModelLimitsMap())
	} else {
		c.Set("token_model_limit_enabled", false)
	}
	common.SetContextKey(c, constant.ContextKeyTokenGroup, token.Group)
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, token.CrossGroupRetry)
	if len(parts) > 1 {
		if model.IsAdmin(token.UserId) {
			c.Set("specific_channel_id", parts[1])
		} else {
			c.Header("specific_channel_version", "701e3ae1dc3f7975556d354e0675168d004891c8")
			abortWithOpenAiMessage(c, http.StatusForbidden, "普通用户不支持指定渠道")
			return fmt.Errorf("普通用户不支持指定渠道")
		}
	}
	return nil
}
