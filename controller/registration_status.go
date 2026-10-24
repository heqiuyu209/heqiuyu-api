package controller

import (
	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// registrationStatusFields 返回注册防护相关的前端状态字段，
// 由 GetStatus 合并进 /api/status 响应。字段均为「生效值」：
// 注册防护总开关关闭时，蜜罐与表单令牌均视为关闭。
func registrationStatusFields() map[string]any {
	registrationSetting := operation_setting.GetRegistrationSetting()
	return map[string]any{
		"register_enabled": common.RegisterEnabled,
		"password_register_enabled": common.RegisterEnabled &&
			common.PasswordRegisterEnabled,
		"oauth_register_enabled":          common.RegisterEnabled,
		"registration_form_token_enabled": service.RegistrationFormTokenEnabled(),
		"registration_honeypot_enabled": registrationSetting.Enabled &&
			registrationSetting.HoneypotEnabled,
		// 邀请码策略独立于注册防护总开关：只读 invite_code_required。
		"invite_code_required": registrationSetting.InviteCodeRequired,
	}
}
