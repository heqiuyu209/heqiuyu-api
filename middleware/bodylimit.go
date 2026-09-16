package middleware

import (
	"fmt"
	"net/http"

	"github.com/heqiuyu/heqiuyu-api/constant"

	"github.com/gin-gonic/gin"
)

// BodyLimit 为所有入站请求设置请求体大小上限。
//
// 必须在引擎上注册，且**早于任何路由注册**：Gin 在注册路由时就固定了该路由的中间件链，
// 因此把限额放在某个分组里（例如 relay 的 DecompressRequestMiddleware）无法覆盖
// 更早注册的 /api 分组，未认证接口就能带着任意大的 JSON body 打进来造成内存耗尽
// （审计报告 H5）。
//
// 中继路径会在本中间件之上再叠加"解压后"的限额（middleware.DecompressRequestMiddleware），
// 两者叠加是安全的：这里限制传输体积，那里限制解压后的体积。
func BodyLimit() gin.HandlerFunc {
	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 32
	}
	maxBytes := int64(maxMB) << 20

	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}

		// Content-Length 已知时直接拒绝，给出明确的 413 而不是让解码器报语法错误。
		if c.Request.ContentLength > maxBytes {
			abortBodyTooLarge(c, maxMB)
			return
		}

		// 分块传输或 Content-Length 不可信时，用 MaxBytesReader 硬性截断。
		// 超限会让读取方返回错误，超出部分不会被缓冲。
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// abortBodyTooLarge 同时提供 {success,message} 与 OpenAI 风格 {error} 两种字段，
// 以便后台与 API 客户端都能正确识别。
func abortBodyTooLarge(c *gin.Context, maxMB int) {
	message := fmt.Sprintf("请求体过大，上限为 %d MB", maxMB)
	c.JSON(http.StatusRequestEntityTooLarge, gin.H{
		"success": false,
		"message": message,
		"error": gin.H{
			"message": message,
			"type":    "heqiuyu_api_error",
			"code":    "request_body_too_large",
		},
	})
	c.Abort()
}
