package fiber

import (
	"bytes"
	"errors"
	"reflect"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/zhoudm1743/go-fast-framework/contracts"
	"github.com/zhoudm1743/go-fast-framework/filesystem"
	"github.com/zhoudm1743/go-fast-framework/http/base"
)

// Context 实现 contracts.Context，内部包装 *fiber.Ctx。
// 应用代码只见 contracts.Context，不感知 Fiber。
type Context struct {
	c          *fiber.Ctx
	store      map[string]any
	validator  contracts.Validation
	storage    contracts.Storage
	viewEngine contracts.ViewEngine
}

// NewContext 创建 Fiber 上下文包装器。
func NewContext(c *fiber.Ctx, v contracts.Validation, s contracts.Storage, ve contracts.ViewEngine) *Context {
	return &Context{c: c, store: make(map[string]any), validator: v, storage: s, viewEngine: ve}
}

// ── 请求读取 ────────────────────────────────────────────────────────

func (ctx *Context) Method() string { return ctx.c.Method() }
func (ctx *Context) Path() string   { return ctx.c.Path() }

func (ctx *Context) Param(key string) string { return ctx.c.Params(key) }

func (ctx *Context) Query(key string, defaultValue ...string) string {
	val := ctx.c.Query(key)
	if val == "" && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return val
}

func (ctx *Context) QueryInt(key string, defaultValue ...int) int {
	return base.QueryInt(ctx.c.Query(key), defaultValue...)
}

func (ctx *Context) QueryInt64(key string, defaultValue ...int64) int64 {
	return base.QueryInt64(ctx.c.Query(key), defaultValue...)
}

func (ctx *Context) QueryFloat64(key string, defaultValue ...float64) float64 {
	return base.QueryFloat64(ctx.c.Query(key), defaultValue...)
}

func (ctx *Context) QueryBool(key string, defaultValue ...bool) bool {
	return base.QueryBool(ctx.c.Query(key), defaultValue...)
}

func (ctx *Context) Header(key string) string { return ctx.c.Get(key) }
func (ctx *Context) IP() string               { return ctx.c.IP() }
func (ctx *Context) BodyRaw() []byte          { return ctx.c.Body() }

func (ctx *Context) FormValue(key string) string { return ctx.c.FormValue(key) }
func (ctx *Context) ContentType() string         { return ctx.c.Get(fiber.HeaderContentType) }
func (ctx *Context) UserAgent() string           { return ctx.c.Get(fiber.HeaderUserAgent) }
func (ctx *Context) FullPath() string            { return ctx.c.Route().Path }

// Bind 将请求数据填充到 obj（URI → Query → Body），最后统一验证。
func (ctx *Context) Bind(obj any) error {
	// 1. URI 路径参数
	bindURI(obj, ctx.c.Params)

	// 2. Query String
	if err := ctx.c.QueryParser(obj); err != nil {
		return err
	}

	// 3. 请求体
	if len(ctx.c.Body()) > 0 {
		if err := ctx.c.BodyParser(obj); err != nil {
			return err
		}
	}

	// 4. 应用 default 标签默认值
	base.ApplyDefaults(obj)

	// 5. 验证
	if ctx.validator != nil {
		return ctx.validator.Validate(obj)
	}
	return nil
}

// ── 文件与存储 ────────────────────────────────────────────────────────

func (ctx *Context) Storage() contracts.Storage { return ctx.storage }

func (ctx *Context) File(key string) (contracts.File, error) {
	header, err := ctx.c.FormFile(key)
	if err != nil {
		return nil, err
	}
	return filesystem.NewUploadedFile(header, ctx.storage), nil
}

// Files 返回 multipart 表单中指定 key 的所有上传文件，兼容单文件和多文件。
func (ctx *Context) Files(key string) ([]contracts.File, error) {
	form, err := ctx.c.MultipartForm()
	if err != nil {
		return nil, err
	}
	headers := form.File[key]
	files := make([]contracts.File, len(headers))
	for i, h := range headers {
		files[i] = filesystem.NewUploadedFile(h, ctx.storage)
	}
	return files, nil
}

func (ctx *Context) SendFile(path string) error { return ctx.c.SendFile(path) }

// ── 响应发送 ────────────────────────────────────────────────────────

func (ctx *Context) JSON(code int, obj any) error {
	return ctx.c.Status(code).JSON(obj)
}

func (ctx *Context) String(code int, s string) error {
	return ctx.c.Status(code).SendString(s)
}

// Redirect 发送 HTTP 重定向响应。
func (ctx *Context) Redirect(code int, location string) error {
	return ctx.c.Redirect(location, code)
}

// Write 写入原始字节到响应体。
func (ctx *Context) Write(data []byte) error {
	_, err := ctx.c.Write(data)
	return err
}

// HTML 渲染 HTML 模板并发送响应。
// name 为相对于模板目录的路径，例如 "home/index.html"。
func (ctx *Context) HTML(code int, name string, data any) error {
	if ctx.viewEngine == nil {
		return errors.New("view: no view engine configured, please register view.ServiceProvider")
	}
	var buf bytes.Buffer
	if err := ctx.viewEngine.Render(&buf, name, data); err != nil {
		return err
	}
	ctx.c.Status(code).Set(fiber.HeaderContentType, "text/html; charset=utf-8")
	return ctx.c.Send(buf.Bytes())
}

func (ctx *Context) Response() contracts.Response {
	return base.NewResponse(ctx)
}

func (ctx *Context) Status(code int) contracts.Context {
	ctx.c.Status(code)
	return ctx
}

func (ctx *Context) SetHeader(key, value string) contracts.Context {
	ctx.c.Set(key, value)
	return ctx
}

// ── 上下文存储 ──────────────────────────────────────────────────────

func (ctx *Context) Value(key string) any {
	return ctx.c.Locals(key)
}

func (ctx *Context) WithValue(key string, value any) contracts.Context {
	ctx.c.Locals(key, value)
	return ctx
}

// ── Middleware 控制 ─────────────────────────────────────────────────

func (ctx *Context) Next() error {
	return ctx.c.Next()
}

func (ctx *Context) Abort() error {
	return nil
}

func (ctx *Context) AbortWithCode(code int) error {
	return ctx.c.SendStatus(code)
}

func (ctx *Context) AbortWithJson(code int, obj any) error {
	return ctx.c.Status(code).JSON(obj)
}

// Underlying 返回底层 *fiber.Ctx，用于 SSE 等高级场景。
func (ctx *Context) Underlying() any {
	return ctx.c
}

// ── Cookie ──────────────────────────────────────────────────────────

func (ctx *Context) Cookie(name string) string {
	return ctx.c.Cookies(name)
}

func (ctx *Context) SetCookie(name, value string, opts contracts.CookieOptions) {
	path := opts.Path
	if path == "" {
		path = "/"
	}
	c := &fiber.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		Domain:   opts.Domain,
		MaxAge:   opts.MaxAge,
		Secure:   opts.Secure,
		HTTPOnly: opts.HTTPOnly,
		SameSite: opts.SameSite,
	}
	ctx.c.Cookie(c)
}

func (ctx *Context) ClearCookie(name string) {
	ctx.SetCookie(name, "", contracts.CookieOptions{MaxAge: -1, Path: "/", HTTPOnly: true})
}

// ── 内部：URI 路径参数绑定 ──────────────────────────────────────────

func bindURI(obj any, params func(key string, defaultValue ...string) string) {
	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("uri")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		val := params(name)
		if val == "" {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		base.SetFieldFromString(fv, val)
	}
}
