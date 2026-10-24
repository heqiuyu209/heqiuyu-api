package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// t8TokenSetup 为每个用例准备确定性的表单令牌配置、干净的内存 nonce 表与可控时钟。
//
// registrationTokenNow 与 formTokenStore 都是 service 包内变量（无导出重置钩子），
// 因此本文件使用包内测试直接替换/恢复：测试之间不共享状态。
func t8TokenSetup(t *testing.T) (*operation_setting.RegistrationSetting, *time.Time) {
	t.Helper()

	prevRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false // 本机无 Redis：使用内存 nonce 表
	t.Cleanup(func() { common.RedisEnabled = prevRedisEnabled })

	setting := operation_setting.GetRegistrationSetting()
	savedSetting := *setting
	t.Cleanup(func() { *setting = savedSetting })

	*setting = operation_setting.RegistrationSetting{
		Enabled:             true,
		FormTokenEnabled:    true,
		FormTokenMinSeconds: 3,
		FormTokenMaxMinutes: 30,
	}

	formTokenStoreMu.Lock()
	savedStore := formTokenStore
	formTokenStore = make(map[string]*formTokenRecord)
	formTokenStoreMu.Unlock()
	t.Cleanup(func() {
		formTokenStoreMu.Lock()
		formTokenStore = savedStore
		formTokenStoreMu.Unlock()
	})

	prevNow := registrationTokenNow
	now := time.Unix(1_700_000_000, 0)
	registrationTokenNow = func() time.Time { return now }
	t.Cleanup(func() { registrationTokenNow = prevNow })

	return setting, &now
}

// TestRegistrationFormTokenDisabled 覆盖关闭时签发/消费均返回 ErrFormTokenDisabled。
func TestRegistrationFormTokenDisabled(t *testing.T) {
	setting, _ := t8TokenSetup(t)

	setting.FormTokenEnabled = false
	if _, _, _, err := IssueRegistrationFormToken(); !errors.Is(err, ErrFormTokenDisabled) {
		t.Errorf("form_token_enabled=false 时签发应返回 ErrFormTokenDisabled，实际: %v", err)
	}
	if err := ConsumeRegistrationFormToken("v1:0:nonce:sig"); !errors.Is(err, ErrFormTokenDisabled) {
		t.Errorf("form_token_enabled=false 时消费应返回 ErrFormTokenDisabled，实际: %v", err)
	}
	if RegistrationFormTokenEnabled() {
		t.Errorf("form_token_enabled=false 时 RegistrationFormTokenEnabled() 应为 false")
	}

	// 注册防护总开关关闭时同样视为关闭。
	setting.FormTokenEnabled = true
	setting.Enabled = false
	if _, _, _, err := IssueRegistrationFormToken(); !errors.Is(err, ErrFormTokenDisabled) {
		t.Errorf("registration_setting.enabled=false 时签发应返回 ErrFormTokenDisabled，实际: %v", err)
	}
	if RegistrationFormTokenEnabled() {
		t.Errorf("registration_setting.enabled=false 时 RegistrationFormTokenEnabled() 应为 false")
	}
}

// TestIssueRegistrationFormToken 覆盖签发格式以及 min_seconds/expires_in 与配置一致。
func TestIssueRegistrationFormToken(t *testing.T) {
	setting, now := t8TokenSetup(t)
	*now = time.Unix(1_700_000_123, 0)

	token, minSeconds, expiresIn, err := IssueRegistrationFormToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}
	if !RegistrationFormTokenEnabled() {
		t.Errorf("form_token_enabled=true 时 RegistrationFormTokenEnabled() 应为 true")
	}
	if minSeconds != setting.FormTokenMinSeconds {
		t.Errorf("min_seconds 应等于 form_token_min_seconds=%d，实际 %d", setting.FormTokenMinSeconds, minSeconds)
	}
	if want := setting.FormTokenMaxMinutes * 60; expiresIn != want {
		t.Errorf("expires_in 应为 %d，实际 %d", want, expiresIn)
	}

	parts := strings.Split(token, ":")
	if len(parts) != 4 || parts[0] != "v1" {
		t.Fatalf("令牌格式应为 v1:<issuedAt>:<nonce>:<hmac>，实际 %q", token)
	}
	if parts[1] != strconv.FormatInt(now.Unix(), 10) {
		t.Errorf("令牌 issuedAt 应来自注入时钟 %d，实际 %q", now.Unix(), parts[1])
	}
	if parts[2] == "" {
		t.Errorf("令牌 nonce 不应为空")
	}
	if want := common.GenerateHMAC(strings.Join(parts[:3], ":")); parts[3] != want {
		t.Errorf("令牌 HMAC 签名不正确")
	}
}

// TestConsumeRegistrationFormTokenOnce 覆盖一次性消费：第二次返回 ErrFormTokenReused。
func TestConsumeRegistrationFormTokenOnce(t *testing.T) {
	_, now := t8TokenSetup(t)
	token, _, _, err := IssueRegistrationFormToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}

	*now = now.Add(5 * time.Second)
	if err := ConsumeRegistrationFormToken(token); err != nil {
		t.Fatalf("首次消费合法令牌应成功，实际: %v", err)
	}
	if err := ConsumeRegistrationFormToken(token); !errors.Is(err, ErrFormTokenReused) {
		t.Fatalf("重复消费应返回 ErrFormTokenReused，实际: %v", err)
	}
}

// TestConsumeRegistrationFormTokenSignature 覆盖签名校验与篡改。
func TestConsumeRegistrationFormTokenSignature(t *testing.T) {
	_, now := t8TokenSetup(t)
	token, _, _, err := IssueRegistrationFormToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}
	*now = now.Add(5 * time.Second)

	parts := strings.Split(token, ":")
	// 篡改签名。
	tampered := strings.Join([]string{parts[0], parts[1], parts[2], "deadbeef"}, ":")
	if err := ConsumeRegistrationFormToken(tampered); !errors.Is(err, ErrFormTokenInvalid) {
		t.Errorf("签名不正确应返回 ErrFormTokenInvalid，实际: %v", err)
	}
	// 篡改 nonce（HMAC 不再匹配）。
	tampered = strings.Join([]string{parts[0], parts[1], "another-nonce", parts[3]}, ":")
	if err := ConsumeRegistrationFormToken(tampered); !errors.Is(err, ErrFormTokenInvalid) {
		t.Errorf("篡改 nonce 应返回 ErrFormTokenInvalid，实际: %v", err)
	}
	// 原令牌未被消耗，仍可用。
	if err := ConsumeRegistrationFormToken(token); err != nil {
		t.Errorf("篡改尝试不应消耗原令牌，实际: %v", err)
	}
}

// TestConsumeRegistrationFormTokenUnknownNonce 覆盖签名合法但 nonce 未知（未签发）的令牌。
func TestConsumeRegistrationFormTokenUnknownNonce(t *testing.T) {
	_, now := t8TokenSetup(t)

	payload := strings.Join([]string{"v1", strconv.FormatInt(now.Unix(), 10), "unknown-nonce"}, ":")
	forged := payload + ":" + common.GenerateHMAC(payload)
	if err := ConsumeRegistrationFormToken(forged); !errors.Is(err, ErrFormTokenInvalid) {
		t.Errorf("未签发的 nonce 应返回 ErrFormTokenInvalid，实际: %v", err)
	}
}

// TestConsumeRegistrationFormTokenMinSeconds 覆盖表单最短填写时间。
func TestConsumeRegistrationFormTokenMinSeconds(t *testing.T) {
	setting, now := t8TokenSetup(t)
	setting.FormTokenMinSeconds = 3
	token, _, _, err := IssueRegistrationFormToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}

	*now = now.Add(2 * time.Second)
	if err := ConsumeRegistrationFormToken(token); !errors.Is(err, ErrFormTokenInvalid) {
		t.Errorf("填写时间小于 form_token_min_seconds 应返回 ErrFormTokenInvalid，实际: %v", err)
	}
	*now = now.Add(2 * time.Second) // 距签发 4 秒
	if err := ConsumeRegistrationFormToken(token); err != nil {
		t.Errorf("达到最短填写时间后应可消费，实际: %v", err)
	}
}

// TestConsumeRegistrationFormTokenExpiry 覆盖令牌最长有效期与未来时间戳。
func TestConsumeRegistrationFormTokenExpiry(t *testing.T) {
	setting, now := t8TokenSetup(t)
	setting.FormTokenMaxMinutes = 1 // 60 秒有效期
	token, _, expiresIn, err := IssueRegistrationFormToken()
	if err != nil {
		t.Fatalf("签发令牌失败: %v", err)
	}
	if expiresIn != 60 {
		t.Fatalf("expires_in 应为 60，实际 %d", expiresIn)
	}

	// 超过有效期（多留 1 秒余量）。
	*now = now.Add(62 * time.Second)
	if err := ConsumeRegistrationFormToken(token); !errors.Is(err, ErrFormTokenExpired) {
		t.Errorf("超过有效期应返回 ErrFormTokenExpired，实际: %v", err)
	}

	// 未来时间戳（时钟回拨/伪造）→ 无效。
	futurePayload := strings.Join([]string{"v1", strconv.FormatInt(now.Add(time.Hour).Unix(), 10), "future-nonce"}, ":")
	future := futurePayload + ":" + common.GenerateHMAC(futurePayload)
	if err := ConsumeRegistrationFormToken(future); !errors.Is(err, ErrFormTokenInvalid) {
		t.Errorf("未来时间戳应返回 ErrFormTokenInvalid，实际: %v", err)
	}
}

// TestConsumeRegistrationFormTokenMalformed 覆盖格式非法与过期/重放错误可区分。
func TestConsumeRegistrationFormTokenMalformed(t *testing.T) {
	_, _ = t8TokenSetup(t)

	cases := []string{
		"",
		"v1",
		"v1:123",
		"v1:123:nonce",
		"v2:123:nonce:sig",
		"v1:not-a-number:nonce:sig",
		"v1:123::sig",
		"v1:123:nonce:sig:extra",
	}
	for _, token := range cases {
		if err := ConsumeRegistrationFormToken(token); !errors.Is(err, ErrFormTokenInvalid) {
			t.Errorf("非法令牌 %q 应返回 ErrFormTokenInvalid，实际: %v", token, err)
		}
	}
}

// TestRegistrationFormTokenStoreEvictsOldest 覆盖 R2-TESTS-2 的 nonce 逐出专项：
// 内存 nonce 表达到 formTokenStoreMaxEntries 后，继续签发必须逐出最旧记录、
// 表大小保持硬上限，且新 nonce 可正常消费（不因表满而拒绝签发）。
func TestRegistrationFormTokenStoreEvictsOldest(t *testing.T) {
	setting, now := t8TokenSetup(t)
	setting.FormTokenEnabled = true

	// 直接填充内存表到上限：expiresAt 递增，便于断言"最旧"记录被逐出。
	formTokenStoreMu.Lock()
	for i := 0; i < formTokenStoreMaxEntries; i++ {
		nonce := fmt.Sprintf("nonce-%05d", i)
		formTokenStore[nonce] = &formTokenRecord{
			expiresAt: now.Add(time.Duration(i+1) * time.Minute),
		}
	}
	formTokenStoreMu.Unlock()

	token, _, _, err := IssueRegistrationFormToken()
	if err != nil {
		t.Fatalf("nonce 表已满时仍应允许签发（逐出最旧记录），实际: %v", err)
	}
	parts := strings.Split(token, ":")
	if len(parts) != 4 || parts[2] == "" {
		t.Fatalf("签发的令牌格式非法: %q", token)
	}
	newNonce := parts[2]

	formTokenStoreMu.Lock()
	defer formTokenStoreMu.Unlock()
	if len(formTokenStore) != formTokenStoreMaxEntries {
		t.Errorf("逐出后表大小应保持上限 %d，实际 %d", formTokenStoreMaxEntries, len(formTokenStore))
	}
	if _, ok := formTokenStore["nonce-00000"]; ok {
		t.Errorf("最旧记录 nonce-00000 应被逐出")
	}
	if _, ok := formTokenStore[newNonce]; !ok {
		t.Errorf("本次签发的新 nonce 应在表中")
	}
	if _, ok := formTokenStore["nonce-09999"]; !ok {
		t.Errorf("最新记录 nonce-09999 不应被逐出")
	}
}
