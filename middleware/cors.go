package middleware

import (
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/heqiuyu/heqiuyu-api/common"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	// 安全修复：AllowAllOrigins（回显任意 Origin）与 AllowCredentials 不能同时为 true，
	// 否则任意网站都能携带用户 Cookie/凭证向本服务发起请求，造成跨站请求伪造风险。
	// 本服务 API 鉴权基于 Authorization 头而非 Cookie，故默认关闭凭证透传。
	config.AllowAllOrigins = true
	config.AllowCredentials = false
	// 如需跨域携带 Cookie（如管理后台与前端分域部署），可配置环境变量
	// CORS_ALLOW_ORIGINS=https://a.example.com,https://b.example.com 开启白名单模式：
	// 该模式下仅放行白名单域名，并自动恢复凭证透传。
	if origins := strings.TrimSpace(os.Getenv("CORS_ALLOW_ORIGINS")); origins != "" {
		list := make([]string, 0)
		for _, o := range strings.Split(origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				list = append(list, o)
			}
		}
		if len(list) > 0 {
			config.AllowAllOrigins = false
			config.AllowOrigins = list
			config.AllowCredentials = true
		}
	}
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	return cors.New(config)
}

func PoweredBy() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Heqiuyu-Api-Version", common.Version)
		c.Next()
	}
}
