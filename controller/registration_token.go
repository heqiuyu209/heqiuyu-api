package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/service"
)

// GetRegistrationToken 签发一次性注册表单令牌
// GET /api/user/register/token
//
// 注册防护或表单令牌关闭时返回 404；成功时返回:
//
//	{"success": true, "message": "", "data": {"token": "...", "min_seconds": 3, "expires_in": 1800}}
//
// 内存 nonce 表达到上限时逐出最旧记录（service 层保证），不会因表满返回 5xx。
func GetRegistrationToken(c *gin.Context) {
	token, minSeconds, expiresIn, err := service.IssueRegistrationFormToken()
	if err != nil {
		if errors.Is(err, service.ErrFormTokenDisabled) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "Not Found",
			})
			return
		}
		common.SysLog("failed to issue registration form token: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to issue registration token",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"token":       token,
			"min_seconds": minSeconds,
			"expires_in":  expiresIn,
		},
	})
}
