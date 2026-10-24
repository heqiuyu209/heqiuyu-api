package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/constant"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/logger"
	"github.com/heqiuyu/heqiuyu-api/middleware"
	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/oauth"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting"
	"github.com/heqiuyu/heqiuyu-api/setting/console_setting"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
	"github.com/heqiuyu/heqiuyu-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func TestStatus(c *gin.Context) {
	err := model.PingDB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "数据库连接失败",
		})
		return
	}
	// 获取HTTP统计信息
	httpStats := middleware.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "Server is running",
		"http_stats": httpStats,
	})
	return
}

func GetStatus(c *gin.Context) {

	cs := console_setting.GetConsoleSetting()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	// 注册防护相关字段（register_enabled / password_register_enabled /
	// oauth_register_enabled / registration_form_token_enabled /
	// registration_honeypot_enabled）由 registration_status.go 统一提供。
	// 在 RLock 之后读取：helper 会读 common.RegisterEnabled 与配置，必须与
	// updateOptionMap 的写入串行化（helper 内部不会再次加锁）。
	registrationFields := registrationStatusFields()

	passkeySetting := system_setting.GetPasskeySettings()
	legalSetting := system_setting.GetLegalSettings()

	data := gin.H{
		"version":                     common.Version,
		"start_time":                  common.StartTime,
		"email_verification":          common.EmailVerificationEnabled,
		"github_oauth":                common.GitHubOAuthEnabled,
		"github_client_id":            common.GitHubClientId,
		"discord_oauth":               system_setting.GetDiscordSettings().Enabled,
		"discord_client_id":           system_setting.GetDiscordSettings().ClientId,
		"linuxdo_oauth":               common.LinuxDOOAuthEnabled,
		"linuxdo_client_id":           common.LinuxDOClientId,
		"linuxdo_minimum_trust_level": common.LinuxDOMinimumTrustLevel,
		"telegram_oauth":              common.TelegramOAuthEnabled,
		"telegram_bot_name":           common.TelegramBotName,
		"system_name":                 common.SystemName,
		"logo":                        common.Logo,
		"footer_html":                 common.Footer,
		"wechat_qrcode":               common.WeChatAccountQRCodeImageURL,
		"wechat_login":                common.WeChatAuthEnabled,
		"server_address":              system_setting.ServerAddress,
		"turnstile_check":             common.TurnstileCheckEnabled,
		"turnstile_site_key":          common.TurnstileSiteKey,
		"top_up_link":                 common.TopUpLink,
		"docs_link":                   operation_setting.GetGeneralSetting().DocsLink,
		"quota_per_unit":              common.QuotaPerUnit,
		// 兼容旧前端：保留 display_in_currency，同时提供新的 quota_display_type
		"display_in_currency":           operation_setting.IsCurrencyDisplay(),
		"quota_display_type":            operation_setting.GetQuotaDisplayType(),
		"custom_currency_symbol":        operation_setting.GetGeneralSetting().CustomCurrencySymbol,
		"custom_currency_exchange_rate": operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate,
		"enable_batch_update":           common.BatchUpdateEnabled,
		"enable_drawing":                common.DrawingEnabled,
		"enable_task":                   common.TaskEnabled,
		"enable_data_export":            common.DataExportEnabled,
		"data_export_default_time":      common.DataExportDefaultTime,
		"default_collapse_sidebar":      common.DefaultCollapseSidebar,
		"mj_notify_enabled":             setting.MjNotifyEnabled,
		"chats":                         setting.Chats,
		"demo_site_enabled":             operation_setting.DemoSiteEnabled,
		"self_use_mode_enabled":         operation_setting.SelfUseModeEnabled,
		"default_use_auto_group":        setting.DefaultUseAutoGroup,

		"usd_exchange_rate": operation_setting.USDExchangeRate,

		// 面板启用开关
		"api_info_enabled":      cs.ApiInfoEnabled,
		"uptime_kuma_enabled":   cs.UptimeKumaEnabled,
		"announcements_enabled": cs.AnnouncementsEnabled,
		"faq_enabled":           cs.FAQEnabled,

		// 模块管理配置
		"HeaderNavModules":    common.OptionMap["HeaderNavModules"],
		"SidebarModulesAdmin": common.OptionMap["SidebarModulesAdmin"],

		"oidc_enabled":                system_setting.GetOIDCSettings().Enabled,
		"oidc_client_id":              system_setting.GetOIDCSettings().ClientId,
		"oidc_authorization_endpoint": system_setting.GetOIDCSettings().AuthorizationEndpoint,
		"passkey_login":               passkeySetting.Enabled,
		"passkey_display_name":        passkeySetting.RPDisplayName,
		"passkey_rp_id":               passkeySetting.RPID,
		"passkey_origins":             passkeySetting.Origins,
		"passkey_allow_insecure":      passkeySetting.AllowInsecureOrigin,
		"passkey_user_verification":   passkeySetting.UserVerification,
		"passkey_attachment":          passkeySetting.AttachmentPreference,
		"setup":                       constant.Setup,
		"user_agreement_enabled":      legalSetting.UserAgreement != "",
		"privacy_policy_enabled":      legalSetting.PrivacyPolicy != "",
		"checkin_enabled":             operation_setting.GetCheckinSetting().Enabled,
	}

	// 合并注册防护状态字段（step6 的 registrationStatusFields）。
	for key, value := range registrationFields {
		data[key] = value
	}

	// 根据启用状态注入可选内容
	if cs.ApiInfoEnabled {
		data["api_info"] = console_setting.GetApiInfo()
	}
	if cs.AnnouncementsEnabled {
		data["announcements"] = console_setting.GetAnnouncements()
	}
	if cs.FAQEnabled {
		data["faq"] = console_setting.GetFAQ()
	}

	// Add enabled custom OAuth providers
	customProviders := oauth.GetEnabledCustomProviders()
	if len(customProviders) > 0 {
		type CustomOAuthInfo struct {
			Id                    int    `json:"id"`
			Name                  string `json:"name"`
			Slug                  string `json:"slug"`
			Icon                  string `json:"icon"`
			ClientId              string `json:"client_id"`
			AuthorizationEndpoint string `json:"authorization_endpoint"`
			Scopes                string `json:"scopes"`
		}
		providersInfo := make([]CustomOAuthInfo, 0, len(customProviders))
		for _, p := range customProviders {
			config := p.GetConfig()
			providersInfo = append(providersInfo, CustomOAuthInfo{
				Id:                    config.Id,
				Name:                  config.Name,
				Slug:                  config.Slug,
				Icon:                  config.Icon,
				ClientId:              config.ClientId,
				AuthorizationEndpoint: config.AuthorizationEndpoint,
				Scopes:                config.Scopes,
			})
		}
		data["custom_oauth_providers"] = providersInfo
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
	return
}

func GetNotice(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Notice"],
	})
	return
}

func GetAbout(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["About"],
	})
	return
}

func GetUserAgreement(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetLegalSettings().UserAgreement,
	})
	return
}

func GetPrivacyPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetLegalSettings().PrivacyPolicy,
	})
	return
}

func GetMidjourney(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["Midjourney"],
	})
	return
}

func GetHomePageContent(c *gin.Context) {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    common.OptionMap["HomePageContent"],
	})
	return
}

// maskEmail 对日志中的邮箱做脱敏（a***@domain），避免明文邮箱进入日志。
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "***"
	}
	localPart := email[:at]
	domain := email[at+1:]
	if len(localPart) == 0 {
		return "***@" + domain
	}
	return localPart[:1] + "***@" + domain
}

func SendEmailVerification(c *gin.Context) {
	// 邮箱统一规范化：验证码/限流/占用的键都必须与注册时一致。
	email := strings.ToLower(strings.TrimSpace(c.Query("email")))
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的邮箱地址",
		})
		return
	}
	localPart := parts[0]
	domainPart := parts[1]
	if common.EmailDomainRestrictionEnabled {
		allowed := false
		for _, domain := range common.EmailDomainWhitelist {
			if domainPart == domain {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "The administrator has enabled the email domain name whitelist, and your email address is not allowed due to special symbols or it's not in the whitelist.",
			})
			return
		}
	}
	if common.EmailAliasRestrictionEnabled {
		containsSpecialSymbols := strings.Contains(localPart, "+") || strings.Contains(localPart, ".")
		if containsSpecialSymbols {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员已启用邮箱地址别名限制，您的邮箱地址由于包含特殊符号而被拒绝。",
			})
			return
		}
	}

	// 发码用途：purpose=register 表示注册用途（开启邀请码时使用注册专用验证码 "vr"），
	// 其他用途（如绑定邮箱）仍使用普通 "v"。
	emailPurpose := common.EmailVerificationPurpose
	if strings.TrimSpace(c.Query("purpose")) == registerEmailPurposeParam {
		emailPurpose = common.RegistrationEmailVerificationPurpose
		// 开启邀请码时 controller 侧再校验一次（中间件 RegistrationInviteGuard 已先拦截），
		// 保证任何调用路径都不会给无效邀请码发送注册验证码。
		if inviteCodeRequired() {
			if _, err := validateInviteCode(c.Query("invite_code")); err != nil {
				respondInviteCodeError(c, err)
				return
			}
		}
	}

	if model.IsEmailAlreadyTaken(email) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "邮箱地址已被占用",
		})
		return
	}
	// 验证码位数 6 → 8（提高暴力猜测成本；错误次数另有 5 次锁定）。
	code := common.GenerateVerificationCode(8)
	common.RegisterVerificationCodeWithKey(email, code, emailPurpose)
	subject := fmt.Sprintf("%s邮箱验证邮件", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s邮箱验证。</p>"+
		"<p>您的验证码为: <strong>%s</strong></p>"+
		"<p>验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, code, common.VerificationValidMinutes)
	// 先存码再入队：入队失败必须删除验证码，避免"队列满但验证码仍有效"的悬挂状态。
	if err := service.EnqueueEmail("verification", email, subject, content); err != nil {
		common.DeleteKey(email, emailPurpose)
		common.SysLog(fmt.Sprintf("[mail-queue] enqueue verification email failed for %s: %v", maskEmail(email), err))
		if errors.Is(err, service.ErrMailRateLimited) {
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgUserEmailSendRateLimited),
			})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgUserEmailSendUnavailable),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func SendPasswordResetEmail(c *gin.Context) {
	email := strings.ToLower(strings.TrimSpace(c.Query("email")))
	if err := common.Validate.Var(email, "required,email"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	if model.IsEmailAlreadyTaken(email) {
		code := common.GenerateVerificationCode(0)
		common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
		link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", system_setting.ServerAddress, email, code)
		subject := fmt.Sprintf("%s密码重置", common.SystemName)
		content := fmt.Sprintf("<p>您好，你正在进行%s密码重置。</p>"+
			"<p>点击 <a href='%s'>此处</a> 进行密码重置。</p>"+
			"<p>如果链接无法点击，请尝试点击下面的链接或将其复制到浏览器中打开：<br> %s </p>"+
			"<p>重置链接 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, link, link, common.VerificationValidMinutes)
		if err := service.EnqueueEmail("password-reset", email, subject, content); err != nil {
			common.DeleteKey(email, common.PasswordResetPurpose)
			// 该接口对"邮箱是否存在"必须保持不可区分：入队失败只记录日志，
			// 仍返回成功（与历史行为一致），否则会泄露邮箱是否已注册。
			common.SysLog(fmt.Sprintf("[mail-queue] enqueue password reset email failed for %s: %v", maskEmail(email), err))
			logger.LogError(c.Request.Context(), fmt.Sprintf("failed to enqueue password reset email to %s: %s", maskEmail(email), err.Error()))
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type PasswordResetRequest struct {
	Email       string `json:"email"`
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func ResetPassword(c *gin.Context) {
	var req PasswordResetRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)
	if err != nil || req.Email == "" || req.Token == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !common.VerifyCodeWithKey(req.Email, req.Token, common.PasswordResetPurpose) {
		locked, retryAfter := service.RegisterVerificationFailure(req.Email, common.PasswordResetPurpose)
		if locked {
			// 计划 §5.4：错误次数达到上限后删除验证码，窗口内即使猜中也无法继续使用。
			common.DeleteKey(req.Email, common.PasswordResetPurpose)
			if retryAfter <= 0 {
				retryAfter = 60
			}
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgUserVerificationAttemptsLimit),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "重置链接非法或已过期",
		})
		return
	}
	service.ClearVerificationFailures(req.Email, common.PasswordResetPurpose)
	// 由用户自行设置新密码，服务端不再生成并在响应中回传明文密码。
	if req.NewPassword == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "请设置新密码",
		})
		return
	}
	if err := common.Validate.Var(req.NewPassword, "required,min=8,max=20"); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "密码长度需为 8-20 位",
		})
		return
	}
	err = model.ResetUserPasswordByEmail(req.Email, req.NewPassword)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.DeleteKey(req.Email, common.PasswordResetPurpose)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}
