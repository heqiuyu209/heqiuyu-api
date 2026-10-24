package middleware

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/service"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// registrationPayload 是注册请求中防护中间件需要的字段。
//
// 其余字段（username/password/email/verification_code 等）仍由 controller.Register
// 自行解析；这里读取并复位请求体，不影响后续解析。
type registrationPayload struct {
	Website   string `json:"website"`
	FormToken string `json:"form_token"`
}

func abortRegistrationRejected(c *gin.Context, reason string) {
	common.SysLog("[registration-guard] registration rejected (" + reason + ") ip=" + c.ClientIP())
	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"message": i18n.T(c, i18n.MsgUserRegistrationRejected),
	})
	c.Abort()
}

// readRegistrationPayload 读取并复位注册请求体，解析出防护字段。
//
// 注意：这里不依赖 Content-Type（controller.Register 始终按 JSON 解析请求体，
// 若依赖 Content-Type 解析，攻击者可用 text/plain 绕过蜜罐/表单令牌），
// 而是直接对原始字节做 JSON 解析，失败时原样交回 controller 处理。
//
// 当请求体超过 RegistrationBodyLimit 设置的上限时返回
// common.IsRequestBodyTooLargeError 可识别的错误，由调用方返回 413。
func readRegistrationPayload(c *gin.Context) (registrationPayload, error) {
	var payload registrationPayload
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		// 读取阶段超限（Content-Length 未知的分块传输等）也要返回 413。
		if common.IsRequestBodyTooLargeError(err) {
			return payload, err
		}
		return payload, nil
	}
	raw, err := storage.Bytes()
	if err != nil {
		return payload, nil
	}
	// 复位请求体：无论解析成功与否，controller 都必须能重新读取完整请求体。
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return payload, nil
	}
	c.Request.Body = io.NopCloser(storage)
	_ = json.Unmarshal(raw, &payload)
	return payload, nil
}

// RegistrationPayloadGuard 校验注册请求体中的蜜罐字段与一次性表单令牌。
//
//   - 蜜罐：registration_setting.honeypot_enabled 时，website 非空即拒绝（机器人填充）。
//   - 表单令牌：registration_setting.form_token_enabled 时，必须提供合法且未使用过的
//     form_token（HMAC 签名 + 最短填写时间 + 有效期 + 一次性 nonce）。
//
// 注册防护总开关关闭时直接放行。
func RegistrationPayloadGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		setting := operation_setting.GetRegistrationSetting()
		if !setting.Enabled {
			c.Next()
			return
		}
		honeypotEnabled := setting.HoneypotEnabled
		formTokenEnabled := service.RegistrationFormTokenEnabled()
		if !honeypotEnabled && !formTokenEnabled {
			c.Next()
			return
		}

		payload, err := readRegistrationPayload(c)
		if err != nil {
			if common.IsRequestBodyTooLargeError(err) {
				abortRequestBodyTooLarge(c)
				return
			}
			// 其它读取错误交给 controller 统一返回参数错误。
			c.Next()
			return
		}

		if honeypotEnabled && strings.TrimSpace(payload.Website) != "" {
			abortRegistrationRejected(c, "honeypot")
			return
		}

		if formTokenEnabled {
			if strings.TrimSpace(payload.FormToken) == "" {
				abortRegistrationRejected(c, "missing form_token")
				return
			}
			if err := service.ConsumeRegistrationFormToken(payload.FormToken); err != nil {
				// 配置在请求期间被热关闭：不因开关变化误伤正常注册。
				if errors.Is(err, service.ErrFormTokenDisabled) {
					c.Next()
					return
				}
				abortRegistrationRejected(c, "form_token: "+err.Error())
				return
			}
		}

		c.Next()
	}
}
