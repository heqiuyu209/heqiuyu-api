package middleware

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMaskInviteCodeInPath 覆盖访问日志中 invite_code 脱敏（INV-S2）。
//
// 已知残余：仅匹配字面 key `invite_code`，percent-encoded 的 `invite%5Fcode`
// 不会命中（gin 的 RawQuery 通常保留原文），已在验收文档记录。
func TestMaskInviteCodeInPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no query unchanged",
			in:   "/api/verification",
			want: "/api/verification",
		},
		{
			name: "invite code masked and other params preserved",
			in:   "/api/verification?email=a%40b.com&invite_code=AbC123&purpose=register",
			want: "/api/verification?email=a%40b.com&invite_code=***&purpose=register",
		},
		{
			name: "invite code first param",
			in:   "/api/oauth/state?invite_code=SecretCode&aff=XYZ",
			want: "/api/oauth/state?invite_code=***&aff=XYZ",
		},
		{
			name: "multiple invite code params all masked",
			in:   "/api/oauth/wechat?invite_code=A&x=1&invite_code=B",
			want: "/api/oauth/wechat?invite_code=***&x=1&invite_code=***",
		},
		{
			name: "empty value masked",
			in:   "/api/verification?invite_code=&email=a%40b.com",
			want: "/api/verification?invite_code=***&email=a%40b.com",
		},
		{
			name: "value containing equals is fully masked",
			in:   "/api/verification?invite_code=abc=def&x=1",
			want: "/api/verification?invite_code=***&x=1",
		},
		{
			name: "other params only unchanged",
			in:   "/api/verification?email=a%40b.com&purpose=register&x=1",
			want: "/api/verification?email=a%40b.com&purpose=register&x=1",
		},
		{
			name: "empty query unchanged",
			in:   "/api/verification?",
			want: "/api/verification?",
		},
		{
			// 已知残余：percent-encoded key 不匹配字面量，不脱敏。
			name: "percent-encoded key is known residual (not masked)",
			in:   "/api/verification?invite%5Fcode=AbC123&x=1",
			want: "/api/verification?invite%5Fcode=AbC123&x=1",
		},
		{
			// key 精确匹配：不同大小写不当作 invite_code（残余）。
			name: "case variant key not masked (known residual)",
			in:   "/api/verification?Invite_Code=AbC123",
			want: "/api/verification?Invite_Code=AbC123",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, maskInviteCodeInPath(tc.in))
			if strings.Contains(tc.in, "invite_code=") {
				require.NotContains(t, maskInviteCodeInPath(tc.in), "AbC123")
				require.NotContains(t, maskInviteCodeInPath(tc.in), "SecretCode")
			}
		})
	}
}

// TestMaskInviteCodeInPathKeepsLogFormat 确认 mask 只改 query，不影响路径/日志格式。
func TestMaskInviteCodeInPathKeepsLogFormat(t *testing.T) {
	masked := maskInviteCodeInPath("/api/oauth/state?invite_code=abc123&aff=AFF1")
	require.True(t, strings.HasPrefix(masked, "/api/oauth/state?"))
	require.NotContains(t, masked, "abc123")
	require.Contains(t, masked, "invite_code=***")
	require.Contains(t, masked, "aff=AFF1")
}
