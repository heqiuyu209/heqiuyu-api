package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/middleware"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/oauth"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func loginSessionTestRouter(t *testing.T, seed map[string]interface{}, register func(*gin.Engine)) (*gin.Engine, *http.Cookie) {
	t.Helper()
	require.NoError(t, i18n.Init())
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("login-session-regression-test-key"))))
	router.GET("/seed", func(c *gin.Context) {
		session := sessions.Default(c)
		for key, value := range seed {
			session.Set(key, value)
		}
		require.NoError(t, session.Save())
		c.Status(http.StatusOK)
	})
	router.GET("/session", func(c *gin.Context) {
		session := sessions.Default(c)
		values := gin.H{}
		for _, key := range []string{"id", "username", "pending_user_id", "pending_username", middleware.SessionLoginAtKey, middleware.SecureVerificationSessionKey, "secure_verified_method"} {
			values[key] = session.Get(key)
		}
		c.JSON(http.StatusOK, values)
	})
	// Gin sizes pooled route parameters on the first request, so register every route first.
	register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.NotEmpty(t, response.Result().Cookies())
	return router, response.Result().Cookies()[0]
}

func requestWithSession(router *gin.Engine, path string, session *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(session)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestBeginLoginFailsClosedOnTwoFADatabaseError(t *testing.T) {
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{})) // Deliberately omit the 2FA table.
	user := &model.User{Username: "login_error", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	router, session := loginSessionTestRouter(t, map[string]interface{}{"oauth_state": "state"}, func(router *gin.Engine) {
		router.GET("/login", func(c *gin.Context) { beginLogin(c, user) })
	})
	response := requestWithSession(router, "/login", session)
	var result tokenAPIResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.False(t, result.Success, "a failed 2FA lookup must not establish a login")
	require.Empty(t, response.Result().Cookies())
}

func TestLoginDoesNotInheritSessionSecurityState(t *testing.T) {
	for _, require2FA := range []bool{false, true} {
		name := "complete"
		if require2FA {
			name = "pending_2fa"
		}
		t.Run(name, func(t *testing.T) {
			db := openTokenControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.TwoFA{}))
			user := &model.User{Username: "new_account", Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(user).Error)
			if require2FA {
				require.NoError(t, db.Create(&model.TwoFA{UserId: user.Id, IsEnabled: true, Secret: "unused"}).Error)
			}
			router, session := loginSessionTestRouter(t, map[string]interface{}{
				"id": 999, "username": "old_account", "pending_user_id": 998,
				middleware.SessionLoginAtKey:            time.Now().Unix(),
				middleware.SecureVerificationSessionKey: time.Now().Unix(),
				"secure_verified_method":                "2fa",
			}, func(router *gin.Engine) {
				router.GET("/login", func(c *gin.Context) { beginLogin(c, user) })
			})
			response := requestWithSession(router, "/login", session)
			require.NotEmpty(t, response.Result().Cookies())
			response = requestWithSession(router, "/session", response.Result().Cookies()[0])
			var state map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
			require.Nil(t, state["username"])
			require.Nil(t, state[middleware.SecureVerificationSessionKey])
			require.Nil(t, state["secure_verified_method"])
			if require2FA {
				require.Nil(t, state["id"])
				require.Nil(t, state[middleware.SessionLoginAtKey])
				require.Equal(t, float64(user.Id), state["pending_user_id"])
			} else {
				require.Equal(t, float64(user.Id), state["id"])
				require.Nil(t, state["pending_user_id"])
			}
		})
	}
}

type sessionBindingProvider struct{ oauth.Provider }

func (*sessionBindingProvider) IsEnabled() bool { return true }
func (*sessionBindingProvider) GetName() string { return "Session binding test" }
func (*sessionBindingProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return &oauth.OAuthToken{}, nil
}
func (*sessionBindingProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return &oauth.OAuthUser{ProviderUserID: "123456"}, nil
}
func (*sessionBindingProvider) IsUserIDTaken(string) bool                     { return false }
func (*sessionBindingProvider) SetProviderUserID(user *model.User, id string) { user.GitHubId = id }

func TestOAuthBindsIDOnlyLoginSession(t *testing.T) {
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	user := &model.User{Username: "existing_account", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	originalRegisterEnabled := common.RegisterEnabled
	common.RegisterEnabled = false
	t.Cleanup(func() { common.RegisterEnabled = originalRegisterEnabled })
	oauth.Register("session-binding-test", &sessionBindingProvider{})
	t.Cleanup(func() { oauth.Unregister("session-binding-test") })
	router, session := loginSessionTestRouter(t, map[string]interface{}{"id": user.Id, "oauth_state": "state"}, func(router *gin.Engine) {
		router.GET("/oauth/:provider", HandleOAuth)
	})
	response := requestWithSession(router, "/oauth/session-binding-test?state=state&code=code", session)
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Action string `json:"action"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	require.Equal(t, "bind", result.Data.Action)
	require.NoError(t, db.First(user, user.Id).Error)
	require.Equal(t, "123456", user.GitHubId)
}

// ---------------------------------------------------------------------------
// INV-S2：OAuth 首登邀请码校验（mock provider，不触网）
// ---------------------------------------------------------------------------

// inviteCodeMockProvider 是可控的 OAuth provider：
//   - taken=false 时模拟"首次登录的新用户"（会走建号分支）；
//   - taken=true + existing 时模拟"已有绑定用户"（走老用户分支，不校验邀请码）。
type inviteCodeMockProvider struct {
	name     string
	taken    bool
	existing *model.User
}

func (p *inviteCodeMockProvider) GetName() string           { return p.name }
func (p *inviteCodeMockProvider) IsEnabled() bool           { return true }
func (p *inviteCodeMockProvider) GetProviderPrefix() string { return "invmock_" }

func (p *inviteCodeMockProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return &oauth.OAuthToken{}, nil
}

func (p *inviteCodeMockProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return &oauth.OAuthUser{
		ProviderUserID: "invite-mock-uid-1",
		Username:       "invite_mock_user",
		DisplayName:    "Invite Mock",
		Email:          "Invite.Mock@Example.COM",
	}, nil
}

func (p *inviteCodeMockProvider) IsUserIDTaken(string) bool { return p.taken }

func (p *inviteCodeMockProvider) FillUserByProviderID(user *model.User, _ string) error {
	if p.existing == nil {
		return nil
	}
	*user = *p.existing
	return nil
}

func (p *inviteCodeMockProvider) SetProviderUserID(user *model.User, id string) {
	user.GitHubId = id
}

func t30OAuthInviteRouter(t *testing.T, provider *inviteCodeMockProvider, seed map[string]interface{}) (*gin.Engine, *http.Cookie) {
	t.Helper()
	oauth.Register(provider.name, provider)
	t.Cleanup(func() { oauth.Unregister(provider.name) })
	return loginSessionTestRouter(t, seed, func(router *gin.Engine) {
		router.GET("/oauth/:provider", HandleOAuth)
	})
}

type t30OAuthResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func t30DoOAuth(t *testing.T, router *gin.Engine, session *http.Cookie, providerName string) t30OAuthResponse {
	t.Helper()
	response := requestWithSession(router, "/oauth/"+providerName+"?state=state&code=code", session)
	var result t30OAuthResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
	return result
}

func t30OAuthSetup(t *testing.T, inviteRequired bool) (*gorm.DB, *operation_setting.RegistrationSetting) {
	t.Helper()
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TwoFA{}, &model.Log{}))
	setting := t30InviteSettingSnapshot(t)
	setting.InviteCodeRequired = inviteRequired
	// 隔离注册防护全局计数：OAuth 首登建号会调用 CheckRegistrationAttempt /
	// RecordRegistrationSuccess；不清零会导致 `-count=2` / `-shuffle` / 全量时
	// 命中默认 IpMaxSuccessPerHour=5 等上限，第二轮失败。
	service.ResetRegistrationGuardForTests()
	t.Cleanup(service.ResetRegistrationGuardForTests)
	setting.IpMaxAttemptsPerWindow = 0
	setting.IpMaxSuccessPerHour = 0
	setting.IpMaxSuccessPerDay = 0
	setting.GlobalMaxSuccessPerHour = 0
	prevRegisterEnabled := common.RegisterEnabled
	common.RegisterEnabled = true
	t.Cleanup(func() { common.RegisterEnabled = prevRegisterEnabled })
	return db, setting
}

func t30SeedOAuthInviter(t *testing.T, db *gorm.DB) *model.User {
	t.Helper()
	inviter := &model.User{
		Username: "oauth-inviter",
		Password: "x",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "OAuthAff1",
	}
	require.NoError(t, db.Create(inviter).Error)
	return inviter
}

// TestOAuthInviteCodeRequiredForNewUser 覆盖：开启且 OAuth 首登无码 → 拒绝且不建号。
func TestOAuthInviteCodeRequiredForNewUser(t *testing.T) {
	db, _ := t30OAuthSetup(t, true)
	provider := &inviteCodeMockProvider{name: "invite-mock-required"}
	router, session := t30OAuthInviteRouter(t, provider, map[string]interface{}{"oauth_state": "state"})

	result := t30DoOAuth(t, router, session, provider.name)
	require.False(t, result.Success)
	require.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgUserInviteCodeRequired), result.Message)

	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	require.Zero(t, count, "邀请码缺失时不应建号")
}

// TestOAuthInviteCodeInvalidForNewUser 覆盖：开启且 OAuth 首登错码 → 拒绝且不建号。
func TestOAuthInviteCodeInvalidForNewUser(t *testing.T) {
	db, _ := t30OAuthSetup(t, true)
	provider := &inviteCodeMockProvider{name: "invite-mock-invalid"}
	router, session := t30OAuthInviteRouter(t, provider, map[string]interface{}{
		"oauth_state": "state",
		"invite_code": "wrong-code",
	})

	result := t30DoOAuth(t, router, session, provider.name)
	require.False(t, result.Success)
	require.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgUserInviteCodeInvalid), result.Message)

	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	require.Zero(t, count, "邀请码无效时不应建号")
}

// TestOAuthInviteCodeValidCreatesInviter 覆盖：开启 + 有效邀请码 → 建号且邀请归因正确。
func TestOAuthInviteCodeValidCreatesInviter(t *testing.T) {
	db, _ := t30OAuthSetup(t, true)
	// 邀请奖励只在 QuotaForInviter > 0 时累计（与密码注册共用 Insert/Finalize 逻辑）。
	prevInviterQuota := common.QuotaForInviter
	common.QuotaForInviter = 100
	t.Cleanup(func() { common.QuotaForInviter = prevInviterQuota })

	inviter := t30SeedOAuthInviter(t, db)
	require.True(t, inviteCodeRequired(), "前置：invite_code_required 应为 true")
	provider := &inviteCodeMockProvider{name: "invite-mock-valid"}
	router, session := t30OAuthInviteRouter(t, provider, map[string]interface{}{
		"oauth_state": "state",
		"invite_code": inviter.AffCode,
	})

	result := t30DoOAuth(t, router, session, provider.name)
	require.True(t, result.Success, result.Message)

	var created model.User
	require.NoError(t, db.Where("github_id = ?", "invite-mock-uid-1").First(&created).Error)
	require.Equal(t, "invite.mock@example.com", created.Email, "OAuth 邮箱应 lower+trim")
	require.Equal(t, inviter.Id, created.InviterId, "OAuth 首登应把邀请码所属用户写入 users.inviter_id")

	// 邀请归因：邀请人 aff_count 递增（QuotaForInviter>0 时）。
	var reloadedInviter model.User
	require.NoError(t, db.First(&reloadedInviter, inviter.Id).Error)
	require.Equal(t, 1, reloadedInviter.AffCount, "有效邀请码应给邀请人记一次 aff_count")
}

// TestOAuthInviteCodeDisabledUnchanged 覆盖：关闭邀请码时 OAuth 首登行为不变（无码、无 aff → inviter_id=0）。
func TestOAuthInviteCodeDisabledUnchanged(t *testing.T) {
	db, _ := t30OAuthSetup(t, false)
	provider := &inviteCodeMockProvider{name: "invite-mock-disabled"}
	router, session := t30OAuthInviteRouter(t, provider, map[string]interface{}{"oauth_state": "state"})

	result := t30DoOAuth(t, router, session, provider.name)
	require.True(t, result.Success, result.Message)

	var created model.User
	require.NoError(t, db.Where("github_id = ?", "invite-mock-uid-1").First(&created).Error)
	require.Zero(t, created.InviterId, "关闭邀请码且无 aff 时 inviter_id 应为 0")
}

// TestOAuthInviteCodeDisabledUsesAff 覆盖：关闭邀请码 + 携带 aff 推荐码 → inviter_id 为 aff 所属用户。
func TestOAuthInviteCodeDisabledUsesAff(t *testing.T) {
	db, _ := t30OAuthSetup(t, false)
	affUser := t30SeedOAuthInviter(t, db)
	provider := &inviteCodeMockProvider{name: "invite-mock-disabled-aff"}
	router, session := t30OAuthInviteRouter(t, provider, map[string]interface{}{
		"oauth_state": "state",
		"aff":         affUser.AffCode,
	})

	result := t30DoOAuth(t, router, session, provider.name)
	require.True(t, result.Success, result.Message)

	var created model.User
	require.NoError(t, db.Where("github_id = ?", "invite-mock-uid-1").First(&created).Error)
	require.Equal(t, affUser.Id, created.InviterId, "关闭邀请码时 aff 推荐码应写入 users.inviter_id")
}

// TestOAuthInviteCodeExistingUserBypass 覆盖：老用户登录不校验邀请码。
func TestOAuthInviteCodeExistingUserBypass(t *testing.T) {
	db, _ := t30OAuthSetup(t, true)
	existing := &model.User{
		Username:  "oauth-existing",
		Password:  "x",
		Role:      common.RoleCommonUser,
		Status:    common.UserStatusEnabled,
		Group:     "default",
		AffCode:   "EXIST1",
		InviterId: 42,
	}
	require.NoError(t, db.Create(existing).Error)
	provider := &inviteCodeMockProvider{name: "invite-mock-existing", taken: true, existing: existing}
	router, session := t30OAuthInviteRouter(t, provider, map[string]interface{}{"oauth_state": "state"})

	result := t30DoOAuth(t, router, session, provider.name)
	require.True(t, result.Success, "老用户即使无邀请码也应可登录: %s", result.Message)

	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	require.Equal(t, int64(1), count, "老用户登录不应新建账号")

	var reloaded model.User
	require.NoError(t, db.First(&reloaded, existing.Id).Error)
	require.Equal(t, 42, reloaded.InviterId, "老用户登录不应改写已有 inviter_id")
}
