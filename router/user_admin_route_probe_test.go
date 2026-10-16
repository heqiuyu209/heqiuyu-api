package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// TestUserAdminFrontendRoutesReachable 断言前端 features/users/api.ts 里实际调用的
// 每个用户管理 URL 都能命中后端路由（未登录时应是 401，而不是 404）。
//
// 背景：这类"前后端脱节"编译器发现不了。本次排查中确认：
//   - DELETE /api/user/:id/ 曾带尾斜杠，会先吃一个 307 重定向（已在前端去掉尾斜杠）；
//   - 路由本身都存在，问题出在 handler（缺 auth_version 列）与权限门槛不一致。
func TestUserAdminFrontendRoutesReachable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 与 main.go 一致：authorize() 依赖 sessions.Default(c)，缺了会 panic
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("probe-secret"))))
	SetApiRouter(engine)

	// 与 web/default/src/features/users/api.ts 一一对应
	frontendCalls := []struct {
		name   string
		method string
		path   string
	}{
		{"getUsers", http.MethodGet, "/api/user/"},
		{"searchUsers", http.MethodGet, "/api/user/search"},
		{"getUser", http.MethodGet, "/api/user/2"},
		{"createUser", http.MethodPost, "/api/user/"},
		{"updateUser(改密码/资料)", http.MethodPut, "/api/user/"},
		{"deleteUser(删用户)", http.MethodDelete, "/api/user/2"},
		{"manageUser", http.MethodPost, "/api/user/manage"},
		{"resetUserPasskey", http.MethodDelete, "/api/user/2/reset_passkey"},
		{"resetUserTwoFA", http.MethodDelete, "/api/user/2/2fa"},
		{"getUserOAuthBindings", http.MethodGet, "/api/user/2/oauth/bindings"},
		{"adminClearUserBinding", http.MethodDelete, "/api/user/2/bindings/github"},
	}

	for _, c := range frontendCalls {
		req := httptest.NewRequest(c.method, c.path, nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		t.Logf("%-28s %-6s %-42s -> %d", c.name, c.method, c.path, w.Code)
		// 404 = 路由不存在；307 = 需要尾斜杠重定向（前端不应依赖它）
		if w.Code == http.StatusNotFound {
			t.Errorf("%s: %s %s 未注册（404）", c.name, c.method, c.path)
		}
		if w.Code == http.StatusTemporaryRedirect || w.Code == http.StatusMovedPermanently {
			t.Errorf("%s: %s %s 命中重定向（%d -> %s），前端不应依赖尾斜杠重定向",
				c.name, c.method, c.path, w.Code, w.Header().Get("Location"))
		}
	}
}
