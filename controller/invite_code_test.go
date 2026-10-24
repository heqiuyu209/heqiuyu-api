package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func t30InviteControllerDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{
		Username: "ctrl-inviter",
		Password: "x",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "CtrlABC",
	}).Error)
}

func t30InviteSettingSnapshot(t *testing.T) *operation_setting.RegistrationSetting {
	t.Helper()
	setting := operation_setting.GetRegistrationSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })
	return setting
}

// TestControllerValidateInviteCode 覆盖 controller 层委托 service 的行为。
func TestControllerValidateInviteCode(t *testing.T) {
	t30InviteControllerDB(t)

	inviter, err := validateInviteCode("CtrlABC")
	require.NoError(t, err)
	require.NotZero(t, inviter)

	_, err = validateInviteCode("ctrlABC")
	require.ErrorIs(t, err, service.ErrInviteCodeInvalid)

	_, err = validateInviteCode("   ")
	require.ErrorIs(t, err, service.ErrInviteCodeRequired)
}

// TestControllerInviteCodeRequired 覆盖 controller 读取开关（独立于 registration_setting.enabled）。
func TestControllerInviteCodeRequired(t *testing.T) {
	setting := t30InviteSettingSnapshot(t)
	setting.InviteCodeRequired = true
	setting.Enabled = false
	require.True(t, inviteCodeRequired())
	setting.InviteCodeRequired = false
	setting.Enabled = true
	require.False(t, inviteCodeRequired())
}

// TestControllerInviteCodeMessageKey 覆盖邀请码错误到 i18n key 的映射。
func TestControllerInviteCodeMessageKey(t *testing.T) {
	require.Equal(t, i18n.MsgUserInviteCodeRequired, inviteCodeMessageKey(service.ErrInviteCodeRequired))
	require.Equal(t, i18n.MsgUserInviteCodeInvalid, inviteCodeMessageKey(service.ErrInviteCodeInvalid))
}

// TestRespondInviteCodeError 覆盖统一错误响应（200 + success:false + 翻译文案）。
func TestRespondInviteCodeError(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/t", func(c *gin.Context) {
		respondInviteCodeError(c, service.ErrInviteCodeRequired)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.Success)
	require.NotEmpty(t, body.Message)
	require.NotEqual(t, i18n.MsgUserInviteCodeRequired, body.Message, "应返回翻译文案而不是原始 key")
}

// TestSessionInviteCode 覆盖 OAuth session 中邀请码的读取。
func TestSessionInviteCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("t30-invite-session-secret"))))
	engine.GET("/seed", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set(inviteCodeSessionKey, "  AbC123  ")
		require.NoError(t, s.Save())
		c.Status(http.StatusOK)
	})
	engine.GET("/read", func(c *gin.Context) {
		c.String(http.StatusOK, sessionInviteCode(sessions.Default(c)))
	})

	// nil session 安全返回空串。
	require.Empty(t, sessionInviteCode(nil))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/seed", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)

	req := httptest.NewRequest(http.MethodGet, "/read", nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	readRec := httptest.NewRecorder()
	engine.ServeHTTP(readRec, req)
	require.Equal(t, "  AbC123  ", readRec.Body.String(), "sessionInviteCode 只读取，不做 trim")
}
