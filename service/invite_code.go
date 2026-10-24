package service

import (
	"errors"
	"strings"

	"github.com/heqiuyu/heqiuyu-api/model"
	"github.com/heqiuyu/heqiuyu-api/setting/operation_setting"
)

// 邀请码错误。controller 据此映射 i18n；错误信息不包含邀请码明文。
var (
	ErrInviteCodeRequired = errors.New("invite code is required")
	ErrInviteCodeInvalid  = errors.New("invite code is invalid")
)

// InviteCodeRequired 只读 invite_code_required，不受 registration_setting.enabled 影响。
func InviteCodeRequired() bool {
	return operation_setting.GetRegistrationSetting().InviteCodeRequired
}

// ValidateInviteCode 校验邀请码并返回邀请人（aff_code 所属用户）ID。
//
// 邀请码来源为任意现有用户的 aff_code；仅 trim，精确匹配、区分大小写。
// 注意：MySQL 默认 collation 大小写不敏感，`aff_code = ?` 会匹配大小写变体，
// 因此这里显式取回 aff_code 并在应用层用 == 精确比较，保证变体一律按 invalid 处理。
// 本函数不打印邀请码明文。
func ValidateInviteCode(raw string) (int, error) {
	code := strings.TrimSpace(raw)
	if code == "" {
		return 0, ErrInviteCodeRequired
	}
	if model.DB == nil {
		return 0, ErrInviteCodeInvalid
	}
	var user model.User
	err := model.DB.Select("id, aff_code").First(&user, "aff_code = ?", code).Error
	if err != nil || user.Id == 0 || user.AffCode != code {
		return 0, ErrInviteCodeInvalid
	}
	return user.Id, nil
}
