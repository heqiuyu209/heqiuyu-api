package middleware

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/model"
)

// TestAuthFailureStatusCodeConvention 锁定鉴权失败的状态码约定（Q1）：
//
//	401 —— 身份/凭据失效（未登录、改密作废会话、账号被封禁、令牌无效、用户头缺失/不匹配）
//	403 —— 已认证但权限不足
//	200 + success:false 只用于业务错误，不得用于鉴权失败
//
// 背景：`auth_version` 失效与访问令牌无效原先返回 200+success:false，而前端只对 401
// 清登录态，导致被改密踢下线的设备"每个接口都失败但仍然显示已登录"。
func TestAuthFailureStatusCodeConvention(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	prevDB, prevLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = prevDB, prevLogDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	seed := func(username string, role, status int, affCode string) *model.User {
		u := &model.User{
			Username: username, Password: "x", Role: role, Status: status,
			Group: "default", AffCode: affCode,
		}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("seed %s: %v", username, err)
		}
		return u
	}
	root := seed("root-user", common.RoleRootUser, common.UserStatusEnabled, "AAAA")
	normal := seed("normal-user", common.RoleCommonUser, common.UserStatusEnabled, "BBBB")
	banned := seed("banned-user", common.RoleCommonUser, common.UserStatusDisabled, "CCCC")

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("status-convention-secret"))))

	// 受保护端点
	engine.GET("/user-endpoint", UserAuth(), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	engine.GET("/admin-endpoint", AdminAuth(), func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 测试专用登录：id 必填，authVersion 为空表示"会话里不写版本号"
	engine.GET("/__login", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("id", mustAtoi(c.Query("id")))
		if v := c.Query("authVersion"); v != "" {
			s.Set(SessionAuthVersionKey, mustAtoi(v))
		}
		if err := s.Save(); err != nil {
			c.String(http.StatusInternalServerError, "%v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	newClient := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Jar: jar}
	}
	login := func(client *http.Client, id int, authVersion string) {
		u := fmt.Sprintf("%s/__login?id=%d", server.URL, id)
		if authVersion != "" {
			u += "&authVersion=" + authVersion
		}
		res, err := client.Get(u)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("login returned %d", res.StatusCode)
		}
	}
	get := func(client *http.Client, path string, apiUserId int) int {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if apiUserId > 0 {
			req.Header.Set("Heqiuyu-Api-User", fmt.Sprint(apiUserId))
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	currentVersion := func(id int) string {
		v, err := model.GetUserAuthVersion(id)
		if err != nil {
			t.Fatalf("get auth version: %v", err)
		}
		return fmt.Sprint(v)
	}

	cases := []struct {
		name     string
		run      func() int
		expected int
	}{
		{
			name:     "无会话访问受保护端点 → 401",
			run:      func() int { return get(newClient(), "/user-endpoint", root.Id) },
			expected: http.StatusUnauthorized,
		},
		{
			name: "会话有效且角色足够 → 200",
			run: func() int {
				c := newClient()
				login(c, root.Id, currentVersion(root.Id))
				return get(c, "/user-endpoint", root.Id)
			},
			expected: http.StatusOK,
		},
		{
			name: "会话里的 auth_version 落后（改密作废）→ 401",
			run: func() int {
				c := newClient()
				login(c, root.Id, "0") // 库中为 0，这里刻意写入 1 造成不匹配
				login(c, root.Id, "1")
				return get(c, "/user-endpoint", root.Id)
			},
			expected: http.StatusUnauthorized,
		},
		{
			name: "已认证但角色不足（普通用户访问管理员端点）→ 403",
			run: func() int {
				c := newClient()
				login(c, normal.Id, currentVersion(normal.Id))
				return get(c, "/admin-endpoint", normal.Id)
			},
			expected: http.StatusForbidden,
		},
		{
			name: "账号被封禁 → 401（前端应登出）",
			run: func() int {
				c := newClient()
				login(c, banned.Id, "0")
				return get(c, "/user-endpoint", banned.Id)
			},
			expected: http.StatusUnauthorized,
		},
		{
			name: "缺少 Heqiuyu-Api-User 头 → 401",
			run: func() int {
				c := newClient()
				login(c, root.Id, "0")
				return get(c, "/user-endpoint", 0)
			},
			expected: http.StatusUnauthorized,
		},
		{
			name: "Heqiuyu-Api-User 与会话不一致 → 401",
			run: func() int {
				c := newClient()
				login(c, root.Id, "0")
				return get(c, "/user-endpoint", normal.Id)
			},
			expected: http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.run(); got != tc.expected {
				t.Errorf("期望 %d，实际 %d", tc.expected, got)
			}
		})
	}
}

func mustAtoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
