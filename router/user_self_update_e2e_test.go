package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/heqiuyu/heqiuyu-api/middleware"
	"github.com/heqiuyu/heqiuyu-api/model"
)

// TestSelfChangePasswordKeepsUsername 锁定「个人中心改自己的密码会把用户名/显示名清空」。
//
// 回归背景：PUT /api/user/self 的前端调用方（change-password-dialog）只发送
// original_password + password，而控制器把请求体反序列化成 model.User 后，
// 用其中的零值 username/display_name 整列写回。于是用户一改自己的密码：
//   - username 变成 ""  → 中间件 validUserInfo 直接 403，该账号被锁死无法再登录；
//   - display_name 变成 ""；
//   - 管理员随后想改该用户的密码，编辑抽屉里用户名是空的且不可编辑，
//     又被 zod 的 "Username is required" 拦住 —— 彻底死锁（用户反馈的现象）。
//
// 本测试用真实路由 + 真实中间件 + 真实 sqlite 复现：改密必须成功，且
// username / display_name 必须原样保留。
func TestSelfChangePasswordKeepsUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.PasswordLoginEnabled = true
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

	oldPwd := "OldPass1234"
	hashed, err := common.Password2Hash(oldPwd)
	if err != nil {
		t.Fatalf("hash pwd: %v", err)
	}
	user := model.User{
		Username:    "ceshi",
		Password:    hashed,
		DisplayName: "ceshi-display",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "vip",
		AffCode:     "VICT",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("e2e-secret"))))
	SetApiRouter(engine)
	engine.GET("/__test_login_self", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("id", user.Id)
		version, verr := model.GetUserAuthVersion(user.Id)
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

	server := httptest.NewServer(engine)
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Get(server.URL + "/__test_login_self")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = resp.Body.Close()

	// 与前端 change-password-dialog 完全一致的 payload：只有原密码 + 新密码
	payload, _ := json.Marshal(map[string]any{
		"original_password": oldPwd,
		"password":          "NewPass5678",
	})
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/user/self", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Heqiuyu-Api-User", fmt.Sprint(user.Id))
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/user/self: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	t.Logf("PUT /api/user/self (self change password) -> %d %s", res.StatusCode, truncate(string(body), 200))

	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"success":true`) {
		t.Fatalf("改自己的密码应当成功，实际 %d %s", res.StatusCode, string(body))
	}

	var after model.User
	if err := db.Where("id = ?", user.Id).First(&after).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	t.Logf("after: username=%q display_name=%q", after.Username, after.DisplayName)

	if after.Username != "ceshi" {
		t.Errorf("改密后 username 被写成 %q，必须保持 %q（否则该账号会因 validUserInfo 无法登录）", after.Username, "ceshi")
	}
	if after.DisplayName != "ceshi-display" {
		t.Errorf("改密后 display_name 被写成 %q，必须保持 %q", after.DisplayName, "ceshi-display")
	}
	if after.Group != "vip" {
		t.Errorf("改密后 group 被写成 %q，必须保持 %q", after.Group, "vip")
	}
	if !common.ValidatePasswordAndHash("NewPass5678", after.Password) {
		t.Errorf("新密码未生效")
	}
	if after.AuthVersion != 1 {
		t.Errorf("改密应让 auth_version 自增为 1（旧会话失效），实际 %d", after.AuthVersion)
	}

	// 改密后：新会话版本必须匹配（说明账号仍然可用）
	if version, err := model.GetUserAuthVersion(user.Id); err == nil && version != after.AuthVersion {
		t.Errorf("auth_version 读取不一致: %d vs %d", version, after.AuthVersion)
	}
}

// TestAdminUpdateUserCannotBlankUsername 锁定管理员更新用户时「空用户名不得清空库里用户名」，
// 以及「补填用户名可以修复历史空用户名脏数据」。
func TestAdminUpdateUserCannotBlankUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.PasswordLoginEnabled = true
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

	rootPwd, _ := common.Password2Hash("rootpass123")
	root := model.User{Username: "root", Password: rootPwd, Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "ROOT"}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("create root: %v", err)
	}

	// 历史脏数据：username 为空（已被「自改密码清空用户名」缺陷污染），display_name 还在
	brokenPwd, _ := common.Password2Hash("victimpwd123")
	broken := model.User{Username: "", Password: brokenPwd, DisplayName: "ceshi", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "vip", AffCode: "VICT"}
	if err := db.Create(&broken).Error; err != nil {
		t.Fatalf("create broken user: %v", err)
	}

	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("e2e-secret"))))
	SetApiRouter(engine)
	engine.GET("/__test_login_as_root", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("id", root.Id)
		version, verr := model.GetUserAuthVersion(root.Id)
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

	server := httptest.NewServer(engine)
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Get(server.URL + "/__test_login_as_root")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = resp.Body.Close()

	doPut := func(payload map[string]any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, rerr := http.NewRequest(http.MethodPut, server.URL+"/api/user/", bytes.NewReader(raw))
		if rerr != nil {
			t.Fatalf("new request: %v", rerr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Heqiuyu-Api-User", fmt.Sprint(root.Id))
		res, rerr := client.Do(req)
		if rerr != nil {
			t.Fatalf("PUT /api/user/: %v", rerr)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	// 1) 提交空用户名 + 新密码：不得静默清空（用户名本来就是空的，这里断言
	//    请求被明确拒绝，避免前端以为改成功、账号却依旧无法登录）。
	st, body := doPut(map[string]any{
		"id":           broken.Id,
		"username":     "",
		"display_name": "ceshi",
		"password":     "newpass456",
		"group":        "vip",
	})
	t.Logf("PUT empty username -> %d %s", st, truncate(body, 200))
	if strings.Contains(body, `"success":true`) {
		t.Errorf("空用户名不应被接受（会把账号锁死），实际 %d %s", st, body)
	}

	// 2) 补填用户名可以修复脏数据：
	st, body = doPut(map[string]any{
		"id":           broken.Id,
		"username":     "ceshi",
		"display_name": "ceshi",
		"password":     "newpass456",
		"group":        "vip",
	})
	t.Logf("PUT repaired username -> %d %s", st, truncate(body, 200))
	if st != http.StatusOK || !strings.Contains(body, `"success":true`) {
		t.Fatalf("补填用户名应当成功，实际 %d %s", st, body)
	}
	var repaired model.User
	if err := db.Where("id = ?", broken.Id).First(&repaired).Error; err != nil {
		t.Fatalf("reload broken user: %v", err)
	}
	if repaired.Username != "ceshi" {
		t.Errorf("补填后 username = %q，期望 \"ceshi\"", repaired.Username)
	}
	if !common.ValidatePasswordAndHash("newpass456", repaired.Password) {
		t.Errorf("补填用户名时的新密码未生效")
	}
}
