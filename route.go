package fiber

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/zhoudm1743/go-fast-framework/contracts"
	fastcors "github.com/zhoudm1743/go-fast-framework/http/cors"
)

// route 实现 contracts.Route，封装 Fiber。
type route struct {
	app        *fiber.App
	cfg        contracts.Config
	router     fiber.Router
	validator  contracts.Validation
	storage    contracts.Storage
	log        contracts.Log
	viewEngine contracts.ViewEngine
}

// NewRoute 创建基于 Fiber 的 HTTP 路由服务实例。
func NewRoute(cfg contracts.Config, validator contracts.Validation, storage contracts.Storage, log contracts.Log) (contracts.Route, error) {
	readTimeout := time.Duration(cfg.GetInt("server.read_timeout_sec", 30)) * time.Second
	writeTimeout := time.Duration(cfg.GetInt("server.write_timeout_sec", 30)) * time.Second
	idleTimeout := time.Duration(cfg.GetInt("server.idle_timeout_sec", 120)) * time.Second
	name := cfg.GetString("server.name", "GoFast")

	fiberCfg := fiber.Config{
		AppName:               name,
		ServerHeader:          name,
		DisableStartupMessage: true, // 由框架 log 统一输出启动信息
		ReadTimeout:           readTimeout,
		WriteTimeout:          writeTimeout,
		IdleTimeout:           idleTimeout,
		Prefork:               cfg.GetBool("server.prefork"),
	}
	if limit := cfg.GetInt("server.body_limit_mb"); limit > 0 {
		fiberCfg.BodyLimit = limit * 1024 * 1024
	}

	app := fiber.New(fiberCfg)

	// Recovery 中间件：使用框架 log 记录 panic
	app.Use(recoveryMiddleware(log))
	// Logger 中间件：使用框架 log 记录每次请求
	app.Use(loggerMiddleware(log))
	app.Use(requestid.New())

	// 基础安全响应头中间件（默认关闭，server.security_headers_enabled 开启）
	if cfg.GetBool("server.security_headers_enabled") {
		app.Use(securityHeadersMiddleware(fastcors.HSTSMaxAge(cfg)))
	}

	// CORS 中间件
	corsOpts := fastcors.Load(cfg)
	if corsOpts.Wildcard() && corsOpts.AllowCredentials {
		// fiber 官方 cors 中间件拒绝「放行所有源 + 凭据」组合（直接 panic），
		// 用自定义实现与 gin 驱动对齐：逐请求回显请求 Origin
		app.Use(wildcardCredentialsCORSMiddleware(corsOpts))
	} else {
		app.Use(cors.New(cors.Config{
			AllowOrigins:     strings.Join(corsOpts.AllowOrigins, ","),
			AllowMethods:     corsOpts.AllowMethods,
			AllowHeaders:     corsOpts.AllowHeaders,
			ExposeHeaders:    corsOpts.ExposeHeaders,
			AllowCredentials: corsOpts.AllowCredentials,
			MaxAge:           corsOpts.MaxAge,
		}))
	}

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	return &route{app: app, cfg: cfg, router: app, validator: validator, storage: storage, log: log}, nil
}

// SetViewEngine 将 HTML 模板引擎注入路由，后续每个请求上下文将持有该引擎引用。
func (r *route) SetViewEngine(ve contracts.ViewEngine) {
	r.viewEngine = ve
}

func (r *route) Run(addr ...string) error {
	address := fmt.Sprintf("%s:%d",
		r.cfg.GetString("server.host", "0.0.0.0"),
		r.cfg.GetInt("server.port", 3000))
	if len(addr) > 0 && addr[0] != "" {
		address = addr[0]
	}
	r.log.Infof("[GoFast/fiber] listening on %s", address)
	return r.app.Listen(address)
}

func (r *route) Shutdown() error {
	r.log.Info("[GoFast/fiber] graceful stop...")
	timeout := time.Duration(r.cfg.GetInt("server.shutdown_timeout_sec", 10)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.app.ShutdownWithContext(ctx)
}

func (r *route) Get(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Get(path, r.wrap(h))
	return r
}
func (r *route) Post(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Post(path, r.wrap(h))
	return r
}
func (r *route) Put(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Put(path, r.wrap(h))
	return r
}
func (r *route) Delete(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Delete(path, r.wrap(h))
	return r
}
func (r *route) Patch(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Patch(path, r.wrap(h))
	return r
}
func (r *route) Head(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Head(path, r.wrap(h))
	return r
}
func (r *route) Options(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Options(path, r.wrap(h))
	return r
}
func (r *route) Any(path string, h contracts.HandlerFunc) contracts.Route {
	r.router.All(path, r.wrap(h))
	return r
}
func (r *route) Match(methods []string, path string, h contracts.HandlerFunc) contracts.Route {
	for _, m := range methods {
		r.router.Add(m, path, r.wrap(h))
	}
	return r
}
func (r *route) Add(method string, path string, h contracts.HandlerFunc) contracts.Route {
	r.router.Add(method, path, r.wrap(h))
	return r
}

func (r *route) Group(prefix string, args ...any) contracts.Route {
	group := &route{
		app:        r.app,
		cfg:        r.cfg,
		router:     r.router.Group(prefix),
		validator:  r.validator,
		storage:    r.storage,
		log:        r.log,
		viewEngine: r.viewEngine,
	}

	var callback func(contracts.Route)

	for _, arg := range args {
		switch v := arg.(type) {
		case func(contracts.Context) error:
			group.Use(v)
		case contracts.HandlerFunc:
			group.Use(v)
		case func(contracts.Route):
			callback = v
		}
	}

	if callback != nil {
		callback(group)
	}

	return group
}

func (r *route) Use(middleware ...contracts.HandlerFunc) contracts.Route {
	for _, m := range middleware {
		r.router.Use(r.wrap(m))
	}
	return r
}

func (r *route) Register(controllers ...contracts.Controller) contracts.Route {
	for _, c := range controllers {
		var target contracts.Route = r

		if pc, ok := c.(contracts.Prefixer); ok {
			if p := pc.Prefix(); p != "" {
				target = target.Group(p)
			}
		}

		if mc, ok := c.(contracts.Middlewarer); ok {
			if m := mc.Middleware(); len(m) > 0 {
				target.Use(m...)
			}
		}

		c.Boot(target)
	}
	return r
}

// Static 从本地目录 dir 提供静态文件服务。
func (r *route) Static(urlPrefix, dir string) contracts.Route {
	r.app.Static(urlPrefix, dir)
	return r
}

// StaticFS 从任意 http.FileSystem 提供静态文件服务（支持 http.FS(embed.FS)）。
func (r *route) StaticFS(urlPrefix string, fs http.FileSystem) contracts.Route {
	r.app.Use(urlPrefix, filesystem.New(filesystem.Config{
		Root:   fs,
		Browse: false,
	}))
	return r
}

// SPA 挂载单页应用（全站兜底，必须最后注册）。
func (r *route) SPA(fsys fs.FS, root string) contracts.Route {
	sub := fsys
	if root != "" && root != "." {
		s, err := fs.Sub(fsys, root)
		if err != nil {
			r.log.Warnf("[GoFast/fiber] SPA disabled: fs.Sub(%q) failed: %v", root, err)
			return r
		}
		sub = s
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		r.log.Warnf("[GoFast/fiber] SPA disabled: index.html not found")
		return r
	}
	r.app.Use(filesystem.New(filesystem.Config{
		Root:         http.FS(sub),
		Index:        "index.html",
		NotFoundFile: "index.html",
		MaxAge:       3600,
	}))
	r.log.Info("[GoFast/fiber] SPA mounted on /")
	return r
}

// StaticSPA 在指定 URL 前缀挂载单页应用。
func (r *route) StaticSPA(prefix string, fsys fs.FS, root string) contracts.Route {
	sub := fsys
	if root != "" && root != "." {
		s, err := fs.Sub(fsys, root)
		if err != nil {
			r.log.Warnf("[GoFast/fiber] StaticSPA disabled: fs.Sub(%q) failed: %v", root, err)
			return r
		}
		sub = s
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		r.log.Warnf("[GoFast/fiber] StaticSPA disabled: index.html not found")
		return r
	}
	r.app.Use(prefix, filesystem.New(filesystem.Config{
		Root:         http.FS(sub),
		Index:        "index.html",
		NotFoundFile: "index.html",
		MaxAge:       3600,
	}))
	r.log.Infof("[GoFast/fiber] StaticSPA mounted on %s", prefix)
	return r
}

// wrap 将 contracts.HandlerFunc 转为 Fiber handler。
func (r *route) wrap(h contracts.HandlerFunc) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := h(NewContext(c, r.validator, r.storage, r.viewEngine))
		if err == nil {
			return nil
		}
		// 响应已写出（哨兵 ErrResponseSent）：必须以 nil 结束请求。
		// 若把哨兵原样交给 fiber，默认错误处理会对“已写出的响应”再追加一段
		// 500 文本，造成响应体拼接（响应双写缺陷的路由层根治点）。
		if errors.Is(err, contracts.ErrResponseSent) {
			return nil
		}
		// 其余错误原样上抛，走 fiber 错误处理路径（保持现状）。
		return err
	}
}

// Routes 返回所有已注册路由的基本信息（Method + Path）。
func (r *route) Routes() []contracts.RouteInfo {
	fiberRoutes := r.app.GetRoutes()
	seen := make(map[string]struct{}, len(fiberRoutes))
	var result []contracts.RouteInfo
	for _, fr := range fiberRoutes {
		key := fr.Method + ":" + fr.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, contracts.RouteInfo{
			Method: fr.Method,
			Path:   fr.Path,
		})
	}
	if result == nil {
		result = []contracts.RouteInfo{}
	}
	return result
}

// Route 返回指定路径的路由信息，不存在时返回零值 RouteInfo。
func (r *route) Route(path string) contracts.RouteInfo {
	for _, fr := range r.app.GetRoutes() {
		if fr.Path == path {
			return contracts.RouteInfo{
				Method: fr.Method,
				Path:   fr.Path,
			}
		}
	}
	return contracts.RouteInfo{}
}

// Underlying 返回底层 *fiber.App，用于需要访问 Fiber 原生能力的场景。
func (r *route) Underlying() any {
	return r.app
}

// ── 内置中间件 ──────────────────────────────────────────────────────

// loggerMiddleware 记录每个请求的方法、路径、状态码和耗时。
func loggerMiddleware(log contracts.Log) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		latency := time.Since(start)
		status := c.Response().StatusCode()
		method := c.Method()
		path := c.OriginalURL()
		clientIP := c.IP()

		entry := log.WithFields(map[string]any{
			"status":  status,
			"latency": latency.String(),
			"ip":      clientIP,
			"method":  method,
			"path":    path,
		})

		switch {
		case status >= http.StatusInternalServerError:
			entry.Error("[GoFast/fiber]")
		case status >= http.StatusBadRequest:
			entry.Warn("[GoFast/fiber]")
		default:
			entry.Info("[GoFast/fiber]")
		}

		return err
	}
}

// recoveryMiddleware 捕获 panic 并通过框架 log 记录堆栈，返回 500。
func recoveryMiddleware(log contracts.Log) fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.WithFields(map[string]any{
					"error": fmt.Sprintf("%v", r),
					"stack": string(stack),
				}).Error("[GoFast/fiber] panic recovered")
				err = c.SendStatus(http.StatusInternalServerError)
			}
		}()
		return c.Next()
	}
}

// securityHeadersMiddleware 基础安全响应头中间件（server.security_headers_enabled 开启）。
// HSTS 仅对 HTTPS 请求输出（HTTP 下无意义）。
func securityHeadersMiddleware(hstsMaxAge int) fiber.Handler {
	return func(c *fiber.Ctx) error {
		h := &c.Response().Header
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if hstsMaxAge > 0 && c.Secure() {
			h.Set("Strict-Transport-Security", fmt.Sprintf("max-age=%d", hstsMaxAge))
		}
		return c.Next()
	}
}

// wildcardCredentialsCORSMiddleware 处理「放行所有源 + 携带凭据」组合。
// fiber 官方 cors 中间件拒绝该组合（构造时直接 panic，CORS 规范禁止
// ACAO:* 搭配凭据），此处与 gin 驱动对齐：逐请求回显请求 Origin。
func wildcardCredentialsCORSMiddleware(opts fastcors.Options) fiber.Handler {
	return func(c *fiber.Ctx) error {
		origin := c.Get(fiber.HeaderOrigin)
		// 无 Origin 头或预检请求缺 Access-Control-Request-Method 时不在 CORS 范畴，
		// 不输出 CORS 头（避免缓存投毒），直接放行
		if origin == "" ||
			(c.Method() == fiber.MethodOptions && c.Get(fiber.HeaderAccessControlRequestMethod) == "") {
			return c.Next()
		}
		h := &c.Response().Header
		h.Set(fiber.HeaderAccessControlAllowOrigin, origin)
		h.Set(fiber.HeaderAccessControlAllowCredentials, "true")
		if c.Method() == fiber.MethodOptions {
			h.Set(fiber.HeaderAccessControlAllowMethods, opts.AllowMethods)
			h.Set(fiber.HeaderAccessControlAllowHeaders, opts.AllowHeaders)
			if opts.MaxAge > 0 {
				h.Set(fiber.HeaderAccessControlMaxAge, fmt.Sprintf("%d", opts.MaxAge))
			}
			return c.SendStatus(fiber.StatusNoContent)
		}
		if opts.ExposeHeaders != "" {
			h.Set(fiber.HeaderAccessControlExposeHeaders, opts.ExposeHeaders)
		}
		return c.Next()
	}
}
