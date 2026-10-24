package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/i18n"
)

// RegistrationBodyMaxBytes 是注册接口允许的最大请求体（16KB）。
//
// 注册请求体只包含用户名/密码/邮箱/验证码/蜜罐/表单令牌等小字段，
// 16KB 足够；限制它在 Turnstile 校验之前，避免未认证请求用大 body 消耗资源。
const RegistrationBodyMaxBytes int64 = 16 * 1024

// abortRequestBodyTooLarge 返回注册请求体过大的统一 413 响应。
func abortRequestBodyTooLarge(c *gin.Context) {
	c.JSON(http.StatusRequestEntityTooLarge, gin.H{
		"success": false,
		"message": i18n.T(c, i18n.MsgUserRequestBodyTooLarge),
	})
	c.Abort()
}

// RegistrationBodyLimit 为注册接口设置请求体硬上限。
//
// 处理方式：
//  1. Content-Length 已知且超限：直接 413，不读取 body；
//  2. 否则把 c.Request.Body 包成 http.MaxBytesReader。
//
// 边界说明：只有 RegistrationPayloadGuard 读取请求体的路径保证把读取超限映射为
// 413（通过 common.IsRequestBodyTooLargeError）；若 PayloadGuard 未读取 body
// （例如注册防护总开关关闭、或蜜罐与表单令牌均关闭），controller.Register 直接
// decode 超限请求体会回落为"无效参数"，而不是 413。该回落边界已记录在 runbook。
func RegistrationBodyLimit(maxBytes int64) gin.HandlerFunc {
	if maxBytes <= 0 {
		maxBytes = RegistrationBodyMaxBytes
	}
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxBytes {
			abortRequestBodyTooLarge(c)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
