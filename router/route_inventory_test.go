package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestCriticalRoutesRegistered 断言关键路由确实已注册。
//
// 背景：前端钱包页面一直在调用 POST /api/user/topup 与 GET /api/user/topup/self，
// 但后端从未注册它们，用户看到的一直是 404（审计报告 D1）。这类"前后端脱节"编译器
// 发现不了，因此这里用路由清单做回归保护：任何被删除/改名/漏注册的关键路由都会让
// 该测试失败。
func TestCriticalRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	required := []struct {
		method string
		path   string
	}{
		// 钱包：兑换码充值 + 本人流水（曾因未注册而 404）
		{http.MethodPost, "/api/user/topup"},
		{http.MethodGet, "/api/user/topup/self"},
		// 登录与二次验证
		{http.MethodPost, "/api/user/login"},
		{http.MethodPost, "/api/user/login/2fa"},
		{http.MethodPost, "/api/verify"},
		// 凭据读取（root + 二次验证门禁所在）
		{http.MethodPost, "/api/channel/:id/key"},
		{http.MethodPost, "/api/token/:id/key"},
		// 用户管理
		{http.MethodGet, "/api/user/"},
		{http.MethodGet, "/api/user/search"},
		{http.MethodPost, "/api/user/manage"},
	}

	registered := make(map[string]struct{})
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for _, want := range required {
		key := want.method + " " + want.path
		if _, ok := registered[key]; !ok {
			t.Errorf("required route not registered: %s", key)
		}
	}
}
