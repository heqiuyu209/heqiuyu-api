package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/heqiuyu/heqiuyu-api/common"
	"github.com/heqiuyu/heqiuyu-api/middleware"
	"github.com/heqiuyu/heqiuyu-api/model"
)

// TestRootAdminManageOtherUserE2E 端到端复现「超管改别人密码 / 删用户」：
// 真实路由 + 真实中间件 + 真实 sqlite（内存），用带 cookie jar 且自动跟随重定向的
// HTTP 客户端，与浏览器行为一致（包括前端 api.ts 里带尾斜杠的 DELETE URL）。
func TestRootAdminManageOtherUserE2E(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.PasswordLoginEnabled = true
	common.TurnstileCheckEnabled = false
	common.RegisterEnabled = true

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.User{}, &model.Log{}, &model.Token{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 种子数据：1 = 超管(root, role 100)，2 = 普通用户(role 1)
	rootPwd, err := common.Password2Hash("rootpass123")
	if err != nil {
		t.Fatalf("hash root pwd: %v", err)
	}
	victimPwd, err := common.Password2Hash("victimpwd123")
	if err != nil {
		t.Fatalf("hash victim pwd: %v", err)
	}
	root := model.User{Username: "root", Password: rootPwd, Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "ROOT"}
	victim := model.User{Username: "victim", Password: victimPwd, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "VICT"}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := db.Create(&victim).Error; err != nil {
		t.Fatalf("create victim: %v", err)
	}
	t.Logf("seeded root id=%d role=%d, victim id=%d role=%d", root.Id, root.Role, victim.Id, victim.Role)

	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("e2e-secret"))))
	SetApiRouter(engine)

	// 直接注入会话（避免依赖登录流程本身）：模拟超管已登录
	engine.GET("/__test_login_as_root", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("id", root.Id)
		if err := s.Save(); err != nil {
			c.String(http.StatusInternalServerError, "save session: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	})

	// 同样的方式给"被改密的用户"造一个会话，并按 setupLogin 的做法写入签发时的
	// auth_version —— 这样才触发 sessionAuthVersionValid 的失效逻辑。
	engine.GET("/__test_login_as_victim", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("id", victim.Id)
		version, verr := model.GetUserAuthVersion(victim.Id)
		if verr != nil {
			c.String(http.StatusInternalServerError, "get auth version: %v", verr)
			return
		}
		s.Set(middleware.SessionAuthVersionKey, version)
		if err := s.Save(); err != nil {
			c.String(http.StatusInternalServerError, "save session: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	})

	// 诊断路由：回显服务端从 cookie 里实际读到的会话内容（类型 + 值）
	engine.GET("/__test_session_dump", func(c *gin.Context) {
		s := sessions.Default(c)
		v := s.Get(middleware.SessionAuthVersionKey)
		idv := s.Get("id")
		c.JSON(http.StatusOK, gin.H{
			"auth_version": fmt.Sprintf("%T=%v", v, v),
			"id":           fmt.Sprintf("%T=%v", idv, idv),
		})
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar} // 默认跟随重定向，与浏览器一致

	// 建立会话
	resp, err := client.Get(server.URL + "/__test_login_as_root")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = resp.Body.Close()
	if u, perr := url.Parse(server.URL); perr == nil {
		t.Logf("session bootstrap -> %d, cookies=%d", resp.StatusCode, len(jar.Cookies(u)))
	}

	do := func(method, path, body string) (int, string) {
		var rdr io.Reader
		if body != "" {
			rdr = bytes.NewReader([]byte(body))
		}
		req, err := http.NewRequest(method, server.URL+path, rdr)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Heqiuyu-Api-User", fmt.Sprint(root.Id))
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	// 1) 列表（基线，确认鉴权链路是通的）
	st, body := do(http.MethodGet, "/api/user/?p=1&page_size=10", "")
	t.Logf("GET  /api/user/            -> %d %s", st, truncate(body, 120))

	// 1b) 被改密用户先自己登录出一个会话，用于验证"改密后旧会话立刻失效"
	victimJar, _ := cookiejar.New(nil)
	victimClient := &http.Client{Jar: victimJar}
	vresp, verr := victimClient.Get(server.URL + "/__test_login_as_victim")
	if verr != nil {
		t.Fatalf("victim login: %v", verr)
	}
	_ = vresp.Body.Close()

	// 注意：本项目部分鉴权失败路径用 HTTP 200 + success:false 返回（见 middleware.denyJSON），
	// 所以这里必须同时看状态码和响应体，不能只看状态码。
	doVictim := func(path string) (int, string) {
		req, rerr := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if rerr != nil {
			t.Fatalf("new victim request: %v", rerr)
		}
		req.Header.Set("Heqiuyu-Api-User", fmt.Sprint(victim.Id))
		res, rerr := victimClient.Do(req)
		if rerr != nil {
			t.Fatalf("victim GET %s: %v", path, rerr)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	stBefore, bodyBefore := doVictim("/api/user/self")
	t.Logf("GET  /api/user/self (被改密用户改密前) -> %d %s", stBefore, truncate(bodyBefore, 80))

	// 1c) 已认证但权限不足：必须 403（Q1 修复后不再用 200+success:false）
	stForbidden, bodyForbidden := doVictim("/api/user/?p=1&page_size=10")
	t.Logf("GET  /api/user/ (普通用户访问管理接口) -> %d %s", stForbidden, truncate(bodyForbidden, 100))
	if stForbidden != http.StatusForbidden {
		t.Errorf("权限不足应返回 403，实际 %d %s", stForbidden, bodyForbidden)
	}

	// 2) 改别人密码：与前端 updateUser 完全一致的 payload
	pwdPayload, _ := json.Marshal(map[string]any{
		"id":           victim.Id,
		"username":     "victim",
		"display_name": "victim",
		"password":     "newpass456",
		"group":        "default",
	})
	st, body = do(http.MethodPut, "/api/user/", string(pwdPayload))
	t.Logf("PUT  /api/user/  (改密码)  -> %d %s", st, truncate(body, 200))

	var afterPwd model.User
	if err := db.Where("id = ?", victim.Id).First(&afterPwd).Error; err != nil {
		t.Fatalf("reload victim: %v", err)
	}
	pwdChanged := common.ValidatePasswordAndHash("newpass456", afterPwd.Password)
	t.Logf("    密码是否已改为 newpass456 = %v", pwdChanged)

	// 2b) 改密即凭据轮换（审计报告 R1）：目标的旧会话必须立刻失效
	stAfter, bodyAfter := doVictim("/api/user/self")
	storedVersion, _ := model.GetUserAuthVersion(victim.Id)
	dumpReq, _ := http.NewRequest(http.MethodGet, server.URL+"/__test_session_dump", nil)
	dumpReq.Header.Set("Heqiuyu-Api-User", fmt.Sprint(victim.Id))
	dumpBody := ""
	if dumpRes, derr := victimClient.Do(dumpReq); derr == nil {
		b, _ := io.ReadAll(dumpRes.Body)
		_ = dumpRes.Body.Close()
		dumpBody = string(b)
	}
	t.Logf("GET  /api/user/self (被改密用户改密后) -> %d %s", stAfter, truncate(bodyAfter, 120))
	t.Logf("    DB 中 auth_version=%d；服务端从 cookie 读到的会话=%s", storedVersion, dumpBody)

	// 改密前必须真的能用（success:true）
	beforeOK := strings.Contains(bodyBefore, `"success":true`)
	if !beforeOK {
		t.Errorf("前置条件失败：被改密用户的会话在改密前就不可用（%d %s）", stBefore, bodyBefore)
	}
	// 改密后旧会话必须立刻失效，且必须是 401（Q1 修复后统一为 401：
	// 前端只有收到 401 才会清登录态并跳转登录页）
	if stAfter != http.StatusUnauthorized {
		t.Errorf("改密后旧会话应返回 401，实际 %d %s", stAfter, bodyAfter)
	}
	// 失效后服务端已清空会话（cookie 被清），再次请求同样是 401
	stAgain, bodyAgain := doVictim("/api/user/self")
	if stAgain != http.StatusUnauthorized {
		t.Errorf("旧会话被清理后再次请求应返回 401，实际 %d %s", stAgain, bodyAgain)
	}

	// 3) 删除用户：与前端 features/users/api.ts 一致（不带尾斜杠；
	//    带尾斜杠的旧写法会先吃一个 307 重定向，见 user_admin_route_probe_test.go）
	st, body = do(http.MethodDelete, fmt.Sprintf("/api/user/%d", victim.Id), "")
	t.Logf("DEL  /api/user/%d (删用户)  -> %d %s", victim.Id, st, truncate(body, 200))

	var cnt int64
	db.Unscoped().Model(&model.User{}).Where("id = ?", victim.Id).Count(&cnt)
	t.Logf("    数据库中该用户剩余行数 = %d", cnt)

	if !pwdChanged {
		t.Errorf("改别人密码失败：PUT /api/user/ 返回 %d %s", st, body)
	}
	if cnt != 0 {
		t.Errorf("删除用户失败：DELETE /api/user/%d/ 后仍有 %d 行", victim.Id, cnt)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
