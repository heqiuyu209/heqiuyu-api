package service

import (
	"runtime"
	"testing"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// t8GuardSetup 为每个用例准备一份确定性的注册防护配置与干净的内存限流状态。
//
// 配置是全局指针（后台可热改），用例结束后必须恢复；内存限流器同样是进程级全局，
// 每个用例开始/结束都重置，避免测试之间互相污染。
func t8GuardSetup(t *testing.T) *operation_setting.RegistrationSetting {
	t.Helper()

	prevRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false // 本机无 Redis：统一走内存实现
	t.Cleanup(func() { common.RedisEnabled = prevRedisEnabled })

	setting := operation_setting.GetRegistrationSetting()
	saved := *setting
	t.Cleanup(func() { *setting = saved })

	// 基线：只打开总开关，其余上限全部关闭；用例按需显式设置。
	*setting = operation_setting.RegistrationSetting{Enabled: true}

	ResetRegistrationGuardForTests()
	t.Cleanup(ResetRegistrationGuardForTests)
	return setting
}

// TestRegistrationGuardAttemptWindow 覆盖计划 §5.2/§7：单 IP 窗口内尝试数上限。
func TestRegistrationGuardAttemptWindow(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.IpAttemptWindowSeconds = 600
	setting.IpMaxAttemptsPerWindow = 10

	const ip = "192.0.2.10"
	for attempt := 1; attempt <= 10; attempt++ {
		decision := CheckRegistrationAttempt(ip)
		if !decision.Allowed {
			t.Fatalf("第 %d 次尝试应当放行，实际被拒: %+v", attempt, decision)
		}
	}

	decision := CheckRegistrationAttempt(ip)
	if decision.Allowed {
		t.Fatalf("第 11 次尝试应当被拒（ip_max_attempts_per_window=10），实际放行")
	}
	if decision.Reason != RegistrationDenyAttemptWindow {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyAttemptWindow, decision.Reason)
	}
	if decision.RetryAfterSeconds <= 0 || decision.RetryAfterSeconds > 600 {
		t.Errorf("RetryAfterSeconds 应在 (0,600] 内，实际 %d", decision.RetryAfterSeconds)
	}

	// 另一个 IP 不应受该窗口计数影响。
	other := CheckRegistrationAttempt("192.0.2.11")
	if !other.Allowed {
		t.Fatalf("不同 IP 的首次尝试应当放行，实际被拒: %+v", other)
	}
}

// TestRegistrationGuardIPHourlySuccessLimit 覆盖单 IP 每小时成功注册上限。
func TestRegistrationGuardIPHourlySuccessLimit(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.IpMaxSuccessPerHour = 2

	const ip = "192.0.2.20"
	RecordRegistrationSuccess(ip)
	RecordRegistrationSuccess(ip)

	decision := CheckRegistrationAttempt(ip)
	if decision.Allowed {
		t.Fatalf("达到 ip_max_success_per_hour=2 后应当被拒，实际放行")
	}
	if decision.Reason != RegistrationDenyIPHourlySuccess {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyIPHourlySuccess, decision.Reason)
	}
	if decision.RetryAfterSeconds <= 0 || decision.RetryAfterSeconds > 3600 {
		t.Errorf("RetryAfterSeconds 应在 (0,3600] 内，实际 %d", decision.RetryAfterSeconds)
	}

	if other := CheckRegistrationAttempt("192.0.2.21"); !other.Allowed {
		t.Errorf("其他 IP 不应被单个 IP 的成功计数拦截，实际: %+v", other)
	}
}

// TestRegistrationGuardIPDailySuccessLimit 覆盖单 IP 每天成功注册上限。
func TestRegistrationGuardIPDailySuccessLimit(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.IpMaxSuccessPerDay = 2

	const ip = "192.0.2.30"
	RecordRegistrationSuccess(ip)
	RecordRegistrationSuccess(ip)

	decision := CheckRegistrationAttempt(ip)
	if decision.Allowed {
		t.Fatalf("达到 ip_max_success_per_day=2 后应当被拒，实际放行")
	}
	if decision.Reason != RegistrationDenyIPDailySuccess {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyIPDailySuccess, decision.Reason)
	}
	if decision.RetryAfterSeconds <= 0 || decision.RetryAfterSeconds > 86400 {
		t.Errorf("RetryAfterSeconds 应在 (0,86400] 内，实际 %d", decision.RetryAfterSeconds)
	}
}

// TestRegistrationGuardGlobalHourlySuccessLimit 覆盖全站每小时成功注册上限（0 = 关闭）。
func TestRegistrationGuardGlobalHourlySuccessLimit(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.GlobalMaxSuccessPerHour = 2

	RecordRegistrationSuccess("192.0.2.40")
	RecordRegistrationSuccess("192.0.2.41")

	decision := CheckRegistrationAttempt("192.0.2.42")
	if decision.Allowed {
		t.Fatalf("达到 global_max_success_per_hour=2 后应当被拒，实际放行")
	}
	if decision.Reason != RegistrationDenyGlobalHourlySuccess {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyGlobalHourlySuccess, decision.Reason)
	}

	// 0 表示关闭全站上限。
	setting.GlobalMaxSuccessPerHour = 0
	ResetRegistrationGuardForTests()
	RecordRegistrationSuccess("192.0.2.43")
	RecordRegistrationSuccess("192.0.2.44")
	if decision := CheckRegistrationAttempt("192.0.2.45"); !decision.Allowed {
		t.Errorf("global_max_success_per_hour=0 时应关闭全站上限，实际被拒: %+v", decision)
	}
}

// TestRegistrationGuardAllowlist 覆盖逗号分隔 IP/CIDR 白名单永久放行。
func TestRegistrationGuardAllowlist(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.IpMaxAttemptsPerWindow = 2
	setting.IpMaxSuccessPerHour = 1
	setting.IpAllowlist = "10.0.0.0/8,203.0.113.9,not-an-ip,192.0.2.0/33"

	// CIDR 白名单：远超尝试上限也始终放行。
	for i := 0; i < 10; i++ {
		if decision := CheckRegistrationAttempt("10.1.2.3"); !decision.Allowed {
			t.Fatalf("CIDR 白名单 IP 第 %d 次尝试应当放行，实际: %+v", i+1, decision)
		}
	}
	// 单个 IP 白名单。
	if decision := CheckRegistrationAttempt("203.0.113.9"); !decision.Allowed {
		t.Fatalf("白名单单 IP 应当放行，实际: %+v", decision)
	}
	// 白名单 IP 的成功计数不累计（RecordRegistrationSuccess 直接返回）。
	for i := 0; i < 3; i++ {
		RecordRegistrationSuccess("10.1.2.3")
	}

	// 非法条目被忽略，非白名单 IP 仍受限。
	if decision := CheckRegistrationAttempt("192.0.2.50"); !decision.Allowed {
		t.Fatalf("非白名单 IP 首次尝试应当放行，实际: %+v", decision)
	}
	if decision := CheckRegistrationAttempt("192.0.2.50"); !decision.Allowed {
		t.Fatalf("非白名单 IP 第 2 次尝试应当放行，实际: %+v", decision)
	}
	if decision := CheckRegistrationAttempt("192.0.2.50"); decision.Allowed {
		t.Fatalf("非白名单 IP 第 3 次尝试应当被拒（ip_max_attempts_per_window=2），实际放行")
	}
}

// TestRegistrationGuardDisabled 覆盖总开关关闭时全部放行（一键回滚路径）。
func TestRegistrationGuardDisabled(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.Enabled = false
	setting.IpMaxAttemptsPerWindow = 1
	setting.IpMaxSuccessPerHour = 1

	const ip = "192.0.2.60"
	for i := 0; i < 3; i++ {
		if decision := CheckRegistrationAttempt(ip); !decision.Allowed {
			t.Fatalf("总开关关闭时应全部放行，第 %d 次被拒: %+v", i+1, decision)
		}
	}
	// 关闭时成功计数也不应记录。
	for i := 0; i < 3; i++ {
		RecordRegistrationSuccess(ip)
	}

	// 重新打开后，之前的尝试没有计数，第 1 次放行、第 2 次被拒（上限 1）。
	setting.Enabled = true
	if decision := CheckRegistrationAttempt(ip); !decision.Allowed {
		t.Fatalf("重新打开后首次尝试应当放行，实际: %+v", decision)
	}
	if decision := CheckRegistrationAttempt(ip); decision.Allowed {
		t.Fatalf("重新打开后第 2 次尝试应当被拒（上限 1），实际放行")
	}
}

// TestRegistrationGuardHotReload 覆盖配置热读取：修改配置立即生效，无需重启/重置状态。
func TestRegistrationGuardHotReload(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.IpAttemptWindowSeconds = 600
	setting.IpMaxAttemptsPerWindow = 2

	const ip = "192.0.2.70"
	if d := CheckRegistrationAttempt(ip); !d.Allowed {
		t.Fatalf("第 1 次尝试应放行: %+v", d)
	}
	if d := CheckRegistrationAttempt(ip); !d.Allowed {
		t.Fatalf("第 2 次尝试应放行: %+v", d)
	}
	if d := CheckRegistrationAttempt(ip); d.Allowed {
		t.Fatalf("上限=2 时第 3 次尝试应被拒，实际放行")
	}

	// 热调大上限后应立即放行，且复用同一窗口内的已有计数。
	setting.IpMaxAttemptsPerWindow = 5
	if d := CheckRegistrationAttempt(ip); !d.Allowed {
		t.Fatalf("热调大上限后第 3 次尝试应放行，实际: %+v", d)
	}
	if d := CheckRegistrationAttempt(ip); !d.Allowed {
		t.Fatalf("热调大上限后第 4 次尝试应放行，实际: %+v", d)
	}
	if d := CheckRegistrationAttempt(ip); !d.Allowed {
		t.Fatalf("热调大上限后第 5 次尝试应放行，实际: %+v", d)
	}
	if d := CheckRegistrationAttempt(ip); d.Allowed {
		t.Fatalf("上限=5 时第 6 次尝试应被拒，实际放行")
	}
}

// TestRegistrationGuardEmailCooldown 覆盖同邮箱发码冷却（含规范化与 purpose 隔离）。
func TestRegistrationGuardEmailCooldown(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.EmailCooldownSeconds = 60

	const ip = "192.0.2.80"
	if decision := CheckEmailSend(ip, "User@Example.COM ", "v"); !decision.Allowed {
		t.Fatalf("首次发码应放行，实际: %+v", decision)
	}

	// 大小写/空格规范化后是同一个邮箱 → 冷却期内被拒。
	decision := CheckEmailSend(ip, "  user@example.com", "v")
	if decision.Allowed {
		t.Fatalf("同一邮箱在 60 秒冷却期内二次发码应被拒，实际放行")
	}
	if decision.Reason != RegistrationDenyEmailCooldown {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyEmailCooldown, decision.Reason)
	}
	if decision.RetryAfterSeconds <= 0 || decision.RetryAfterSeconds > 60 {
		t.Errorf("RetryAfterSeconds 应在 (0,60] 内，实际 %d", decision.RetryAfterSeconds)
	}

	// 不同邮箱不受影响。
	if decision := CheckEmailSend(ip, "other@example.com", "v"); !decision.Allowed {
		t.Errorf("不同邮箱应放行，实际: %+v", decision)
	}

	// 不同 purpose（注册验证码 v / 密码重置 r）使用不同冷却键。
	if decision := CheckEmailSend(ip, "user@example.com", "r"); !decision.Allowed {
		t.Errorf("不同 purpose 应使用独立冷却键，实际: %+v", decision)
	}

	// 空邮箱放行（参数校验由 controller 负责）。
	if decision := CheckEmailSend(ip, "", "v"); !decision.Allowed {
		t.Errorf("空邮箱应放行给 controller 校验，实际: %+v", decision)
	}
}

// TestRegistrationGuardEmailDailyLimit 覆盖同邮箱每日发码上限。
func TestRegistrationGuardEmailDailyLimit(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.EmailCooldownSeconds = 0
	setting.EmailMaxPerDay = 2

	const email = "daily@example.com"
	if decision := CheckEmailSend("192.0.2.90", email, "v"); !decision.Allowed {
		t.Fatalf("第 1 次发码应放行，实际: %+v", decision)
	}
	if decision := CheckEmailSend("192.0.2.91", email, "v"); !decision.Allowed {
		t.Fatalf("第 2 次发码应放行，实际: %+v", decision)
	}
	decision := CheckEmailSend("192.0.2.92", email, "v")
	if decision.Allowed {
		t.Fatalf("达到 email_max_per_day=2 后第 3 次发码应被拒，实际放行")
	}
	if decision.Reason != RegistrationDenyEmailDaily {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyEmailDaily, decision.Reason)
	}
	if decision.RetryAfterSeconds <= 0 || decision.RetryAfterSeconds > 86400 {
		t.Errorf("RetryAfterSeconds 应在 (0,86400] 内，实际 %d", decision.RetryAfterSeconds)
	}

	// 换邮箱不受该邮箱日上限影响。
	if decision := CheckEmailSend("192.0.2.93", "another-daily@example.com", "v"); !decision.Allowed {
		t.Errorf("不同邮箱应放行，实际: %+v", decision)
	}
}

// TestRegistrationGuardEmailIPHourlyLimit 覆盖单 IP 每小时发码上限。
func TestRegistrationGuardEmailIPHourlyLimit(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.EmailCooldownSeconds = 0
	setting.EmailIpMaxPerHour = 1

	const ip = "192.0.2.100"
	if decision := CheckEmailSend(ip, "ip-a@example.com", "v"); !decision.Allowed {
		t.Fatalf("该 IP 第 1 次发码应放行，实际: %+v", decision)
	}
	decision := CheckEmailSend(ip, "ip-b@example.com", "v")
	if decision.Allowed {
		t.Fatalf("达到 email_ip_max_per_hour=1 后第 2 次发码应被拒，实际放行")
	}
	if decision.Reason != RegistrationDenyEmailIPHourly {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyEmailIPHourly, decision.Reason)
	}

	// 另一个 IP 不受影响。
	if decision := CheckEmailSend("192.0.2.101", "ip-c@example.com", "v"); !decision.Allowed {
		t.Errorf("不同 IP 应放行，实际: %+v", decision)
	}
}

// TestRegistrationGuardEmailGlobalHourlyLimit 覆盖全站每小时发码预算（保护 SMTP）。
func TestRegistrationGuardEmailGlobalHourlyLimit(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.EmailCooldownSeconds = 0
	setting.EmailGlobalMaxPerHour = 1

	if decision := CheckEmailSend("192.0.2.110", "global-a@example.com", "v"); !decision.Allowed {
		t.Fatalf("第 1 次发码应放行，实际: %+v", decision)
	}
	decision := CheckEmailSend("192.0.2.111", "global-b@example.com", "v")
	if decision.Allowed {
		t.Fatalf("达到 email_global_max_per_hour=1 后第 2 次发码应被拒，实际放行")
	}
	if decision.Reason != RegistrationDenyEmailGlobalHourly {
		t.Errorf("拒绝原因应为 %q，实际 %q", RegistrationDenyEmailGlobalHourly, decision.Reason)
	}
}

// TestRegistrationGuardVerificationLockoutAndClear 覆盖验证码错误 5 次锁定与成功清除。
func TestRegistrationGuardVerificationLockoutAndClear(t *testing.T) {
	setting := t8GuardSetup(t)
	// 计划默认：5 次 / 600 秒窗口。
	setting.VerificationMaxAttempts = 5
	setting.VerificationAttemptWindowSeconds = 600

	const email = "lock@example.com"
	for attempt := 1; attempt <= 4; attempt++ {
		locked, _ := RegisterVerificationFailure(email, common.EmailVerificationPurpose)
		if locked {
			t.Fatalf("第 %d 次验证码错误不应锁定", attempt)
		}
	}
	locked, retryAfter := RegisterVerificationFailure(email, common.EmailVerificationPurpose)
	if !locked {
		t.Fatalf("第 5 次验证码错误应触发锁定")
	}
	if retryAfter <= 0 || retryAfter > 600 {
		t.Errorf("锁定后的 retryAfterSeconds 应在 (0,600] 内，实际 %d", retryAfter)
	}

	// 验证成功后清除错误计数：再错 1 次不应立即锁定。
	ClearVerificationFailures(email, common.EmailVerificationPurpose)
	if locked, _ := RegisterVerificationFailure(email, common.EmailVerificationPurpose); locked {
		t.Fatalf("ClearVerificationFailures 后第 1 次错误不应锁定（说明计数未清除）")
	}

	// purpose 隔离：密码重置的失败计数不影响注册验证码。
	const email2 = "lock-purpose@example.com"
	for attempt := 1; attempt <= 5; attempt++ {
		RegisterVerificationFailure(email2, common.PasswordResetPurpose)
	}
	if locked, _ := RegisterVerificationFailure(email2, common.EmailVerificationPurpose); locked {
		t.Errorf("注册验证码 purpose=\"v\" 不应被密码重置 purpose=\"r\" 的错误计数锁定")
	}
	if locked, _ := RegisterVerificationFailure(email2, common.PasswordResetPurpose); !locked {
		t.Errorf("密码重置 purpose=\"r\" 达到 5 次后应锁定")
	}

	// 大小写/空格规范化后为同一邮箱：清除后又累计 1 次错误，
	// 再补 3 次达到 4 次，第 5 次（本次）应命中同一锁定计数。
	for attempt := 0; attempt < 3; attempt++ {
		if locked, _ := RegisterVerificationFailure("  LOCK@Example.COM ", common.EmailVerificationPurpose); locked {
			t.Fatalf("规范化邮箱第 %d 次补充错误不应提前锁定", attempt+1)
		}
	}
	if locked, _ := RegisterVerificationFailure("  LOCK@Example.COM ", common.EmailVerificationPurpose); !locked {
		t.Errorf("邮箱规范化后应命中同一锁定计数（lock@example.com）")
	}
}

// TestRegistrationGuardRejectsEmptyIdentifiers 覆盖空 IP/空邮箱不误伤（放行交给后续校验）。
func TestRegistrationGuardRejectsEmptyIdentifiers(t *testing.T) {
	setting := t8GuardSetup(t)
	setting.IpMaxAttemptsPerWindow = 1
	setting.EmailCooldownSeconds = 60
	setting.VerificationMaxAttempts = 1

	if decision := CheckRegistrationAttempt("   "); !decision.Allowed {
		t.Errorf("空 IP 应放行，实际: %+v", decision)
	}
	if decision := CheckEmailSend("192.0.2.120", "   ", "v"); !decision.Allowed {
		t.Errorf("空邮箱应放行，实际: %+v", decision)
	}
	if locked, _ := RegisterVerificationFailure("", common.EmailVerificationPurpose); locked {
		t.Errorf("空邮箱的验证码失败不应触发锁定")
	}
}

// TestResetRegistrationGuardForTestsClosesOldLimiter 覆盖 R2-2 §InMemoryRateLimiter 可停止：
// Reset 必须先 Close 旧实例再创建新实例（避免测试期 goroutine 累积）。
func TestResetRegistrationGuardForTestsClosesOldLimiter(t *testing.T) {
	_ = t8GuardSetup(t)

	oldLimiter := guardMemory()
	ResetRegistrationGuardForTests()
	newLimiter := guardMemory()
	if oldLimiter == newLimiter {
		t.Fatalf("ResetRegistrationGuardForTests 应创建新的内存限流器实例")
	}
	// 旧实例已 Close：重复 Close 幂等且不 panic（再次证明 Close 可安全调用）。
	oldLimiter.Close()
	oldLimiter.Close()
}

// TestResetRegistrationGuardForTestsDoesNotLeakGoroutines 覆盖 Reset 后旧清理 goroutine
// 能退出（新实例不再累积 goroutine）。
func TestResetRegistrationGuardForTestsDoesNotLeakGoroutines(t *testing.T) {
	_ = t8GuardSetup(t)

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()

	const rounds = 30
	for i := 0; i < rounds; i++ {
		ResetRegistrationGuardForTests()
	}

	time.Sleep(400 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()
	// 允许少量运行时/测试框架 goroutine 波动，但不应线性增长（旧实现会 +30）。
	if after > before+8 {
		t.Errorf("ResetRegistrationGuardForTests 存在 goroutine 泄漏：before=%d after=%d（%d 次 reset）", before, after, rounds)
	}
}
