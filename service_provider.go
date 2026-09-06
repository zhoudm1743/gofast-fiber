package fiber

import (
	"github.com/zhoudm1743/go-fast-framework/contracts"
	"github.com/zhoudm1743/go-fast-framework/foundation"
	gohttp "github.com/zhoudm1743/go-fast-framework/http"
)

// ServiceProvider Fiber HTTP 驱动接入点（可选驱动，需显式加入应用 providers）。
//
//	import gofastfiber "github.com/zhoudm1743/gofast-fiber"
//	app.SetProviders(append(providers, &gofastfiber.ServiceProvider{}))
//
// 配置 server.driver: fiber（框架默认值）
type ServiceProvider struct{}

func (sp *ServiceProvider) Register(app foundation.Application) {
	gohttp.RegisterDriver("fiber", func(
		cfg contracts.Config,
		validator contracts.Validation,
		storage contracts.Storage,
		log contracts.Log,
	) (contracts.Route, error) {
		return NewRoute(cfg, validator, storage, log)
	})
}

func (sp *ServiceProvider) Boot(app foundation.Application) error {
	return nil
}
