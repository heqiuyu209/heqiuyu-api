package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/heqiuyu/heqiuyu-api/common"
)

const RouteTagKey = "route_tag"

// inviteCodeQueryKey 是访问日志中需要脱敏的 query 参数名（可复用注册凭据）。
const inviteCodeQueryKey = "invite_code"

// maskInviteCodeInPath 对访问日志里的 invite_code query 值做掩码（invite_code=***）。
//
// gin 的 param.Path 形如 "/api/verification?invite_code=ABC&x=1"（logger 中间件已把
// RawQuery 拼到 Path 上）。这里只替换 invite_code 的值，保留其它参数、顺序与日志格式；
// 覆盖 /api/verification、/api/oauth/state、/api/oauth/wechat 等带该参数的请求。
func maskInviteCodeInPath(path string) string {
	queryIndex := strings.IndexByte(path, '?')
	if queryIndex < 0 {
		return path
	}
	prefix := path[:queryIndex+1]
	rawQuery := path[queryIndex+1:]
	if rawQuery == "" {
		return path
	}

	parts := strings.Split(rawQuery, "&")
	changed := false
	for i, part := range parts {
		key := part
		if eq := strings.IndexByte(part, '='); eq >= 0 {
			key = part[:eq]
		}
		if key == inviteCodeQueryKey {
			parts[i] = key + "=***"
			changed = true
		}
	}
	if !changed {
		return path
	}
	return prefix + strings.Join(parts, "&")
}

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID, _ = param.Keys[common.RequestIdKey].(string)
		}
		tag, _ := param.Keys[RouteTagKey].(string)
		if tag == "" {
			tag = "web"
		}
		return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			tag,
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			maskInviteCodeInPath(param.Path),
		)
	}))
}
