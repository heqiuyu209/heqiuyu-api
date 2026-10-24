package operation_setting

import "github.com/heqiuyu/heqiuyu-api/setting/config"

// RegistrationSetting 注册防护配置
//
// 配置键前缀为 registration_setting.，json tag 为配置键后缀。
// 该配置同时被服务端注册防护（限流/蜜罐/表单令牌/异步邮件）与前端注册页、
// 系统设置「注册防护」页使用，字段名与配置键需保持稳定。
type RegistrationSetting struct {
	// 总开关
	Enabled bool `json:"enabled"` // 注册防护总开关

	// Turnstile
	TurnstileStrict bool `json:"turnstile_strict"` // 注册/发码每次强制校验 Turnstile，不复用 session

	// 单 IP 注册限流
	IpAttemptWindowSeconds int    `json:"ip_attempt_window_seconds"`  // 尝试窗口（秒）
	IpMaxAttemptsPerWindow int    `json:"ip_max_attempts_per_window"` // 单 IP 窗口内最多注册请求（含失败）
	IpMaxSuccessPerHour    int    `json:"ip_max_success_per_hour"`    // 单 IP 每小时成功注册上限
	IpMaxSuccessPerDay     int    `json:"ip_max_success_per_day"`     // 单 IP 每天成功注册上限
	IpAllowlist            string `json:"ip_allowlist"`               // 逗号分隔 IP/CIDR 永久放行

	// 全站注册限流
	GlobalMaxSuccessPerHour int `json:"global_max_success_per_hour"` // 全站每小时成功上限（0=关闭）

	// 邮箱验证码发送限流
	EmailCooldownSeconds  int `json:"email_cooldown_seconds"`    // 同邮箱发码最小间隔（秒）
	EmailMaxPerDay        int `json:"email_max_per_day"`         // 同邮箱每天发码上限
	EmailIpMaxPerHour     int `json:"email_ip_max_per_hour"`     // 单 IP 每小时发码上限
	EmailGlobalMaxPerHour int `json:"email_global_max_per_hour"` // 全站每小时发码上限（保护 SMTP）

	// 验证码尝试限制
	VerificationMaxAttempts          int `json:"verification_max_attempts"`           // 验证码错误次数上限
	VerificationAttemptWindowSeconds int `json:"verification_attempt_window_seconds"` // 验证码错误计数窗口（秒）

	// 蜜罐与表单令牌
	HoneypotEnabled     bool `json:"honeypot_enabled"`       // 蜜罐字段开关
	FormTokenEnabled    bool `json:"form_token_enabled"`     // HMAC 表单令牌开关（前端就绪后再开）
	FormTokenMinSeconds int  `json:"form_token_min_seconds"` // 表单最短填写时间（秒）
	FormTokenMaxMinutes int  `json:"form_token_max_minutes"` // 令牌最长有效期（分钟）

	// 异步邮件
	AsyncEmailEnabled         bool `json:"async_email_enabled"`          // 异步发信开关
	AsyncEmailQueueSize       int  `json:"async_email_queue_size"`       // 队列长度（重启生效）
	AsyncEmailWorkers         int  `json:"async_email_workers"`          // worker 数（重启生效）
	AsyncEmailMaxPerMinute    int  `json:"async_email_max_per_minute"`   // 全站发信速率上限
	AsyncEmailBreakerFailures int  `json:"async_email_breaker_failures"` // 连续失败熔断阈值（熔断 60s）

	// 仅限邀请码注册（独立于 Enabled：关总开关不关闭邀请码校验）
	InviteCodeRequired bool `json:"invite_code_required"` // 注册/OAuth 首登必须携带有效邀请码
}

// 默认配置（与《注册防护实施计划》第 2.2 节默认值一致）
var registrationSetting = RegistrationSetting{
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

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("registration_setting", &registrationSetting)
}

// GetRegistrationSetting 获取注册防护配置
func GetRegistrationSetting() *RegistrationSetting {
	return &registrationSetting
}
