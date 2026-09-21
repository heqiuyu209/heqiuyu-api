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
	"github.com/stretchr/testify/require"
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
