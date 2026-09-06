package fiber

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	fastcors "github.com/zhoudm1743/go-fast-framework/http/cors"
)

func newWildcardCredentialsApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Use(wildcardCredentialsCORSMiddleware(fastcors.Options{
		AllowOrigins:     []string{"*"},
		AllowMethods:     "GET,POST",
		AllowHeaders:     "X-Token",
		AllowCredentials: true,
		MaxAge:           600,
		ExposeHeaders:    "X-Total-Count",
	}))
	app.Get("/ping", func(c *fiber.Ctx) error {
		return c.SendString("pong")
	})
	return app
}

func TestWildcardCredentialsCORSEchoesOrigin(t *testing.T) {
	app := newWildcardCredentialsApp(t)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://a.com")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://a.com" {
		t.Errorf("通配 + 凭据时应回显请求 Origin，实际 %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("应输出 ACAC=true，实际 %q", got)
	}
	if got := resp.Header.Get("Access-Control-Expose-Headers"); got != "X-Total-Count" {
		t.Errorf("应输出暴露头，实际 %q", got)
	}
}

func TestWildcardCredentialsCORSPreflight(t *testing.T) {
	app := newWildcardCredentialsApp(t)

	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	req.Header.Set("Origin", "https://a.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("预检请求期望 204，实际 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got != "GET,POST" {
		t.Errorf("预检应输出 ACAM，实际 %q", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "X-Token" {
		t.Errorf("预检应输出 ACAH，实际 %q", got)
	}
	if got := resp.Header.Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("预检应输出 Max-Age，实际 %q", got)
	}
}

func TestWildcardCredentialsCORSNoOrigin(t *testing.T) {
	app := newWildcardCredentialsApp(t)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("无 Origin 头的请求不属于 CORS 范畴，不应输出 ACAO")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("请求应正常处理，实际 %d", resp.StatusCode)
	}
}
