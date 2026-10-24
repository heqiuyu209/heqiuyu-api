package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// t8EmailRateLimitSetup 准备确定性的发码限流配置与干净的内存限流状态。
//
// 注意：本文件的请求全部走 httptest + 假 handler，不会真正发送邮件。
func t8EmailRateLimitSetup(t *testing.T) *operation_setting.RegistrationSetting {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prevRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedisEnabled })

	// 429 文案走 i18n.T，未初始化会 panic。
	if err := i18n.Init(); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}

	setting := operation_setting.GetRegistrationSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })

	// 基线：只开总开关；各维度上限按用例显式设置，避免用例间互相污染。
	*setting = operation_setting.RegistrationSetting{Enabled: true}

	service.ResetRegistrationGuardForTests()
	t.Cleanup(service.ResetRegistrationGuardForTests)
	return setting
}

func t8ResetEmailBurstKey(ip string) {
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	inMemoryRateLimiter.Delete(emailSendBurstMark + ":" + ip)
}

func t8EmailSendEngine() *gin.Engine {
	engine := gin.New()
	handler := func(c *gin.Context) { c.String(http.StatusOK, "handler-called") }
	engine.GET("/api/verification", EmailSendRateLimit(), handler)
	engine.GET("/api/reset_password", EmailSendRateLimit(), handler)
	return engine
}

func t8EmailSendRequest(t *testing.T, engine *gin.Engine, path, ip, email string) (status int, body string, retryAfter string) {
	t.Helper()
	target := path
	if email != "" {
		target += "?email=" + url.QueryEscape(email)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("X-Forwarded-For", ip)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header().Get("Retry-After")
}

// TestEmailSendBurstPerIPLimit 覆盖原 EmailVerificationRateLimit 语义：
// 单 IP 30 秒内最多 2 次，第 3 次 429 + Retry-After。
func TestEmailSendBurstPerIPLimit(t *testing.T) {
	_ = t8EmailRateLimitSetup(t)
	const ip = "198.51.100.10"
	t8ResetEmailBurstKey(ip)
	engine := t8EmailSendEngine()

	for i := 1; i <= 2; i++ {
		status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ip, "burst"+strconv.Itoa(i)+"@example.com")
		if status != http.StatusOK || body != "handler-called" {
			t.Fatalf("第 %d 次发码应放行，实际 %d %q", i, status, body)
		}
	}
	status, body, retryAfter := t8EmailSendRequest(t, engine, "/api/verification", ip, "burst3@example.com")
	if status != http.StatusTooManyRequests {
		t.Fatalf("第 3 次发码应返回 429，实际 %d %q", status, body)
	}
	if retryAfter == "" || retryAfter == "0" {
		t.Errorf("429 响应必须带有效 Retry-After，实际 %q", retryAfter)
	}
	if !strings.Contains(body, `"success":false`) {
		t.Errorf("429 响应体应为 success:false，实际 %s", body)
	}

	// 换 IP 不受影响。
	status, body, _ = t8EmailSendRequest(t, engine, "/api/verification", "198.51.100.11", "burst-other@example.com")
	if status != http.StatusOK || body != "handler-called" {
		t.Errorf("不同 IP 的第 1 次发码应放行，实际 %d %q", status, body)
	}
}

// TestEmailSendCooldownGuard 覆盖同邮箱发码冷却（registration_setting.email_cooldown_seconds）。
func TestEmailSendCooldownGuard(t *testing.T) {
	setting := t8EmailRateLimitSetup(t)
	setting.EmailCooldownSeconds = 60
	const ip = "198.51.100.20"
	t8ResetEmailBurstKey(ip)
	engine := t8EmailSendEngine()

	const email = "cooldown@example.com"
	status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ip, email)
	if status != http.StatusOK || body != "handler-called" {
		t.Fatalf("首次发码应放行，实际 %d %q", status, body)
	}

	status, body, retryAfter := t8EmailSendRequest(t, engine, "/api/verification", ip, email)
	if status != http.StatusTooManyRequests {
		t.Fatalf("冷却期内二次发码应返回 429，实际 %d %q", status, body)
	}
	if retryAfter == "" || retryAfter == "0" {
		t.Errorf("冷却 429 必须带有效 Retry-After，实际 %q", retryAfter)
	}
	if !strings.Contains(body, `"success":false`) {
		t.Errorf("429 响应体应为 success:false，实际 %s", body)
	}
	if strings.Contains(body, i18n.MsgUserEmailSendRateLimited) {
		t.Errorf("429 文案应使用 i18n 翻译而不是原始 key，实际 %s", body)
	}
}

// TestEmailSendMissingEmailPassesThrough 缺 email 参数时不拦截，交给 controller 返回既有参数错误。
func TestEmailSendMissingEmailPassesThrough(t *testing.T) {
	_ = t8EmailRateLimitSetup(t)
	const ip = "198.51.100.30"
	t8ResetEmailBurstKey(ip)
	engine := t8EmailSendEngine()

	status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ip, "")
	if status != http.StatusOK || body != "handler-called" {
		t.Errorf("缺少 email 参数时应放行给 controller，实际 %d %q", status, body)
	}
}

// TestEmailSendDailyLimitGuard 覆盖同邮箱每日发码上限（跨 IP 仍按邮箱计数）。
func TestEmailSendDailyLimitGuard(t *testing.T) {
	setting := t8EmailRateLimitSetup(t)
	setting.EmailCooldownSeconds = 0
	setting.EmailMaxPerDay = 2
	engine := t8EmailSendEngine()

	const email = "daily-limit@example.com"
	ips := []string{"198.51.100.40", "198.51.100.41", "198.51.100.42"}
	for i := 0; i < 2; i++ {
		t8ResetEmailBurstKey(ips[i])
		status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ips[i], email)
		if status != http.StatusOK || body != "handler-called" {
			t.Fatalf("第 %d 次发码应放行，实际 %d %q", i+1, status, body)
		}
	}
	t8ResetEmailBurstKey(ips[2])
	status, body, retryAfter := t8EmailSendRequest(t, engine, "/api/verification", ips[2], email)
	if status != http.StatusTooManyRequests {
		t.Fatalf("达到 email_max_per_day=2 后第 3 次发码应返回 429，实际 %d %q", status, body)
	}
	if retryAfter == "" || retryAfter == "0" {
		t.Errorf("日上限 429 必须带有效 Retry-After，实际 %q", retryAfter)
	}
}

// TestEmailSendIPHourlyLimitGuard 覆盖单 IP 每小时发码上限。
func TestEmailSendIPHourlyLimitGuard(t *testing.T) {
	setting := t8EmailRateLimitSetup(t)
	setting.EmailCooldownSeconds = 0
	setting.EmailIpMaxPerHour = 1
	const ip = "198.51.100.50"
	t8ResetEmailBurstKey(ip)
	engine := t8EmailSendEngine()

	if status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ip, "ip-hour-1@example.com"); status != http.StatusOK || body != "handler-called" {
		t.Fatalf("该 IP 第 1 次发码应放行，实际 %d %q", status, body)
	}
	status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ip, "ip-hour-2@example.com")
	if status != http.StatusTooManyRequests {
		t.Fatalf("达到 email_ip_max_per_hour=1 后第 2 次发码应返回 429，实际 %d %q", status, body)
	}
}

// TestEmailSendGlobalHourlyLimitGuard 覆盖全站每小时发码预算（保护 SMTP）。
func TestEmailSendGlobalHourlyLimitGuard(t *testing.T) {
	setting := t8EmailRateLimitSetup(t)
	setting.EmailCooldownSeconds = 0
	setting.EmailGlobalMaxPerHour = 1
	engine := t8EmailSendEngine()

	const ipA = "198.51.100.60"
	t8ResetEmailBurstKey(ipA)
	if status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ipA, "global-1@example.com"); status != http.StatusOK || body != "handler-called" {
		t.Fatalf("第 1 次发码应放行，实际 %d %q", status, body)
	}
	const ipB = "198.51.100.61"
	t8ResetEmailBurstKey(ipB)
	status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ipB, "global-2@example.com")
	if status != http.StatusTooManyRequests {
		t.Fatalf("达到 email_global_max_per_hour=1 后第 2 次发码应返回 429，实际 %d %q", status, body)
	}
}

// TestEmailSendPurposeIsolation 覆盖 purpose 由路径推导：
// /api/verification(“v”) 与 /api/reset_password(“r”) 使用独立的邮箱冷却键。
func TestEmailSendPurposeIsolation(t *testing.T) {
	setting := t8EmailRateLimitSetup(t)
	setting.EmailCooldownSeconds = 60
	engine := t8EmailSendEngine()

	const email = "purpose@example.com"
	ipV := "198.51.100.70"
	ipR := "198.51.100.71"
	ipV2 := "198.51.100.72"
	t8ResetEmailBurstKey(ipV)
	t8ResetEmailBurstKey(ipR)
	t8ResetEmailBurstKey(ipV2)

	if status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ipV, email); status != http.StatusOK || body != "handler-called" {
		t.Fatalf("注册验证码发码应放行，实际 %d %q", status, body)
	}
	if status, body, _ := t8EmailSendRequest(t, engine, "/api/reset_password", ipR, email); status != http.StatusOK || body != "handler-called" {
		t.Fatalf("同一邮箱的密码重置发码应使用独立 purpose 键而放行，实际 %d %q", status, body)
	}
	// 同一 purpose 的第二次发码仍受冷却限制。
	if status, body, _ := t8EmailSendRequest(t, engine, "/api/verification", ipV2, email); status != http.StatusTooManyRequests {
		t.Fatalf("同一 purpose 冷却期内二次发码应返回 429，实际 %d %q", status, body)
	}
}
