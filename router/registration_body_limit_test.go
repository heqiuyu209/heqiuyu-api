package router

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// t27ProductRegistrationDefaults 在包初始化时捕获 registration_setting 的产品默认值
// （早于任何测试开始修改全局配置），用于默认配置一致性测试的隔离基线。
var t27ProductRegistrationDefaults = *operation_setting.GetRegistrationSetting()

// t27ResetRegistrationSettingToProductDefaults 显式快照当前全局配置并重置为产品默认值，
// 用例结束（Cleanup）恢复快照。使 `-run` 单独执行或全量执行都不依赖测试文件执行顺序。
func t27ResetRegistrationSettingToProductDefaults(t *testing.T) {
	t.Helper()
	setting := operation_setting.GetRegistrationSetting()
	saved := *setting
	*setting = t27ProductRegistrationDefaults
	t.Cleanup(func() { *setting = saved })
}

// TestRegistrationDefaultConfigCopyMatchesProduct 防止 e2e 测试里复制的
// 24 项 registration_setting 默认值随产品默认值变更而漂移（R2-TESTS-2 / INV）。
//
// 隔离方式：显式重置为包初始化时捕获的产品默认值，再与测试副本比较；
// 不再依赖“本文件先于其它测试文件执行”这一隐含顺序。
func TestRegistrationDefaultConfigCopyMatchesProduct(t *testing.T) {
	t27ResetRegistrationSettingToProductDefaults(t)

	product := *operation_setting.GetRegistrationSetting()
	copied := t8DefaultRegistrationSettingForTest()
	if product != copied {
		t.Fatalf("测试默认配置副本与产品默认值漂移：\nproduct=%+v\ncopied =%+v", product, copied)
	}
}

// TestRegistrationBodyLimitE2E 覆盖 R2 §2：注册请求体 >16KB 返回 413 且不落库，
// 正常小 body 仍可注册；Content-Length 未知（分块传输）时由 MaxBytesReader 兜底 413。
func TestRegistrationBodyLimitE2E(t *testing.T) {
	env := t8NewRegistrationEnv(t)
	env.setting.IpMaxAttemptsPerWindow = 0

	// 1) 正常小 body 注册成功。
	status, body, _ := env.register("203.0.113.100", t8RegisterPayload("t8body-ok", nil))
	if status != http.StatusOK || !t8JSONSuccess(t, body) {
		t.Fatalf("正常小 body 注册应成功，实际 %d %s", status, body)
	}
	if count := env.userCount(); count != 1 {
		t.Fatalf("正常注册后应有 1 条用户记录，实际 %d", count)
	}

	// 2) Content-Length 已知且 >16KB → RegistrationBodyLimit 直接 413，不读 body。
	bigPadding := strings.Repeat("a", 17*1024)
	bigPayload := t8RegisterPayload("t8body-big", map[string]any{"padding": bigPadding})
	status, body, _ = env.register("203.0.113.101", bigPayload)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("注册 body >16KB 应返回 413，实际 %d %s", status, body)
	}
	if t8JSONSuccess(t, body) {
		t.Errorf("413 响应不应为 success:true: %s", body)
	}
	if strings.Contains(body, i18n.MsgUserRequestBodyTooLarge) {
		t.Errorf("413 文案应使用 i18n 翻译而不是原始 key: %s", body)
	}
	if count := env.userCount(); count != 1 {
		t.Errorf("超大 body 注册不应落库，实际用户数 %d", count)
	}

	// 3) Content-Length 未知（chunked）→ MaxBytesReader + PayloadGuard 兜底 413。
	chunkedPayload, err := json.Marshal(t8RegisterPayload("t8body-chunked", map[string]any{"padding": bigPadding}))
	if err != nil {
		t.Fatalf("marshal chunked payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, env.server.URL+"/api/user/register", io.NopCloser(bytes.NewReader(chunkedPayload)))
	if err != nil {
		t.Fatalf("new chunked request: %v", err)
	}
	req.ContentLength = -1 // 触发 chunked 传输，服务端 Content-Length 未知
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.102")
	res, err := env.client.Do(req)
	if err != nil {
		t.Fatalf("chunked register request: %v", err)
	}
	defer res.Body.Close()
	chunkedBody, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("分块传输的超大注册 body 也应返回 413，实际 %d %s", res.StatusCode, string(chunkedBody))
	}
	if count := env.userCount(); count != 1 {
		t.Errorf("分块超大 body 注册不应落库，实际用户数 %d", count)
	}
}
