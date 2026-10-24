package middleware

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

type t8SiteverifyCall struct {
	secret   string
	response string
	remoteIP string
}

type t8SiteverifyRecorder struct {
	mu    sync.Mutex
	calls []t8SiteverifyCall
}

func (r *t8SiteverifyRecorder) snapshot() []t8SiteverifyCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]t8SiteverifyCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// t8StartSiteverify 启动 httptest 版 Cloudflare siteverify 并替换 TurnstileVerifyURL。
//
// token 约定：good-token → success=true；slow-token → 延迟 6 秒（触发 5 秒超时）；
// 其余 token → success=false。
func t8StartSiteverify(t *testing.T) *t8SiteverifyRecorder {
	t.Helper()
	recorder := &t8SiteverifyRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		recorder.mu.Lock()
		recorder.calls = append(recorder.calls, t8SiteverifyCall{
			secret:   r.FormValue("secret"),
			response: r.FormValue("response"),
			remoteIP: r.FormValue("remoteip"),
		})
		recorder.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		token := r.FormValue("response")
		if token == "slow-token" {
			time.Sleep(6 * time.Second)
		}
		if token == "good-token" {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	t.Cleanup(server.Close)

	prevURL := TurnstileVerifyURL
	TurnstileVerifyURL = server.URL
	t.Cleanup(func() { TurnstileVerifyURL = prevURL })
	return recorder
}

// t8TurnstileSetup 准备 Turnstile 开关、注册防护配置与测试密钥，结束后恢复全局状态。
func t8TurnstileSetup(t *testing.T) *operation_setting.RegistrationSetting {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevCheckEnabled := common.TurnstileCheckEnabled
	prevSecretKey := common.TurnstileSecretKey
	common.TurnstileCheckEnabled = true
	common.TurnstileSecretKey = "t8-turnstile-secret"
	t.Cleanup(func() {
		common.TurnstileCheckEnabled = prevCheckEnabled
		common.TurnstileSecretKey = prevSecretKey
	})

	setting := operation_setting.GetRegistrationSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })
	*setting = operation_setting.RegistrationSetting{Enabled: true, TurnstileStrict: true}
	return setting
}

// t8TurnstileEngine 构造带 session 中间件的测试引擎：
//
//	GET  /seed/:kind  写入各种会话标记（fresh/expired/legacy-true）
//	POST /strict      TurnstileCheckStrict
//	POST /legacy      TurnstileCheck（旧会话复用语义）
func t8TurnstileEngine() *gin.Engine {
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("t8-turnstile-session-secret"))))
	engine.POST("/seed/:kind", func(c *gin.Context) {
		s := sessions.Default(c)
		switch c.Param("kind") {
		case "fresh":
			s.Set(turnstileSessionKey, time.Now().Unix())
		case "expired":
			s.Set(turnstileSessionKey, time.Now().Add(-31*time.Minute).Unix())
		case "legacy-true":
			s.Set(turnstileSessionKey, true)
		}
		if err := s.Save(); err != nil {
			c.String(http.StatusInternalServerError, "save session: %v", err)
			return
		}
		c.String(http.StatusOK, "seeded")
	})
	engine.POST("/strict", TurnstileCheckStrict(), func(c *gin.Context) {
		c.String(http.StatusOK, "handler-called")
	})
	engine.POST("/legacy", TurnstileCheck(), func(c *gin.Context) {
		c.String(http.StatusOK, "handler-called")
	})
	return engine
}

func t8StartTurnstileServer(t *testing.T, engine *gin.Engine) (*httptest.Server, *http.Client) {
	t.Helper()
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookiejar: %v", err)
	}
	return server, &http.Client{Jar: jar, Timeout: 20 * time.Second}
}

func t8TurnstilePost(t *testing.T, client *http.Client, url string) (int, string, *http.Response) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body), resp
}

// TestTurnstileStrictNoTokenRejected 无 token 直接拒绝，handler 不会执行。
func TestTurnstileStrictNoTokenRejected(t *testing.T) {
	_ = t8TurnstileSetup(t)
	t8StartSiteverify(t)
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict")
	if !strings.Contains(body, "Turnstile token 为空") {
		t.Errorf("缺少 token 时应返回明确提示，实际: %s", body)
	}
	if strings.Contains(body, "handler-called") {
		t.Errorf("缺少 token 时 handler 不应执行")
	}
}

// TestTurnstileStrictFailedVerificationRejected siteverify 返回 success=false 时拒绝。
func TestTurnstileStrictFailedVerificationRejected(t *testing.T) {
	_ = t8TurnstileSetup(t)
	recorder := t8StartSiteverify(t)
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict?turnstile=bad-token")
	if !strings.Contains(body, "Turnstile 校验失败") {
		t.Errorf("校验失败时应返回统一提示，实际: %s", body)
	}
	if strings.Contains(body, "handler-called") {
		t.Errorf("校验失败时 handler 不应执行")
	}

	calls := recorder.snapshot()
	if len(calls) != 1 {
		t.Fatalf("siteverify 应被调用 1 次，实际 %d 次", len(calls))
	}
	if calls[0].secret != "t8-turnstile-secret" {
		t.Errorf("siteverify secret 应使用当前配置，实际 %q", calls[0].secret)
	}
	if calls[0].response != "bad-token" {
		t.Errorf("siteverify response 应为请求中的 turnstile token，实际 %q", calls[0].response)
	}
	if calls[0].remoteIP == "" {
		t.Errorf("siteverify remoteip 不应为空")
	}
}

// TestTurnstileStrictValidTokenAllowed 合法 token 放行，且 strict 不写 session（无 Set-Cookie）。
func TestTurnstileStrictValidTokenAllowed(t *testing.T) {
	_ = t8TurnstileSetup(t)
	t8StartSiteverify(t)
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	status, body, resp := t8TurnstilePost(t, client, server.URL+"/strict?turnstile=good-token")
	if status != http.StatusOK || body != "handler-called" {
		t.Fatalf("合法 token 应放行，实际 %d %q", status, body)
	}
	if cookies := resp.Cookies(); len(cookies) != 0 {
		t.Errorf("strict 校验成功不应写 session（不应有 Set-Cookie），实际 %v", cookies)
	}
}

// TestTurnstileStrictIgnoresSessionMarker strict 不读取 session：
// 即使携带 30 分钟内的有效会话标记，没有 token 仍然必须被拒。
func TestTurnstileStrictIgnoresSessionMarker(t *testing.T) {
	_ = t8TurnstileSetup(t)
	recorder := t8StartSiteverify(t)
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	status, body, _ := t8TurnstilePost(t, client, server.URL+"/seed/fresh")
	if status != http.StatusOK || body != "seeded" {
		t.Fatalf("写入会话标记失败: %d %q", status, body)
	}

	_, body, _ = t8TurnstilePost(t, client, server.URL+"/strict")
	if !strings.Contains(body, "Turnstile token 为空") {
		t.Errorf("strict 模式必须忽略会话标记（不复用），实际: %s", body)
	}

	// 带合法 token 时仍然必须真的调用 siteverify。
	_, body, _ = t8TurnstilePost(t, client, server.URL+"/strict?turnstile=good-token")
	if body != "handler-called" {
		t.Fatalf("strict 模式合法 token 应放行，实际: %s", body)
	}
	if len(recorder.snapshot()) != 1 {
		t.Errorf("strict 模式即使有会话标记也必须调用 siteverify，实际调用 %d 次", len(recorder.snapshot()))
	}
}

// TestTurnstileSessionMarkerExpiry 覆盖旧 L5 修复：
// 日志标记必须是 30 分钟内的时间戳；旧 bool true 一律视为过期。
func TestTurnstileSessionMarkerExpiry(t *testing.T) {
	cases := []struct {
		name        string
		seed        string
		token       string
		wantAllowed bool
	}{
		{name: "fresh timestamp reused", seed: "fresh", wantAllowed: true},
		{name: "expired timestamp rejected", seed: "expired", wantAllowed: false},
		{name: "legacy bool true rejected", seed: "legacy-true", wantAllowed: false},
		{name: "fresh timestamp plus new token", seed: "fresh", token: "good-token", wantAllowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = t8TurnstileSetup(t)
			t8StartSiteverify(t)
			server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

			if status, body, _ := t8TurnstilePost(t, client, server.URL+"/seed/"+tc.seed); status != http.StatusOK {
				t.Fatalf("写入会话标记失败: %d %q", status, body)
			}
			url := server.URL + "/legacy"
			if tc.token != "" {
				url += "?turnstile=" + tc.token
			}
			_, body, _ := t8TurnstilePost(t, client, url)
			allowed := body == "handler-called"
			if allowed != tc.wantAllowed {
				t.Errorf("场景 %q: 期望放行=%v，实际响应 %q", tc.name, tc.wantAllowed, body)
			}
		})
	}
}

// TestTurnstileStrictDecoupledFromRegistrationEnabled 覆盖 R2-2 语义变更：
// strict 只读 turnstile_strict，与 registration_setting.enabled 解耦。
// enabled=false + turnstile_strict=true 时仍然每次强制校验、不读 session。
func TestTurnstileStrictDecoupledFromRegistrationEnabled(t *testing.T) {
	setting := t8TurnstileSetup(t)
	recorder := t8StartSiteverify(t)
	setting.Enabled = false
	setting.TurnstileStrict = true
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	// 预先写入有效会话标记：strict 不得复用。
	if status, body, _ := t8TurnstilePost(t, client, server.URL+"/seed/fresh"); status != http.StatusOK {
		t.Fatalf("写入会话标记失败: %d %q", status, body)
	}

	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict")
	if !strings.Contains(body, "Turnstile token 为空") {
		t.Errorf("enabled=false + turnstile_strict=true 时仍必须每次校验（不读 session），实际: %s", body)
	}
	if calls := len(recorder.snapshot()); calls != 0 {
		t.Errorf("缺少 token 时不应调用 siteverify，实际 %d 次", calls)
	}

	// 合法 token → 放行，且必须真实调用 siteverify（证明没有退回复用）。
	_, body, _ = t8TurnstilePost(t, client, server.URL+"/strict?turnstile=good-token")
	if body != "handler-called" {
		t.Fatalf("enabled=false + turnstile_strict=true 时合法 token 应放行，实际: %s", body)
	}
	if calls := len(recorder.snapshot()); calls != 1 {
		t.Errorf("strict 模式每次请求都应调用 siteverify，实际 %d 次", calls)
	}
}

// TestTurnstileStrictDisabledFallsBackToSession 覆盖 R2-2 回滚路径：
// 只有 turnstile_strict=false 才退回旧会话复用语义。
func TestTurnstileStrictDisabledFallsBackToSession(t *testing.T) {
	setting := t8TurnstileSetup(t)
	t8StartSiteverify(t)
	setting.Enabled = true
	setting.TurnstileStrict = false
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	if status, body, _ := t8TurnstilePost(t, client, server.URL+"/seed/fresh"); status != http.StatusOK {
		t.Fatalf("写入会话标记失败: %d %q", status, body)
	}
	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict")
	if body != "handler-called" {
		t.Errorf("turnstile_strict=false 时应退回会话复用语义，实际: %s", body)
	}

	// 没有会话标记的新客户端：无 token 仍必须被拒。
	server2, client2 := t8StartTurnstileServer(t, t8TurnstileEngine())
	_, body, _ = t8TurnstilePost(t, client2, server2.URL+"/strict")
	if !strings.Contains(body, "Turnstile token 为空") {
		t.Errorf("turnstile_strict=false 且无会话标记时应要求 token，实际: %s", body)
	}
}

// TestTurnstileVerifyTimeout siteverify 挂起 6 秒时必须在 5 秒超时后拒绝。
func TestTurnstileVerifyTimeout(t *testing.T) {
	_ = t8TurnstileSetup(t)
	t8StartSiteverify(t)
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	start := time.Now()
	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict?turnstile=slow-token")
	elapsed := time.Since(start)
	if !strings.Contains(body, "Turnstile 校验失败") {
		t.Errorf("siteverify 超时应返回失败提示，实际: %s", body)
	}
	if elapsed < 4*time.Second {
		t.Errorf("超时测试过早返回（%.1fs），未真正等待 5 秒超时", elapsed.Seconds())
	}
	if elapsed > 10*time.Second {
		t.Errorf("siteverify 超时保护未生效：请求耗时 %.1fs（应约 5s）", elapsed.Seconds())
	}
}

// TestTurnstileDisabledPassThrough common.TurnstileCheckEnabled=false 时直接放行。
func TestTurnstileDisabledPassThrough(t *testing.T) {
	_ = t8TurnstileSetup(t)
	common.TurnstileCheckEnabled = false
	t8StartSiteverify(t)
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict")
	if body != "handler-called" {
		t.Errorf("Turnstile 总开关关闭时应直接放行，实际: %s", body)
	}
}

// TestTurnstileVerifyURLEmpty 覆盖校验地址未配置时的失败路径（不发请求）。
func TestTurnstileVerifyURLEmpty(t *testing.T) {
	_ = t8TurnstileSetup(t)
	recorder := t8StartSiteverify(t)
	TurnstileVerifyURL = ""
	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())

	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict?turnstile=good-token")
	if !strings.Contains(body, "Turnstile 校验失败") {
		t.Errorf("校验地址为空时应拒绝并返回失败提示，实际: %s", body)
	}
	if len(recorder.snapshot()) != 0 {
		t.Errorf("校验地址为空时不应发起 siteverify 请求")
	}
}

// TestTurnstileSiteverifyResponseIsJSON 覆盖 siteverify 返回非 JSON 时按失败处理。
func TestTurnstileSiteverifyResponseIsJSON(t *testing.T) {
	_ = t8TurnstileSetup(t)
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	t.Cleanup(badServer.Close)
	prevURL := TurnstileVerifyURL
	TurnstileVerifyURL = badServer.URL
	t.Cleanup(func() { TurnstileVerifyURL = prevURL })

	server, client := t8StartTurnstileServer(t, t8TurnstileEngine())
	_, body, _ := t8TurnstilePost(t, client, server.URL+"/strict?turnstile=good-token")
	if !strings.Contains(body, "Turnstile 校验失败") {
		t.Errorf("siteverify 返回非 JSON 时应按失败处理，实际: %s", body)
	}
}
