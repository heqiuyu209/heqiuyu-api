package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heqiuyu/heqiuyu-api/constant"

	"github.com/gin-gonic/gin"
)

// 回归测试（审计报告 H5）：请求体上限必须真正生效。
//
// 之前唯一的体积限制挂在 relay 分组上，而 /api 分组在此前就完成了路由注册，
// 因此未认证接口可以带着任意大的 body 打进来。
func TestBodyLimitRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	original := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1 // 1 MiB，便于构造超限请求
	defer func() { constant.MaxRequestBodyMB = original }()

	router := gin.New()
	router.Use(BodyLimit())
	router.POST("/api/echo", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusBadRequest, "read error")
			return
		}
		c.String(http.StatusOK, "%d", len(body))
	})

	// 1) 超过上限且 Content-Length 已知：直接 413，handler 不会被执行
	oversized := strings.Repeat("a", (1<<20)+1)
	req := httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(oversized))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for an oversized body, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 2) 未超限：正常放行，且 body 内容完整可读
	small := strings.Repeat("a", 1024)
	req = httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader(small))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "1024" {
		t.Fatalf("expected 200 with 1024 bytes, got %d %q", rec.Code, rec.Body.String())
	}
}

// GET / HEAD 不应受影响（它们通常没有 body）。
func TestBodyLimitSkipsGetRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(BodyLimit())
	router.GET("/api/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "pong" {
		t.Fatalf("expected GET to pass through, got %d %q", rec.Code, rec.Body.String())
	}
}
