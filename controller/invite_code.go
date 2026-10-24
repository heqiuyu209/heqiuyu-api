package controller

import (
	"errors"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/i18n"
	"github.com/heqiuyu/heqiuyu-api/service"
)

const (
	// registerEmailPurposeParam 是发码接口中"注册用途"的 purpose 参数值。
	registerEmailPurposeParam = "register"
	// inviteCodeSessionKey 是 OAuth 流程中暂存邀请码的 session 键。
	inviteCodeSessionKey = "invite_code"
)

// inviteCodeRequired 只读 invite_code_required，不受 registration_setting.enabled 影响。
func inviteCodeRequired() bool {
	return service.InviteCodeRequired()
}

// inviteCodeMessageKey 返回邀请码错误对应的 i18n 键。
func inviteCodeMessageKey(err error) string {
	if errors.Is(err, service.ErrInviteCodeRequired) {
		return i18n.MsgUserInviteCodeRequired
	}
	return i18n.MsgUserInviteCodeInvalid
}

// respondInviteCodeError 返回统一的邀请码错误响应（不记录邀请码明文）。
func respondInviteCodeError(c *gin.Context, err error) {
	common.ApiErrorI18n(c, inviteCodeMessageKey(err))
}

// validateInviteCode 统一调用 service 的共享实现（精确匹配、区分大小写、不记明文）。
func validateInviteCode(raw string) (int, error) {
	return service.ValidateInviteCode(raw)
}

// sessionInviteCode 读取 OAuth 流程中暂存的邀请码（GenerateOAuthCode 写入）。
func sessionInviteCode(session sessions.Session) string {
	if session == nil {
		return ""
	}
	value, ok := session.Get(inviteCodeSessionKey).(string)
	if !ok {
		return ""
	}
	return value
}
