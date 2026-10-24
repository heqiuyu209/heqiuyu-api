package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/middleware"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// t8RegistrationEnv 是注册防护 e2e 的隔离环境：
// 真实 SetApiRouter + 真实中间件 + 内存 sqlite，且不连接 Redis/SMTP。
type t8RegistrationEnv struct {
	t       *testing.T
	server  *httptest.Server
	client  *http.Client
	db      *gorm.DB
	setting *operation_setting.RegistrationSetting
}

func t8DefaultRegistrationSettingForTest() operation_setting.RegistrationSetting {
	// 与 setting/operation_setting/registration_setting.go 的 24 项默认值一致。
	return operation_setting.RegistrationSetting{
		Enabled:                          true,
		TurnstileStrict:                  true,
		IpAttemptWindowSeconds:           600,
		IpMaxAttemptsPerWindow:           10,
		IpMaxSuccessPerHour:              5,
		IpMaxSuccessPerDay:               20,
		IpAllowlist:                      "",
		GlobalMaxSuccessPerHour:          0,
		EmailCooldownSeconds:             60,
		EmailMaxPerDay:                   5,
		EmailIpMaxPerHour:                10,
		EmailGlobalMaxPerHour:            120,
		VerificationMaxAttempts:          5,
		VerificationAttemptWindowSeconds: 600,
		HoneypotEnabled:                  true,
		FormTokenEnabled:                 false,
		FormTokenMinSeconds:              3,
		FormTokenMaxMinutes:              30,
		AsyncEmailEnabled:                true,
		AsyncEmailQueueSize:              256,
		AsyncEmailWorkers:                2,
		AsyncEmailMaxPerMinute:           60,
		AsyncEmailBreakerFailures:        5,
		InviteCodeRequired:               false,
	}
}

func t8NewRegistrationEnv(t *testing.T) *t8RegistrationEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 429/400 响应文案依赖 i18n.T，必须先初始化（sync.Once，重复调用安全）。
	if err := i18n.Init(); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}

	prevSQLite, prevMySQL, prevPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	// common.RedisEnabled 必须在 SetApiRouter 之前设置：中间件工厂在注册路由时
	// 就决定走 Redis 还是内存实现。
	prevRedisEnabled := common.RedisEnabled
	prevMemoryCache := common.MemoryCacheEnabled
	prevRegisterEnabled := common.RegisterEnabled
	prevPasswordRegisterEnabled := common.PasswordRegisterEnabled
	prevEmailVerificationEnabled := common.EmailVerificationEnabled
	prevTurnstileCheckEnabled := common.TurnstileCheckEnabled
	prevDB, prevLogDB := model.DB, model.LOG_DB

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false
	common.TurnstileCheckEnabled = false

	prevMailSetting := operation_setting.GetRegistrationSetting()
	savedSetting := *prevMailSetting
	*prevMailSetting = t8DefaultRegistrationSettingForTest()

	service.ResetRegistrationGuardForTests()
	service.ResetMailQueueForTests()
	service.SetMailSenderForTest(func(subject, to, content string) error { return nil })

	t.Cleanup(func() {
		// 清理顺序：先停队列/还原 guard（避免 worker 落到真实 SMTP），再还原全局变量。
		service.SetMailSenderForTest(func(subject, to, content string) error { return nil })
		time.Sleep(20 * time.Millisecond)
		service.ResetMailQueueForTests()
		service.ResetRegistrationGuardForTests()
		*prevMailSetting = savedSetting
		common.UsingSQLite = prevSQLite
		common.UsingMySQL = prevMySQL
		common.UsingPostgreSQL = prevPostgres
		common.RedisEnabled = prevRedisEnabled
		common.MemoryCacheEnabled = prevMemoryCache
		common.RegisterEnabled = prevRegisterEnabled
		common.PasswordRegisterEnabled = prevPasswordRegisterEnabled
		common.EmailVerificationEnabled = prevEmailVerificationEnabled
		common.TurnstileCheckEnabled = prevTurnstileCheckEnabled
		model.DB, model.LOG_DB = prevDB, prevLogDB
	})

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.User{}, &model.Log{}, &model.Token{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("t8-registration-e2e-secret"))))
	SetApiRouter(engine)

	// 测试专用登录路由：直接往 session 写入用户 id（EmailBind 等接口依赖 sessions）。
	engine.GET("/__t8_login/:id", func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.String(http.StatusBadRequest, "bad id")
			return
		}
		s := sessions.Default(c)
		s.Set("id", id)
		if err := s.Save(); err != nil {
			c.String(http.StatusInternalServerError, "save session: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	})

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookiejar: %v", err)
	}

	return &t8RegistrationEnv{
		t:       t,
		server:  server,
		client:  &http.Client{Jar: jar, Timeout: 30 * time.Second},
		db:      db,
		setting: prevMailSetting,
	}
}

func (e *t8RegistrationEnv) request(method, path, body, ip string) (int, string, http.Header) {
	e.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		e.t.Fatalf("new request %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatalf("read response body: %v", err)
	}
	return res.StatusCode, string(raw), res.Header
}

func (e *t8RegistrationEnv) register(ip string, payload map[string]any) (int, string, http.Header) {
	e.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		e.t.Fatalf("marshal register payload: %v", err)
	}
	return e.request(http.MethodPost, "/api/user/register", string(raw), ip)
}

func (e *t8RegistrationEnv) get(path, ip string) (int, string, http.Header) {
	e.t.Helper()
	return e.request(http.MethodGet, path, "", ip)
}

// loginAs 通过测试专用路由把指定用户 id 写入 session（用于依赖登录态的接口，如 EmailBind）。
func (e *t8RegistrationEnv) loginAs(userId int) {
	e.t.Helper()
	status, body, _ := e.get("/__t8_login/"+strconv.Itoa(userId), "")
	if status != http.StatusOK {
		e.t.Fatalf("login as %d failed: %d %s", userId, status, body)
	}
}

func (e *t8RegistrationEnv) userCount() int64 {
	e.t.Helper()
	var count int64
	if err := e.db.Model(&model.User{}).Count(&count).Error; err != nil {
		e.t.Fatalf("count users: %v", err)
	}
	return count
}

func t8RegisterPayload(username string, extra map[string]any) map[string]any {
	payload := map[string]any{
		"username": username,
		"password": "Password123",
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func t8JSONObject(t *testing.T, body string) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatalf("response is not JSON: %v; body=%s", err, body)
	}
	return obj
}

func t8JSONSuccess(t *testing.T, body string) bool {
	t.Helper()
	success, _ := t8JSONObject(t, body)["success"].(bool)
	return success
}

func t8JSONData(t *testing.T, body string) map[string]any {
	t.Helper()
	data, _ := t8JSONObject(t, body)["data"].(map[string]any)
	return data
}

func t8RetryAfter(t *testing.T, header http.Header) int {
	t.Helper()
	raw := header.Get("Retry-After")
	if raw == "" {
		t.Fatalf("响应缺少 Retry-After 头")
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		t.Fatalf("Retry-After 必须是正整数秒，实际 %q", raw)
	}
	return seconds
}

func t8ExtractVerificationCode(t *testing.T, content string) string {
	t.Helper()
	const openTag = "<strong>"
	const closeTag = "</strong>"
	start := strings.Index(content, openTag)
	end := strings.Index(content, closeTag)
	if start < 0 || end <= start {
		t.Fatalf("邮件内容中未找到验证码: %s", content)
	}
	return content[start+len(openTag) : end]
}

// TestRegistrationRateLimitE2E11thAttempt 覆盖验收标准第 1 条：
// 默认阈值下单 IP 前 10 次注册请求不被限流，第 11 次返回 429 + Retry-After。
func TestRegistrationRateLimitE2E11thAttempt(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.IpAttemptWindowSeconds = 600
	env.setting.IpMaxAttemptsPerWindow = 10
	// 隔离“尝试窗口”这一个维度：关闭成功计数上限。
	env.setting.IpMaxSuccessPerHour = 0
	env.setting.IpMaxSuccessPerDay = 0
	env.setting.GlobalMaxSuccessPerHour = 0

	const ip = "203.0.113.10"
	for attempt := 1; attempt <= 10; attempt++ {
		status, body, _ := env.register(ip, t8RegisterPayload("t8ratelimit", nil))
		if status == http.StatusTooManyRequests {
			t.Fatalf("第 %d 次注册不应被限流，实际 429: %s", attempt, body)
		}
		t.Logf("attempt %2d -> %d %s", attempt, status, t8TruncateForLog(body, 120))
	}

	status, body, header := env.register(ip, t8RegisterPayload("t8ratelimit", nil))
	if status != http.StatusTooManyRequests {
		t.Fatalf("第 11 次注册应返回 429，实际 %d %s", status, body)
	}
	if t8JSONSuccess(t, body) {
		t.Errorf("429 响应体不应为 success:true: %s", body)
	}
	if retryAfter := t8RetryAfter(t, header); retryAfter > 600 {
		t.Errorf("Retry-After 不应超过尝试窗口 600 秒，实际 %d", retryAfter)
	}

	// 另一个 IP 不受影响。
	status, body, _ = env.register("203.0.113.11", t8RegisterPayload("t8ratelimit-other", nil))
	if status == http.StatusTooManyRequests {
		t.Errorf("不同 IP 不应被该窗口限流，实际 429: %s", body)
	}
}

// TestRegistrationSuccessRecordsCountersE2E 覆盖验收标准第 1 条的后半：
// 成功注册会回写成功计数，下一次注册受 ip_max_success_per_hour 约束。
func TestRegistrationSuccessRecordsCountersE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.IpMaxAttemptsPerWindow = 0 // 只验证成功计数
	env.setting.IpMaxSuccessPerHour = 1
	env.setting.IpMaxSuccessPerDay = 0
	env.setting.GlobalMaxSuccessPerHour = 0

	const ip = "203.0.113.20"
	status, body, _ := env.register(ip, t8RegisterPayload("t8success", nil))
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("首次注册应成功，实际 %d %s", status, body)
	}
	if count := env.userCount(); count != 1 {
		t.Fatalf("注册成功后应写入 1 条用户记录，实际 %d", count)
	}

	status, body, header := env.register(ip, t8RegisterPayload("t8success2", nil))
	if status != http.StatusTooManyRequests {
		t.Fatalf("达到 ip_max_success_per_hour=1 后再次注册应返回 429，实际 %d %s", status, body)
	}
	if retryAfter := t8RetryAfter(t, header); retryAfter > 3600 {
		t.Errorf("Retry-After 不应超过 3600 秒，实际 %d", retryAfter)
	}

	// 不同 IP 仍可注册。
	status, body, _ = env.register("203.0.113.21", t8RegisterPayload("t8success-other", nil))
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Errorf("不同 IP 的注册应成功，实际 %d %s", status, body)
	}
}

// TestRegistrationHoneypotE2E 覆盖验收标准：蜜罐字段非空被拒（400 + registration_rejected），不落库。
func TestRegistrationHoneypotE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.HoneypotEnabled = true

	const ip = "203.0.113.30"
	status, body, _ := env.register(ip, t8RegisterPayload("t8honeypot", map[string]any{
		"website": "https://spam.example.com",
	}))
	if status != http.StatusBadRequest {
		t.Fatalf("蜜罐非空应返回 400，实际 %d %s", status, body)
	}
	if t8JSONSuccess(t, body) {
		t.Errorf("蜜罐拒绝的响应不应为 success:true: %s", body)
	}
	if strings.Contains(body, i18n.MsgUserRegistrationRejected) {
		t.Errorf("拒绝文案应使用 i18n 翻译而不是原始 key: %s", body)
	}
	if count := env.userCount(); count != 0 {
		t.Errorf("蜜罐拒绝后不应写入用户记录，实际 %d 条", count)
	}

	// 蜜罐为空时正常放行。
	status, body, _ = env.register(ip, t8RegisterPayload("t8honeypot-ok", map[string]any{
		"website": "",
	}))
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Errorf("蜜罐为空时注册应成功，实际 %d %s", status, body)
	}
}

// TestRegistrationFormTokenDisabledE2E 覆盖 form_token_enabled=false：
// 注册不需要 token，取 token 接口返回 404。
func TestRegistrationFormTokenDisabledE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.FormTokenEnabled = false

	status, body, _ := env.get("/api/user/register/token", "203.0.113.40")
	if status != http.StatusNotFound {
		t.Errorf("form_token_enabled=false 时 GET /api/user/register/token 应返回 404，实际 %d %s", status, body)
	}

	status, body, _ = env.register("203.0.113.40", t8RegisterPayload("t8notoken", nil))
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("form_token_enabled=false 时无需 form_token 即可注册，实际 %d %s", status, body)
	}
}

// TestRegistrationFormTokenEnabledE2E 覆盖 form_token_enabled=true：
// 缺 token / 篡改 / 重放均被拒，合法一次性 token 可注册；关闭时 token 接口 404。
func TestRegistrationFormTokenEnabledE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.FormTokenEnabled = true
	env.setting.FormTokenMinSeconds = 0 // e2e 无法等待 3 秒，且最短时间是 service 单测覆盖项
	env.setting.IpMaxAttemptsPerWindow = 0

	const ip = "203.0.113.50"
	status, body, _ := env.get("/api/user/register/token", ip)
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("form_token_enabled=true 时应能取到表单令牌，实际 %d %s", status, body)
	}
	data := t8JSONData(t, body)
	token, _ := data["token"].(string)
	if token == "" {
		t.Fatalf("注册表单令牌不应为空: %s", body)
	}
	if minSeconds, _ := data["min_seconds"].(float64); int(minSeconds) != 0 {
		t.Errorf("min_seconds 应与 form_token_min_seconds=0 一致，实际 %v", data["min_seconds"])
	}
	if expiresIn, _ := data["expires_in"].(float64); int(expiresIn) != 30*60 {
		t.Errorf("expires_in 应为 1800，实际 %v", data["expires_in"])
	}

	// 1) 缺 token → 400。
	status, body, _ = env.register(ip, t8RegisterPayload("t8token-missing", nil))
	if status != http.StatusBadRequest {
		t.Fatalf("form_token_enabled=true 时缺 token 应返回 400，实际 %d %s", status, body)
	}
	// 2) 篡改签名 → 400。
	status, body, _ = env.register(ip, t8RegisterPayload("t8token-tampered", map[string]any{
		"form_token": "v1:1234567890:tampered-nonce:deadbeef",
	}))
	if status != http.StatusBadRequest {
		t.Fatalf("篡改表单令牌应返回 400，实际 %d %s", status, body)
	}
	// 3) 合法 token → 注册成功。
	status, body, _ = env.register(ip, t8RegisterPayload("t8token-ok", map[string]any{
		"form_token": token,
	}))
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("合法表单令牌应注册成功，实际 %d %s", status, body)
	}
	// 4) 重放同一 token → 400。
	status, body, _ = env.register(ip, t8RegisterPayload("t8token-replay", map[string]any{
		"form_token": token,
	}))
	if status != http.StatusBadRequest {
		t.Fatalf("重放已消费的表单令牌应返回 400，实际 %d %s", status, body)
	}

	// 5) 注册防护总开关关闭时，取 token 接口返回 404。
	env.setting.Enabled = false
	status, body, _ = env.get("/api/user/register/token", ip)
	if status != http.StatusNotFound {
		t.Errorf("registration_setting.enabled=false 时 GET /api/user/register/token 应返回 404，实际 %d %s", status, body)
	}
}

// TestRegistrationVerificationLockoutE2E 覆盖验收标准：验证码错误 5 次锁定为 429，
// 锁定时验证码被删除（窗口内即使猜中也无法使用）。
func TestRegistrationVerificationLockoutE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	common.EmailVerificationEnabled = true
	env.setting.IpMaxAttemptsPerWindow = 0
	env.setting.VerificationMaxAttempts = 5
	env.setting.VerificationAttemptWindowSeconds = 600

	const email = "lock-e2e@example.com"
	const correctCode = "12345678"
	common.RegisterVerificationCodeWithKey(email, correctCode, common.EmailVerificationPurpose)

	const ip = "203.0.113.60"
	wrong := t8RegisterPayload("t8lock", map[string]any{
		"email":             email,
		"verification_code": "00000000",
	})
	for attempt := 1; attempt <= 4; attempt++ {
		status, body, _ := env.register(ip, wrong)
		if status == http.StatusTooManyRequests {
			t.Fatalf("第 %d 次验证码错误不应锁定，实际 429: %s", attempt, body)
		}
		if t8JSONSuccess(t, body) {
			t.Fatalf("第 %d 次验证码错误不应注册成功: %s", attempt, body)
		}
	}

	status, body, header := env.register(ip, wrong)
	if status != http.StatusTooManyRequests {
		t.Fatalf("第 5 次验证码错误应返回 429 锁定，实际 %d %s", status, body)
	}
	if retryAfter := t8RetryAfter(t, header); retryAfter <= 0 || retryAfter > 600 {
		t.Errorf("锁定 Retry-After 应在 (0,600] 内，实际 %d", retryAfter)
	}
	if common.VerifyCodeWithKey(email, correctCode, common.EmailVerificationPurpose) {
		t.Errorf("锁定后验证码应被删除，实际仍然有效（窗口内可继续爆破）")
	}
}

// TestRegistrationSuccessDeletesVerificationCodeE2E 覆盖验收标准：
// 成功注册后验证码被删除，同一验证码不可重放注册。
func TestRegistrationSuccessDeletesVerificationCodeE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	common.EmailVerificationEnabled = true
	env.setting.IpMaxAttemptsPerWindow = 0

	const email = "success-code@example.com"
	const code = "87654321"
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)

	const ip = "203.0.113.70"
	status, body, _ := env.register(ip, t8RegisterPayload("t8code-ok", map[string]any{
		"email":             email,
		"verification_code": code,
	}))
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("正确验证码应注册成功，实际 %d %s", status, body)
	}
	if common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose) {
		t.Errorf("成功注册后验证码应被删除，实际仍然有效")
	}

	// 重放同一验证码（换用户名）不应成功。
	status, body, _ = env.register(ip, t8RegisterPayload("t8code-replay", map[string]any{
		"email":             email,
		"verification_code": code,
	}))
	if status == http.StatusOK && t8JSONSuccess(t, body) {
		t.Errorf("已删除的验证码不应可重放注册: %d %s", status, body)
	}
}

// TestVerificationEmailFailureDeletesCodeE2E 覆盖验收标准“SMTP 失败时不悬挂”：
// 发码接口先存码再入队；入队失败（同步发送失败）时删除验证码并返回 503。
func TestVerificationEmailFailureDeletesCodeE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.AsyncEmailEnabled = false // 同步路径便于捕获本次发送的验证码
	env.setting.EmailCooldownSeconds = 0

	var (
		mu              sync.Mutex
		capturedContent string
		capturedTo      string
	)
	service.SetMailSenderForTest(func(subject, to, content string) error {
		mu.Lock()
		capturedContent = content
		capturedTo = to
		mu.Unlock()
		return fmt.Errorf("t8 fake smtp failure")
	})

	// 1) 发送失败 → 503 且验证码被删除。
	status, body, _ := env.get("/api/verification?email=queue-fail@example.com", "203.0.113.80")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("邮件发送失败应返回 503，实际 %d %s", status, body)
	}
	mu.Lock()
	failedContent := capturedContent
	failedTo := capturedTo
	mu.Unlock()
	if failedTo != "queue-fail@example.com" {
		t.Errorf("fake sender 收到的收件人不正确: %q", failedTo)
	}
	if failedContent == "" {
		t.Fatalf("未能捕获邮件内容，无法验证“先存码后入队”")
	}
	// 在测试 goroutine 解析验证码（假 sender 运行于 HTTP handler goroutine，
	// 不能在那里调用 t.Fatalf，否则会跨 goroutine 终止测试）。
	failedCode := t8ExtractVerificationCode(t, failedContent)
	// 发送前验证码已存入（sender 能看到它），发送失败后必须删除。
	if common.VerifyCodeWithKey("queue-fail@example.com", failedCode, common.EmailVerificationPurpose) {
		t.Errorf("入队/发送失败后验证码应被删除，实际仍然有效")
	}

	// 2) 发送成功 → 200 且验证码有效（证明先存码后入队、成功后保留）。
	service.SetMailSenderForTest(func(subject, to, content string) error {
		mu.Lock()
		capturedContent = content
		mu.Unlock()
		return nil
	})
	status, body, _ = env.get("/api/verification?email=queue-ok@example.com", "203.0.113.81")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("邮件发送成功时应返回 200 success:true，实际 %d %s", status, body)
	}
	mu.Lock()
	okContent := capturedContent
	mu.Unlock()
	if okContent == "" {
		t.Fatalf("未能捕获成功发送的邮件内容")
	}
	okCode := t8ExtractVerificationCode(t, okContent)
	if !common.VerifyCodeWithKey("queue-ok@example.com", okCode, common.EmailVerificationPurpose) {
		t.Errorf("发送成功后验证码应保留（供注册/绑定使用）")
	}
}

// TestStatusIncludesRegistrationFieldsE2E 覆盖 2.1/2.4 契约：
// /api/status 暴露 5 个注册防护字段，并随配置热变化。
func TestStatusIncludesRegistrationFieldsE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	env.setting.FormTokenEnabled = false
	env.setting.HoneypotEnabled = true

	check := func() map[string]any {
		t.Helper()
		status, body, _ := env.get("/api/status", "203.0.113.90")
		if status != http.StatusOK {
			t.Fatalf("GET /api/status 应返回 200，实际 %d %s", status, body)
		}
		data := t8JSONData(t, body)
		for _, key := range []string{
			"register_enabled",
			"password_register_enabled",
			"oauth_register_enabled",
			"registration_form_token_enabled",
			"registration_honeypot_enabled",
		} {
			if _, ok := data[key]; !ok {
				t.Fatalf("/api/status 缺少字段 %q: %s", key, body)
			}
		}
		return data
	}

	data := check()
	if enabled, _ := data["register_enabled"].(bool); !enabled {
		t.Errorf("register_enabled 应为 true")
	}
	if enabled, _ := data["password_register_enabled"].(bool); !enabled {
		t.Errorf("password_register_enabled 应为 true")
	}
	if enabled, _ := data["oauth_register_enabled"].(bool); !enabled {
		t.Errorf("oauth_register_enabled 应为 true")
	}
	if enabled, _ := data["registration_form_token_enabled"].(bool); enabled {
		t.Errorf("registration_form_token_enabled 应为 false")
	}
	if enabled, _ := data["registration_honeypot_enabled"].(bool); !enabled {
		t.Errorf("registration_honeypot_enabled 应为 true")
	}

	// 配置热变化立即反映到 /api/status。
	env.setting.FormTokenEnabled = true
	env.setting.HoneypotEnabled = false
	data = check()
	if enabled, _ := data["registration_form_token_enabled"].(bool); !enabled {
		t.Errorf("form_token_enabled=true 时 registration_form_token_enabled 应为 true")
	}
	if enabled, _ := data["registration_honeypot_enabled"].(bool); enabled {
		t.Errorf("honeypot_enabled=false 时 registration_honeypot_enabled 应为 false")
	}

	// 注册总开关关闭时相关字段必须为 false。
	common.RegisterEnabled = false
	data = check()
	for _, key := range []string{"register_enabled", "password_register_enabled", "oauth_register_enabled"} {
		if enabled, _ := data[key].(bool); enabled {
			t.Errorf("RegisterEnabled=false 时 %s 应为 false", key)
		}
	}
}

// t8StartTurnstileVerifyServer 启动 httptest 版 siteverify：only validToken 返回 success。
func t8StartTurnstileVerifyServer(t *testing.T, validToken string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("response") == validToken {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	prevURL := middleware.TurnstileVerifyURL
	middleware.TurnstileVerifyURL = server.URL
	t.Cleanup(func() {
		middleware.TurnstileVerifyURL = prevURL
		server.Close()
	})
	return server
}

// TestVerificationTurnstileBeforeEmailQuotaE2E 覆盖 R2 §1 路由顺序：
// /api/verification、/api/reset_password 先 TurnstileCheckStrict 后 EmailSendRateLimit，
// Turnstile 失败不消耗目标邮箱的发码配额（冷却/日上限/全站预算）。
func TestVerificationTurnstileBeforeEmailQuotaE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.TurnstileStrict = true
	env.setting.EmailCooldownSeconds = 60
	env.setting.EmailMaxPerDay = 5
	env.setting.EmailIpMaxPerHour = 10
	env.setting.EmailGlobalMaxPerHour = 120
	t8StartTurnstileVerifyServer(t, "good-token")
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = false })

	// 1) /api/verification：Turnstile 失败被拒，且不得消耗邮箱配额。
	const email = "order-v@example.com"
	const ip = "203.0.113.130"
	status, body, _ := env.get("/api/verification?email="+url.QueryEscape(email)+"&turnstile=bad-token", ip)
	if status != http.StatusOK || t8JSONSuccess(t, body) {
		t.Fatalf("Turnstile 失败应被拒（基线返回 200 success:false），实际 %d %s", status, body)
	}
	if !strings.Contains(body, "Turnstile") {
		t.Errorf("响应应包含 Turnstile 失败文案，实际: %s", body)
	}

	// 2) 关闭 Turnstile 后同一邮箱首次发码应成功；
	//    若失败请求已消耗配额，这里会拿到 429 冷却/日上限。
	common.TurnstileCheckEnabled = false
	status, body, _ = env.get("/api/verification?email="+url.QueryEscape(email), ip)
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("Turnstile 失败不应消耗邮箱配额，关闭 Turnstile 后首次发码应成功，实际 %d %s", status, body)
	}

	// 3) 紧接着第二次同邮箱发码应 429 冷却（证明配额从第 2 步才开始消耗）。
	status, body, _ = env.get("/api/verification?email="+url.QueryEscape(email), ip)
	if status != http.StatusTooManyRequests {
		t.Fatalf("同邮箱 60 秒内二次发码应返回 429（证明配额已消耗），实际 %d %s", status, body)
	}

	// 4) /api/reset_password 同样先 Turnstile 后配额。
	const resetEmail = "order-r@example.com"
	const resetIP = "203.0.113.131"
	common.TurnstileCheckEnabled = true
	status, body, _ = env.get("/api/reset_password?email="+url.QueryEscape(resetEmail)+"&turnstile=bad-token", resetIP)
	if status != http.StatusOK || t8JSONSuccess(t, body) {
		t.Fatalf("reset 路由 Turnstile 失败应被拒，实际 %d %s", status, body)
	}
	common.TurnstileCheckEnabled = false
	status, body, _ = env.get("/api/reset_password?email="+url.QueryEscape(resetEmail), resetIP)
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("reset 路由 Turnstile 失败不应消耗邮箱配额，实际 %d %s", status, body)
	}
}

// TestRegistrationEmailCaseInsensitiveE2E 覆盖 R2 §3 邮箱大小写不敏感：
// 历史大写邮箱可被占用检查/存在检查识别；重复注册被拒；重置流程能识别大写历史数据。
func TestRegistrationEmailCaseInsensitiveE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.IpMaxAttemptsPerWindow = 0

	hashed, err := common.Password2Hash("OldPass1234")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	historical := model.User{
		Username: "old-email-user",
		Password: hashed,
		Email:    "Old@Example.COM", // 历史数据（大写）
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "OLD1",
	}
	if err := env.db.Create(&historical).Error; err != nil {
		t.Fatalf("create historical user: %v", err)
	}

	// model 层：大小写/空格不敏感识别占用与存在。
	if !model.IsEmailAlreadyTaken("old@example.com") {
		t.Errorf("IsEmailAlreadyTaken 应识别历史大写邮箱（old@example.com）")
	}
	if !model.IsEmailAlreadyTaken("  OLD@EXAMPLE.COM  ") {
		t.Errorf("IsEmailAlreadyTaken 应先 trim + lower 再匹配")
	}
	if taken, err := model.CheckUserExistOrDeleted("no-such-user", "Old@Example.Com"); err != nil || !taken {
		t.Errorf("CheckUserExistOrDeleted 应识别历史大写邮箱，taken=%v err=%v", taken, err)
	}

	// 注册同邮箱（大小写不同、用户名不同）→ 应被拒绝，且不落库。
	const ip = "203.0.113.140"
	status, body, _ := env.register(ip, t8RegisterPayload("new-email-user", map[string]any{
		"email": "old@example.com",
	}))
	if status != http.StatusOK || t8JSONSuccess(t, body) {
		t.Fatalf("大小写不同的重复邮箱注册应被拒绝，实际 %d %s", status, body)
	}
	if count := env.userCount(); count != 1 {
		t.Errorf("重复邮箱注册不应落库，实际用户数 %d", count)
	}

	// 重置流程（LOWER(email) 查询）能识别历史大写邮箱。
	const resetCode = "RESETCODE-1"
	common.RegisterVerificationCodeWithKey("old@example.com", resetCode, common.PasswordResetPurpose)
	resetPayload, err := json.Marshal(map[string]any{
		"email":        "old@example.com",
		"token":        resetCode,
		"new_password": "NewPass5678",
	})
	if err != nil {
		t.Fatalf("marshal reset payload: %v", err)
	}
	status, body, _ = env.request(http.MethodPost, "/api/user/reset", string(resetPayload), "203.0.113.141")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("重置流程应能识别历史大写邮箱，实际 %d %s", status, body)
	}
	var afterReset model.User
	if err := env.db.Where("id = ?", historical.Id).First(&afterReset).Error; err != nil {
		t.Fatalf("reload historical user: %v", err)
	}
	if !common.ValidatePasswordAndHash("NewPass5678", afterReset.Password) {
		t.Errorf("历史大写邮箱用户的密码未被重置")
	}
}

// TestEmailBindSuccessDeletesCodeE2E 覆盖 R2 §4：EmailBind 成功后验证码被删除（一次性）。
func TestEmailBindSuccessDeletesCodeE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)

	hashed, err := common.Password2Hash("BindPass1234")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := model.User{
		Username: "bind-user",
		Password: hashed,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "BIND",
	}
	if err := env.db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	env.loginAs(user.Id)

	const email = "bind@example.com"
	const code = "BINDCODE-1234"
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)

	payload, err := json.Marshal(map[string]any{
		"email": "Bind@Example.COM", // 大小写混合，controller 会规范化
		"code":  code,
	})
	if err != nil {
		t.Fatalf("marshal bind payload: %v", err)
	}
	status, body, _ := env.request(http.MethodPost, "/api/oauth/email/bind", string(payload), "203.0.113.150")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("EmailBind 应成功，实际 %d %s", status, body)
	}

	var bound model.User
	if err := env.db.Where("id = ?", user.Id).First(&bound).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if bound.Email != email {
		t.Errorf("绑定后邮箱应为规范化小写 %q，实际 %q", email, bound.Email)
	}
	if common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose) {
		t.Errorf("EmailBind 成功后验证码应被删除（一次性）")
	}

	// 重放已消费的验证码（换目标邮箱）→ 绑定失败。
	replayPayload, err := json.Marshal(map[string]any{
		"email": "bind-replay@example.com",
		"code":  code,
	})
	if err != nil {
		t.Fatalf("marshal replay payload: %v", err)
	}
	status, body, _ = env.request(http.MethodPost, "/api/oauth/email/bind", string(replayPayload), "203.0.113.150")
	if status == http.StatusOK && t8JSONSuccess(t, body) {
		t.Errorf("重放已消费的验证码不应绑定成功: %d %s", status, body)
	}
}

func t8TruncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
