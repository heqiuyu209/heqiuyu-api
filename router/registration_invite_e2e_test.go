package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/middleware"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/service"
)

const t30InviteCode = "AbC123"

// t30MailRecorder 记录同步回退路径下的假发信（count/收件人/内容，均加锁）。
type t30MailRecorder struct {
	mu      sync.Mutex
	count   int
	lastTo  string
	content string
}

func (r *t30MailRecorder) sender(subject, to, content string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	r.lastTo = to
	r.content = content
	return nil
}

func (r *t30MailRecorder) snapshot() (count int, to, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count, r.lastTo, r.content
}

// t30InviteEnv 构造"仅限邀请码注册"测试环境：关闭无关限流、同步假发信、种子邀请人。
func t30InviteEnv(t *testing.T, inviteRequired, emailVerification bool) (*t8RegistrationEnv, *model.User, *t30MailRecorder) {
	t.Helper()
	env := t8NewRegistrationEnv(t)

	env.setting.InviteCodeRequired = inviteRequired
	env.setting.IpMaxAttemptsPerWindow = 0
	env.setting.IpMaxSuccessPerHour = 0
	env.setting.IpMaxSuccessPerDay = 0
	env.setting.GlobalMaxSuccessPerHour = 0
	env.setting.EmailCooldownSeconds = 0
	env.setting.EmailMaxPerDay = 0
	env.setting.EmailIpMaxPerHour = 0
	env.setting.EmailGlobalMaxPerHour = 0
	env.setting.HoneypotEnabled = false
	env.setting.AsyncEmailEnabled = false // 同步发送：请求返回时 fake sender 已执行
	common.EmailVerificationEnabled = emailVerification

	middleware.ResetRegistrationInviteAttemptsForTests()
	t.Cleanup(middleware.ResetRegistrationInviteAttemptsForTests)

	recorder := &t30MailRecorder{}
	service.SetMailSenderForTest(recorder.sender)

	inviter := &model.User{
		Username: "invite-inviter",
		Password: "x",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  t30InviteCode,
	}
	if err := env.db.Create(inviter).Error; err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	return env, inviter, recorder
}

// t30Verification 调用 /api/verification；purpose/inviteCode 为空则不携带对应参数。
func (e *t8RegistrationEnv) t30Verification(ip, email, purpose, inviteCode string) (int, string, http.Header) {
	query := url.Values{}
	query.Set("email", email)
	if purpose != "" {
		query.Set("purpose", purpose)
	}
	if inviteCode != "" {
		query.Set("invite_code", inviteCode)
	}
	return e.get("/api/verification?"+query.Encode(), ip)
}

// t30RequireRegisterCode 发码成功并返回捕获到的验证码（同步假发信保证已发送）。
func t30RequireRegisterCode(t *testing.T, env *t8RegistrationEnv, recorder *t30MailRecorder, ip, email, inviteCode string) string {
	t.Helper()
	before, _, _ := recorder.snapshot()
	status, body, _ := env.t30Verification(ip, email, "register", inviteCode)
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("注册发码应成功，实际 %d %s", status, body)
	}
	after, _, content := recorder.snapshot()
	if after != before+1 {
		t.Fatalf("注册发码成功应发送 1 封邮件，实际 %d -> %d", before, after)
	}
	return t8ExtractVerificationCode(t, content)
}

func (e *t8RegistrationEnv) t30Register(ip, username, email, code, inviteCode string) (int, string, http.Header) {
	extra := map[string]any{
		"email":             email,
		"verification_code": code,
	}
	if inviteCode != "" {
		extra["invite_code"] = inviteCode
	}
	return e.register(ip, t8RegisterPayload(username, extra))
}

func t30UserByUsername(t *testing.T, env *t8RegistrationEnv, username string) *model.User {
	t.Helper()
	var user model.User
	if err := env.db.Where("username = ?", username).First(&user).Error; err != nil {
		t.Fatalf("reload user %q: %v", username, err)
	}
	return &user
}

// TestInviteCodeStatusDefaultAndSwitchE2E 覆盖：status 默认 false，开启后为 true，且独立于 enabled。
func TestInviteCodeStatusDefaultAndSwitchE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)

	status := func() bool {
		t.Helper()
		code, body, _ := env.get("/api/status", "203.0.113.210")
		if code != http.StatusOK {
			t.Fatalf("GET /api/status -> %d %s", code, body)
		}
		data := t8JSONData(t, body)
		value, ok := data["invite_code_required"]
		if !ok {
			t.Fatalf("/api/status 缺少 invite_code_required 字段: %s", body)
		}
		required, _ := value.(bool)
		return required
	}

	if status() {
		t.Errorf("invite_code_required 默认应为 false")
	}
	env.setting.InviteCodeRequired = true
	if !status() {
		t.Errorf("invite_code_required=true 时 /api/status 应为 true")
	}
	env.setting.Enabled = false
	if !status() {
		t.Errorf("invite_code_required 应独立于 registration_setting.enabled（enabled=false 仍为 true）")
	}
}

// TestInviteCodeDisabledAllowsRegistrationE2E 覆盖关闭态：无码/错码完全不影响注册。
func TestInviteCodeDisabledAllowsRegistrationE2E(t *testing.T) {
	env, _, recorder := t30InviteEnv(t, false, true)

	// 1) 普通发码（无 purpose）→ "v" 验证码；注册带错误邀请码也应成功，且 inviter 为空。
	status, body, _ := env.t30Verification("203.0.113.211", "closed-a@example.com", "", "")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("关闭态普通发码应成功，实际 %d %s", status, body)
	}
	_, _, content := recorder.snapshot()
	codeV := t8ExtractVerificationCode(t, content)
	if !common.VerifyCodeWithKey("closed-a@example.com", codeV, common.EmailVerificationPurpose) {
		t.Fatalf("普通发码应写入 purpose=v")
	}
	status, body, _ = env.t30Register("203.0.113.211", "closed-a", "closed-a@example.com", codeV, "WRONG-CODE")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("关闭态带错误邀请码仍应注册成功，实际 %d %s", status, body)
	}
	userA := t30UserByUsername(t, env, "closed-a")
	if userA.InviterId != 0 {
		t.Errorf("关闭态错误邀请码不应设置 inviter，实际 %d", userA.InviterId)
	}

	// 2) purpose=register 发码（写入 "vr"），关闭态不校验邀请码；注册可直接用 "vr"。
	status, body, _ = env.t30Verification("203.0.113.212", "closed-b@example.com", "register", "WRONG")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("关闭态 purpose=register 发码不应校验邀请码，实际 %d %s", status, body)
	}
	_, _, content = recorder.snapshot()
	codeVR := t8ExtractVerificationCode(t, content)
	if !common.VerifyCodeWithKey("closed-b@example.com", codeVR, common.RegistrationEmailVerificationPurpose) {
		t.Fatalf("purpose=register 应写入 purpose=vr")
	}
	status, body, _ = env.t30Register("203.0.113.212", "closed-b", "closed-b@example.com", codeVR, "")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("关闭态无邀请码 + vr 码应注册成功，实际 %d %s", status, body)
	}

	// 3) 关闭态即使携带**合法**邀请码，也不得被当作邀请（inviter_id 必须为 0，防回归）。
	const emailC = "closed-c@example.com"
	const ipC = "203.0.113.213"
	status, body, _ = env.t30Verification(ipC, emailC, "", "")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("关闭态普通发码应成功，实际 %d %s", status, body)
	}
	_, _, content = recorder.snapshot()
	codeVC := t8ExtractVerificationCode(t, content)
	status, body, _ = env.t30Register(ipC, "closed-c", emailC, codeVC, t30InviteCode)
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("关闭态携带合法邀请码仍应注册成功，实际 %d %s", status, body)
	}
	userC := t30UserByUsername(t, env, "closed-c")
	if userC.InviterId != 0 {
		t.Errorf("关闭态合法邀请码不得被当作邀请，inviter_id 应为 0，实际 %d", userC.InviterId)
	}
}

// TestInviteCodeEnabledVerificationRequiresCodeE2E 覆盖开启态发码：缺/错码失败且不发信；
// 无效码不消耗邮箱配额；连续 20 次无效后第 21 次 429 + Retry-After 600；有效码不被限速误伤。
func TestInviteCodeEnabledVerificationRequiresCodeE2E(t *testing.T) {
	env, _, recorder := t30InviteEnv(t, true, true)
	env.setting.EmailMaxPerDay = 1 // 用于证明无效邀请码不消耗邮箱配额

	const emailA = "enabled-verify-a@example.com"
	const ipA = "203.0.113.220"
	// 缺码。
	status, body, _ := env.t30Verification(ipA, emailA, "register", "")
	if status != http.StatusOK || t8JSONSuccess(t, body) {
		t.Fatalf("开启态缺邀请码应失败（200 success:false），实际 %d %s", status, body)
	}
	// 错码（含大小写变体）。
	status, body, _ = env.t30Verification(ipA, emailA, "register", "abc123")
	if status != http.StatusOK || t8JSONSuccess(t, body) {
		t.Fatalf("开启态大小写变体邀请码应失败，实际 %d %s", status, body)
	}
	if count, _, _ := recorder.snapshot(); count != 0 {
		t.Fatalf("邀请码校验失败不应发送邮件，实际 %d 封", count)
	}
	// 有效码成功，且验证码写入 "vr"（不是 "v"）。
	code := t30RequireRegisterCode(t, env, recorder, ipA, emailA, t30InviteCode)
	if !common.VerifyCodeWithKey(emailA, code, common.RegistrationEmailVerificationPurpose) {
		t.Fatalf("注册发码应写入 purpose=vr")
	}
	if common.VerifyCodeWithKey(emailA, code, common.EmailVerificationPurpose) {
		t.Errorf("注册发码不应同时写入 purpose=v")
	}

	// 连续 20 次无效邀请码（新 IP/邮箱），不应发信、不应提前 429。
	const emailB = "enabled-verify-b@example.com"
	const ipB = "203.0.113.221"
	for i := 1; i <= 20; i++ {
		status, body, _ = env.t30Verification(ipB, emailB, "register", fmt.Sprintf("BAD%02d", i))
		if status != http.StatusOK || t8JSONSuccess(t, body) {
			t.Fatalf("第 %d 次无效邀请码应 200 success:false，实际 %d %s", i, status, body)
		}
	}
	// 第 21 次无效 → 429 + Retry-After 600。
	status, body, header := env.t30Verification(ipB, emailB, "register", "BAD-21")
	if status != http.StatusTooManyRequests {
		t.Fatalf("第 21 次无效邀请码应 429，实际 %d %s", status, body)
	}
	if retryAfter := t8RetryAfter(t, header); retryAfter != 600 {
		t.Errorf("邀请码失败限速 Retry-After 应为 600，实际 %d", retryAfter)
	}
	if count, _, _ := recorder.snapshot(); count != 1 {
		t.Fatalf("无效邀请码不应发信（应只有第 1 个有效码 1 封），实际 %d", count)
	}

	// 有效码不被失败限速误伤：同一 IP 携带有效码仍成功。
	status, body, _ = env.t30Verification(ipB, emailB, "register", t30InviteCode)
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("有效邀请码不应被失败计数限速误伤，实际 %d %s", status, body)
	}
	// 邮箱日配额只被 1 次成功发码消耗；换 IP 再发同邮箱 → email_day 429。
	status, body, _ = env.t30Verification("203.0.113.222", emailB, "register", t30InviteCode)
	if status != http.StatusTooManyRequests {
		t.Fatalf("无效邀请码不应消耗邮箱日配额；第 2 次有效发码应因 email_max_per_day=1 返回 429，实际 %d %s", status, body)
	}
}

// TestInviteCodeEnabledRegistrationE2E 覆盖开启态注册：缺/错码拒绝且不消耗验证码；
// 有效码 + "vr" 成功且 inviter 正确；"v" 不能注册。
func TestInviteCodeEnabledRegistrationE2E(t *testing.T) {
	env, inviter, recorder := t30InviteEnv(t, true, true)

	const email = "enabled-reg@example.com"
	const ip = "203.0.113.230"
	code := t30RequireRegisterCode(t, env, recorder, ip, email, t30InviteCode)

	// 1) 缺码 / 错码 / 大小写变体 → 拒绝，且验证码仍有效（未被消耗/删除）。
	for _, invite := range []string{"", "WRONG", "abc123"} {
		status, body, _ := env.t30Register(ip, "enabled-reg", email, code, invite)
		if status != http.StatusOK || t8JSONSuccess(t, body) {
			t.Fatalf("邀请码 %q 应被拒绝，实际 %d %s", invite, status, body)
		}
		if !common.VerifyCodeWithKey(email, code, common.RegistrationEmailVerificationPurpose) {
			t.Fatalf("邀请码校验失败不应消耗邮箱验证码（invite=%q）", invite)
		}
	}
	// 失败邀请码不应累计邮箱验证码错误次数：连续 5 次错误邀请码后，正确邀请码仍可用。
	for i := 0; i < 5; i++ {
		env.t30Register(ip, "enabled-reg", email, "00000000", "WRONG")
	}
	if !common.VerifyCodeWithKey(email, code, common.RegistrationEmailVerificationPurpose) {
		t.Fatalf("错误邀请码不应触发验证码错误计数/删除")
	}

	// 2) 有效码 + "vr" → 成功且 inviter_id 正确。
	status, body, _ := env.t30Register(ip, "enabled-reg", email, code, t30InviteCode)
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("有效邀请码 + vr 码应注册成功，实际 %d %s", status, body)
	}
	created := t30UserByUsername(t, env, "enabled-reg")
	if created.InviterId != inviter.Id {
		t.Errorf("注册用户 inviter_id 应为 %d，实际 %d", inviter.Id, created.InviterId)
	}
	if content := func() string { _, _, c := recorder.snapshot(); return c }(); content == "" {
		t.Errorf("期望存在邮件内容")
	}

	// 3) "v" 码不能注册（即使邀请码有效）：构造普通发码 + v 码。
	const emailV = "enabled-reg-v@example.com"
	status, body, _ = env.t30Verification("203.0.113.231", emailV, "", "") // 无 purpose → v；无需邀请码
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("普通发码应成功，实际 %d %s", status, body)
	}
	_, _, content := recorder.snapshot()
	codeV := t8ExtractVerificationCode(t, content)
	status, body, _ = env.t30Register(ip, "enabled-reg-v", emailV, codeV, t30InviteCode)
	if status != http.StatusOK || t8JSONSuccess(t, body) {
		t.Fatalf("开启邀请码时 \"v\" 码不能用于注册，实际 %d %s", status, body)
	}
	if !common.VerifyCodeWithKey(emailV, codeV, common.EmailVerificationPurpose) {
		t.Errorf("\"v\" 码在注册失败后应仍保留（供绑定邮箱使用）")
	}
}

// TestInviteCodePurposeSeparationE2E 覆盖 purpose 隔离：v 可绑定邮箱，"vr" 不可绑定。
func TestInviteCodePurposeSeparationE2E(t *testing.T) {
	env, _, recorder := t30InviteEnv(t, true, true)

	// 绑定用户 + session。
	bindUser := &model.User{
		Username: "invite-bind-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "BIND30",
	}
	if err := env.db.Create(bindUser).Error; err != nil {
		t.Fatalf("create bind user: %v", err)
	}
	env.loginAs(bindUser.Id)

	const bindEmail = "invite-bind@example.com"
	status, body, _ := env.t30Verification("203.0.113.240", bindEmail, "", "")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("绑定用途发码应成功，实际 %d %s", status, body)
	}
	_, _, content := recorder.snapshot()
	codeV := t8ExtractVerificationCode(t, content)
	payload, err := json.Marshal(map[string]any{"email": bindEmail, "code": codeV})
	if err != nil {
		t.Fatalf("marshal bind payload: %v", err)
	}
	status, body, _ = env.request(http.MethodPost, "/api/oauth/email/bind", string(payload), "203.0.113.240")
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("开启邀请码时 EmailBind 用 \"v\" 码应成功，实际 %d %s", status, body)
	}
	var bound model.User
	if err := env.db.Where("id = ?", bindUser.Id).First(&bound).Error; err != nil {
		t.Fatalf("reload bind user: %v", err)
	}
	if bound.Email != bindEmail {
		t.Errorf("绑定后邮箱应为 %q，实际 %q", bindEmail, bound.Email)
	}

	// "vr" 码不能绑定邮箱。
	const bindEmail2 = "invite-bind-vr@example.com"
	codeVR := t30RequireRegisterCode(t, env, recorder, "203.0.113.241", bindEmail2, t30InviteCode)
	payload, err = json.Marshal(map[string]any{"email": bindEmail2, "code": codeVR})
	if err != nil {
		t.Fatalf("marshal vr bind payload: %v", err)
	}
	status, body, _ = env.request(http.MethodPost, "/api/oauth/email/bind", string(payload), "203.0.113.241")
	if status == http.StatusOK && t8JSONSuccess(t, body) {
		t.Fatalf("\"vr\" 码不应能用于绑定邮箱，实际 %d %s", status, body)
	}
}

// TestInviteCodeRegistrationLockoutPurposeE2E 覆盖 D2：验证码锁定按实际 purpose 计数/删除、不回退。
func TestInviteCodeRegistrationLockoutPurposeE2E(t *testing.T) {
	t.Run("enabled lock vr not fallback v", func(t *testing.T) {
		env, _, recorder := t30InviteEnv(t, true, true)
		const email = "lock-enabled@example.com"
		const ip = "203.0.113.250"
		codeVR := t30RequireRegisterCode(t, env, recorder, ip, email, t30InviteCode)
		// 普通 "v" 验证码（无 purpose 发码）。
		status, body, _ := env.t30Verification("203.0.113.251", email, "", "")
		if status != http.StatusOK || !t8JSONSuccess(t, body) {
			t.Fatalf("普通发码应成功，实际 %d %s", status, body)
		}
		_, _, content := recorder.snapshot()
		codeV := t8ExtractVerificationCode(t, content)

		// 用 "v" 码注册（开启态只尝试 "vr"）连续 5 次 → 第 5 次 429，删除 vr，v 保留。
		var lastHeader http.Header
		for i := 1; i <= 5; i++ {
			status, body, lastHeader = env.t30Register(ip, "lock-enabled", email, codeV, t30InviteCode)
			if i < 5 {
				if status != http.StatusOK || t8JSONSuccess(t, body) {
					t.Fatalf("第 %d 次错误码注册应失败（200 success:false），实际 %d %s", i, status, body)
				}
			}
		}
		if status != http.StatusTooManyRequests {
			t.Fatalf("第 5 次错误码注册应 429 锁定，实际 %d %s", status, body)
		}
		if retryAfter := t8RetryAfter(t, lastHeader); retryAfter <= 0 || retryAfter > 600 {
			t.Errorf("锁定 Retry-After 应在 (0,600]，实际 %d", retryAfter)
		}
		if common.VerifyCodeWithKey(email, codeVR, common.RegistrationEmailVerificationPurpose) {
			t.Errorf("锁定时应删除实际尝试的 \"vr\" 验证码")
		}
		if !common.VerifyCodeWithKey(email, codeV, common.EmailVerificationPurpose) {
			t.Errorf("锁定不应删除 \"v\" 验证码（未回退使用）")
		}
	})

	t.Run("disabled with vr exists locks vr", func(t *testing.T) {
		env, _, recorder := t30InviteEnv(t, false, true)
		const email = "lock-disabled@example.com"
		const ip = "203.0.113.252"
		// 生成 "vr"（purpose=register，关闭态不校验邀请码）。
		status, body, _ := env.t30Verification(ip, email, "register", "")
		if status != http.StatusOK || !t8JSONSuccess(t, body) {
			t.Fatalf("purpose=register 发码应成功，实际 %d %s", status, body)
		}
		_, _, content := recorder.snapshot()
		codeVR := t8ExtractVerificationCode(t, content)
		// 生成 "v"。
		status, body, _ = env.t30Verification("203.0.113.253", email, "", "")
		if status != http.StatusOK || !t8JSONSuccess(t, body) {
			t.Fatalf("普通发码应成功，实际 %d %s", status, body)
		}
		_, _, content = recorder.snapshot()
		codeV := t8ExtractVerificationCode(t, content)

		// 存在 vr 时只按 vr 尝试：用 v 码连续失败 → 第 5 次 429，vr 删除、v 保留。
		var lastStatus int
		for i := 1; i <= 5; i++ {
			lastStatus, body, _ = env.t30Register(ip, "lock-disabled", email, codeV, "")
			if i < 5 && (lastStatus != http.StatusOK || t8JSONSuccess(t, body)) {
				t.Fatalf("第 %d 次错误码注册应失败，实际 %d %s", i, lastStatus, body)
			}
		}
		if lastStatus != http.StatusTooManyRequests {
			t.Fatalf("存在 vr 时第 5 次错误应 429（按 vr 计数），实际 %d %s", lastStatus, body)
		}
		if common.VerifyCodeWithKey(email, codeVR, common.RegistrationEmailVerificationPurpose) {
			t.Errorf("锁定时应删除 \"vr\" 验证码")
		}
		if !common.VerifyCodeWithKey(email, codeV, common.EmailVerificationPurpose) {
			t.Errorf("锁定不应删除 \"v\" 验证码")
		}
	})

	t.Run("disabled only v falls back and success clears failures", func(t *testing.T) {
		env, _, recorder := t30InviteEnv(t, false, true)
		const email = "fallback-v@example.com"
		const ip = "203.0.113.254"
		status, body, _ := env.t30Verification(ip, email, "", "")
		if status != http.StatusOK || !t8JSONSuccess(t, body) {
			t.Fatalf("普通发码应成功，实际 %d %s", status, body)
		}
		_, _, content := recorder.snapshot()
		codeV := t8ExtractVerificationCode(t, content)

		// 注册成功前先制造 2 次验证码错误（计数 2）。
		for i := 1; i <= 2; i++ {
			status, body, _ := env.t30Register(ip, "fallback-v-pre", email, "wrong-pre-"+string(rune('0'+i)), "")
			if status != http.StatusOK || t8JSONSuccess(t, body) {
				t.Fatalf("注册成功前第 %d 次错误码应失败，实际 %d %s", i, status, body)
			}
		}

		// 无 vr → 回退 v 并注册成功（成功后应清理此前 2 次错误计数）。
		status, body, _ = env.t30Register(ip, "fallback-v", email, codeV, "")
		if status != http.StatusOK || !t8JSONSuccess(t, body) {
			t.Fatalf("仅存在 v 时应回退 v 并注册成功，实际 %d %s", status, body)
		}

		// 成功后错误计数已清理：对同一邮箱继续错误 5 次，应第 5 次才 429
		// （若未清理，此前 2 次 + 3 次会在第 3 次提前锁定）。
		for i := 1; i <= 5; i++ {
			status, body, _ = env.t30Register(ip, "fallback-v-2", email, "wrong-after-"+string(rune('0'+i)), "")
			if i < 5 {
				if status != http.StatusOK || t8JSONSuccess(t, body) {
					t.Fatalf("成功后第 %d 次错误不应锁定（计数应已清理），实际 %d %s", i, status, body)
				}
			}
		}
		if status != http.StatusTooManyRequests {
			t.Errorf("成功注册后重新累计 5 次错误应第 5 次 429，实际 %d %s", status, body)
		}
	})
}
