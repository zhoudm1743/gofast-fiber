package fiber

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/zhoudm1743/go-fast-framework/contracts"
)

// 复现响应双写缺陷场景：业务 helper 内部已写出 404 并把结果以 error 上抛。
// 旧版 NotFound 成功后返回 nil → err==nil → 调用方继续执行又写一次，响应体
// 出现两段拼接 JSON；新版返回哨兵 ErrResponseSent，wrap 识别后以 nil 结束请求，
// 不再把哨兵交给 fiber 默认错误处理（否则会对已写出的响应追加 500 文本）。
func TestWrapRecognizesResponseSentinelNoDoubleWrite(t *testing.T) {
	app := fiber.New()
	r := &route{app: app}

	notFound := func(ctx contracts.Context) error {
		return ctx.Response().NotFound("订单不存在")
	}
	handler := func(ctx contracts.Context) error {
		if err := notFound(ctx); err != nil {
			return err // 业务把“已响应”当 error 原样上抛（缺陷触发路径）
		}
		return ctx.Response().Success("ok") // 双写缺陷下会被执行的第二次写出
	}
	app.Get("/sentinel", r.wrap(handler))

	req := httptest.NewRequest(http.MethodGet, "/sentinel", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	body := string(bodyBytes)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("期望 404（第一次写出的状态码），实际 %d", resp.StatusCode)
	}
	if strings.Count(body, "{") != 1 {
		t.Errorf("响应体应只有一段 JSON（无二次写出拼接），实际 %q", body)
	}
	if !strings.Contains(body, "订单不存在") {
		t.Errorf("响应体应保留第一次写出的内容，实际 %q", body)
	}
	if strings.Contains(body, "ok") || strings.Contains(body, "Internal Server Error") {
		t.Errorf("不应出现第二次写出或 500 文本，实际 %q", body)
	}
}

// 哨兵被业务层 %w 包装后仍需被路由层识别（errors.Is 解包）。
func TestWrapRecognizesWrappedResponseSentinel(t *testing.T) {
	app := fiber.New()
	r := &route{app: app}

	handler := func(ctx contracts.Context) error {
		if err := ctx.Response().Unauthorized("token 过期"); err != nil {
			return fmt.Errorf("鉴权失败: %w", err)
		}
		return nil
	}
	app.Get("/wrapped-sentinel", r.wrap(handler))

	req := httptest.NewRequest(http.MethodGet, "/wrapped-sentinel", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("期望 401，实际 %d", resp.StatusCode)
	}
	if strings.Count(body, "{") != 1 || !strings.Contains(body, "token 过期") {
		t.Errorf("响应体应只有一段 401 JSON，实际 %q", body)
	}
	if strings.Contains(body, "Internal Server Error") {
		t.Errorf("被包装的哨兵不应触发 500，实际 %q", body)
	}
}

// 非哨兵的真实错误仍原样上抛，走 fiber 默认错误处理（500），行为保持现状。
func TestWrapPassesRealErrorToFiberErrorHandler(t *testing.T) {
	app := fiber.New()
	r := &route{app: app}

	app.Get("/boom", r.wrap(func(ctx contracts.Context) error {
		return errors.New("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("真实错误应交由 fiber 错误处理返回 500，实际 %d", resp.StatusCode)
	}
}
