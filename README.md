# gofast-fiber

GoFast 框架的 [Fiber](https://gofiber.io) HTTP 引擎驱动插件。

框架核心 `go-fast-framework` 不再内置 HTTP 引擎，需显式安装并注册本插件（或 `gofast-gin`）。

## 安装

```bash
go get github.com/zhoudm1743/gofast-fiber@latest
```

## 接入

```go
import (
    gohttp "github.com/zhoudm1743/go-fast-framework/http"
    "github.com/zhoudm1743/go-fast-framework/foundation"
    gofastfiber "github.com/zhoudm1743/gofast-fiber"
)

app.SetProviders([]foundation.ServiceProvider{
    // ...
    &gohttp.ServiceProvider{},
    &gofastfiber.ServiceProvider{},
})
```

```yaml
server:
  driver: fiber   # 框架配置默认值
```

## 依赖

- `github.com/zhoudm1743/go-fast-framework` >= v0.9.0
- `github.com/gofiber/fiber/v2`

## License

Apache-2.0
