package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// TurnstileVerifyURL 是 Cloudflare siteverify 校验地址。
//
// 声明为包级变量，便于测试替换为 httptest 服务器地址。
var TurnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

const (
	// turnstileSessionKey 是旧版"会话内复用 Turnstile 校验结果"的 session 键。
	turnstileSessionKey = "turnstile"
	// turnstileSessionTTL 是会话标记的有效期（旧实现只要标记存在就永久放行）。
	turnstileSessionTTL = 30 * time.Minute
	// turnstileVerifyTimeout 是 siteverify 请求的超时时间。
	turnstileVerifyTimeout = 5 * time.Second
)

// errTurnstileVerificationFailed 表示 Cloudflare 返回 success=false。
var errTurnstileVerificationFailed = errors.New("Turnstile 校验失败，请刷新重试！")

// turnstileFailureMessage 是对外统一文案：网络错误/内部错误细节只写日志，不返回前端。
const turnstileFailureMessage = "Turnstile 校验失败，请刷新重试！"

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

// turnstileHTTPClient 使用独立超时的 client，避免 SMTP/第三方故障时请求悬挂。
var turnstileHTTPClient = &http.Client{Timeout: turnstileVerifyTimeout}

// registrationTurnstileStrictEnabled 判断是否启用"每次注册/发码都强制校验"。
//
// 只读取 turnstile_strict，不再受 registration_setting.enabled 影响：
// 注册防护总开关用于限流/蜜罐/令牌等，Turnstile 的严格模式回滚由
// turnstile_strict=false 显式控制（runbook 同步此语义）。
func registrationTurnstileStrictEnabled() bool {
	return operation_setting.GetRegistrationSetting().TurnstileStrict
}

// turnstileVerify 调用 siteverify 校验 token。
func turnstileVerify(c *gin.Context, token string) error {
	requestURL := TurnstileVerifyURL
	if requestURL == "" {
		return errors.New("Turnstile 校验地址未配置")
	}
	form := url.Values{
		"secret":   {common.TurnstileSecretKey},
		"response": {token},
		"remoteip": {c.ClientIP()},
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), turnstileVerifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rawRes, err := turnstileHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer rawRes.Body.Close()

	var res turnstileCheckResponse
	if err := json.NewDecoder(io.LimitReader(rawRes.Body, 64*1024)).Decode(&res); err != nil {
		return err
	}
	if !res.Success {
		return errTurnstileVerificationFailed
	}
	return nil
}

// verifyTurnstileQueryToken 从 query 读取 turnstile token 并校验；失败时直接写响应并 abort。
func verifyTurnstileQueryToken(c *gin.Context) bool {
	response := c.Query("turnstile")
	if response == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Turnstile token 为空",
		})
		c.Abort()
		return false
	}
	if err := turnstileVerify(c, response); err != nil {
		common.SysLog(fmt.Sprintf("[turnstile] verification failed for ip=%s: %v", c.ClientIP(), err))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": turnstileFailureMessage,
		})
		c.Abort()
		return false
	}
	return true
}

// turnstileSessionValid 判断会话标记是否仍然有效。
//
// 支持新格式（int64 时间戳）与 time.Time；旧版本写入的 bool true 没有时间戳，
// 一律视为过期，强制重新校验（修复"一次校验永久复用"的 L5 问题）。
func turnstileSessionValid(value any) bool {
	switch checkedAt := value.(type) {
	case int64:
		return time.Since(time.Unix(checkedAt, 0)) < turnstileSessionTTL
	case int:
		return time.Since(time.Unix(int64(checkedAt), 0)) < turnstileSessionTTL
	case time.Time:
		return time.Since(checkedAt) < turnstileSessionTTL
	default:
		return false
	}
}

// turnstileSessionCheck 是旧版检查逻辑：校验通过后把会话标记写成时间戳，30 分钟内复用。
func turnstileSessionCheck(c *gin.Context) {
	session := sessions.Default(c)
	if turnstileSessionValid(session.Get(turnstileSessionKey)) {
		c.Next()
		return
	}
	if !verifyTurnstileQueryToken(c) {
		return
	}
	session.Set(turnstileSessionKey, time.Now().Unix())
	if err := session.Save(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "无法保存会话信息，请重试",
			"success": false,
		})
		c.Abort()
		return
	}
	c.Next()
}

// TurnstileCheck 保留旧行为（会话内复用，30 分钟有效），用于登录/签到等场景。
func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.TurnstileCheckEnabled {
			c.Next()
			return
		}
		turnstileSessionCheck(c)
	}
}

// TurnstileCheckStrict 用于注册/发码等高风险接口。
//
// 开启 registration_setting.turnstile_strict（默认）时，每次请求都重新校验、
// 不读取也不写入 session；否则退回会话复用语义。
func TurnstileCheckStrict() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.TurnstileCheckEnabled {
			c.Next()
			return
		}
		if !registrationTurnstileStrictEnabled() {
			turnstileSessionCheck(c)
			return
		}
		if !verifyTurnstileQueryToken(c) {
			return
		}
		c.Next()
	}
}
