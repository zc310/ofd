package main

import (
	_ "embed"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/goccy/go-json"
	swaggerFiles "github.com/swaggo/files/v2"
	"go.yaml.in/yaml/v3"

	"github.com/zc310/ofd/internal/version"
)

//go:embed openapi.yaml
var openAPIYAML []byte

// 规范在首次读取时转换并缓存；发行版本沿用程序的唯一版本来源。
var openAPIJSON = sync.OnceValue(func() []byte {
	var doc map[string]any
	if err := yaml.Unmarshal(openAPIYAML, &doc); err != nil {
		panic(fmt.Errorf("解析内嵌 OpenAPI 规范: %w", err))
	}
	doc["info"].(map[string]any)["version"] = version.Version
	raw, err := json.Marshal(doc)
	if err != nil {
		panic(fmt.Errorf("编码 OpenAPI 规范: %w", err))
	}
	return raw
})

func handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	raw := openAPIJSON()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	setWriteDeadline(w, responseWriteTimeout)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(raw)
	}
}

func handleDocsRedirect(w http.ResponseWriter, _ *http.Request) {
	// 保留相对 Location，避免 ServeMux 或 http.Redirect 将其补成根路径，
	// 导致反向代理剥离的路径前缀在浏览器跟随跳转时丢失。
	w.Header().Set("Location", "docs/")
	setWriteDeadline(w, responseWriteTimeout)
	w.WriteHeader(http.StatusTemporaryRedirect)
}

// 文档只暴露页面使用的静态文件，不提供上游的示例页面或在线配置入口。
func docsHandler() http.HandlerFunc {
	assets := http.StripPrefix("/docs/", http.FileServer(http.FS(swaggerFiles.FS)))
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		setWriteDeadline(w, responseWriteTimeout)
		switch r.URL.Path {
		case "/docs/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(docsHTML))
			}
		case "/docs/ofd-initializer.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(docsInitializer))
			}
		case "/docs/swagger-ui.css", "/docs/swagger-ui-bundle.js", "/docs/favicon-32x32.png":
			assets.ServeHTTP(w, r)
		default:
			writeError(w, http.StatusNotFound, "not_found", "未知文档资源")
		}
	}
}

const docsHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>ofd-server API</title>
  <link rel="icon" type="image/png" href="./favicon-32x32.png">
  <link rel="stylesheet" href="./swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="./swagger-ui-bundle.js"></script>
  <script src="./ofd-initializer.js"></script>
</body>
</html>
`

const docsInitializer = `window.onload = function () {
  window.ui = SwaggerUIBundle({
    url: "../openapi.json",
    dom_id: "#swagger-ui",
    deepLinking: true,
    validatorUrl: null,
    queryConfigEnabled: false,
    persistAuthorization: false,
    supportedSubmitMethods: ["get", "head", "post", "delete"],
    presets: [SwaggerUIBundle.presets.apis],
    layout: "BaseLayout"
  });
};
`
