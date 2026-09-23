package service

import (
	"fmt"
	"strings"

	"github.com/heqiuyu/heqiuyu-api/dto"
)

// ReplaceContentValues 按顺序将模板中的 {{value}} 占位符替换为 values。
// 全项目通知模板统一使用 dto.ContentValueParam（{{value}}）作为占位符，
// 邮件 / Bark / Gotify / Webhook 均应经由本函数替换，避免各渠道实现分叉。
func ReplaceContentValues(content string, values []interface{}) string {
	for _, value := range values {
		content = strings.Replace(content, dto.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}
	return content
}
