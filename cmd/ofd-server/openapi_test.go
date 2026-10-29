package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/zc310/ofd/internal/server/convertersvc"
	"github.com/zc310/ofd/internal/server/notify"
	"github.com/zc310/ofd/internal/server/runner"
	"github.com/zc310/ofd/internal/version"
)

const contractURL = "https://ofd.example/openapi.json"

type apiContract struct {
	doc      map[string]any
	compiler *jsonschema.Compiler
}

func loadAPIContract(t *testing.T) *apiContract {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(openAPIJSON(), &doc); err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	if err := c.AddResource(contractURL, doc); err != nil {
		t.Fatal(err)
	}
	return &apiContract{doc: doc, compiler: c}
}

func (c *apiContract) at(t *testing.T, pointer string) map[string]any {
	t.Helper()
	var value any = c.doc
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch node := value.(type) {
		case map[string]any:
			value = node[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				t.Fatalf("规范指针无效: %s", pointer)
			}
			value = node[i]
		default:
			t.Fatalf("规范指针不存在: %s", pointer)
		}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("规范指针不是对象: %s", pointer)
	}
	return obj
}

func pointerKey(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func (c *apiContract) validate(t *testing.T, pointer string, value any) {
	t.Helper()
	sch, err := c.compiler.Compile(contractURL + "#" + pointer)
	if err != nil {
		t.Fatalf("编译 %s: %v", pointer, err)
	}
	if err := sch.Validate(value); err != nil {
		t.Fatalf("响应不符合 %s: %v", pointer, err)
	}
}

func (c *apiContract) response(t *testing.T, path, method string, expected int, got response) {
	t.Helper()
	if got.status != expected {
		t.Fatalf("%s %s = %d，期望 %d: %s", method, path, got.status, expected, got.body)
	}
	if got.header(requestIDHeader) == "" {
		t.Fatal("缺少 X-Request-Id")
	}
	pointer := "/paths/" + pointerKey(path) + "/" + strings.ToLower(method) + "/responses/" + strconv.Itoa(expected)
	resp := c.at(t, pointer)
	if ref, ok := resp["$ref"].(string); ok {
		pointer = strings.TrimPrefix(ref, "#")
		resp = c.at(t, pointer)
	}
	if method == http.MethodHead || expected == http.StatusNoContent || expected == http.StatusNotModified {
		if got.body != "" {
			t.Fatal("此响应不应有响应体")
		}
		return
	}
	media, _, err := mime.ParseMediaType(got.contentType)
	if err != nil {
		t.Fatal(err)
	}
	content, ok := resp["content"].(map[string]any)
	if !ok {
		t.Fatalf("响应 %s 未声明 content", pointer)
	}
	if _, ok := content[media]; !ok {
		media = "*/*"
		if _, ok := content[media]; !ok {
			t.Fatalf("%s 未声明媒体类型 %s", pointer, got.contentType)
		}
	}
	if strings.HasPrefix(got.contentType, "application/json") {
		c.validate(t, pointer+"/content/"+pointerKey(media)+"/schema", got.decode(t))
	}
}

// 既检查引用存在，也编译内联 schema，避免只验证 components 而漏掉响应中的 schema。
func TestOpenAPIReferencesSchemasAndExamples(t *testing.T) {
	c := loadAPIContract(t)
	if c.doc["openapi"] != "3.1.0" || c.at(t, "/info")["version"] != version.Version {
		t.Fatal("规范版本或发行版本不正确")
	}
	var walk func(any, string)
	walk = func(value any, pointer string) {
		switch node := value.(type) {
		case map[string]any:
			if ref, ok := node["$ref"].(string); ok {
				if !strings.HasPrefix(ref, "#/") {
					t.Fatalf("规范不应依赖外部引用: %s", ref)
				}
				c.at(t, strings.TrimPrefix(ref, "#"))
			}
			if _, ok := node["schema"]; ok {
				if _, err := c.compiler.Compile(contractURL + "#" + pointer + "/schema"); err != nil {
					t.Fatalf("编译 %s/schema: %v", pointer, err)
				}
				if examples, ok := node["examples"].(map[string]any); ok {
					for name, example := range examples {
						t.Run(name, func(t *testing.T) {
							c.validate(t, pointer+"/schema", example.(map[string]any)["value"])
						})
					}
				}
			}
			for key, child := range node {
				walk(child, pointer+"/"+pointerKey(key))
			}
		case []any:
			for i, child := range node {
				walk(child, pointer+"/"+strconv.Itoa(i))
			}
		}
	}
	walk(c.doc, "")
	for name := range c.at(t, "/components/schemas") {
		if _, err := c.compiler.Compile(contractURL + "#/components/schemas/" + name); err != nil {
			t.Fatalf("编译 %s: %v", name, err)
		}
	}
}

// 保护两种认证的 OR 关系，以及已在浏览器验证过的 multipart 文本字段编码。
func TestOpenAPIAuthenticationAndMultipartContract(t *testing.T) {
	c := loadAPIContract(t)
	security := c.doc["security"].([]any)
	if len(security) != 2 {
		t.Fatal("应提供两种可独立使用的认证方式")
	}
	for i, name := range []string{"BearerAuth", "APIKeyAuth"} {
		requirement := security[i].(map[string]any)
		if _, ok := requirement[name]; !ok || len(requirement) != 1 {
			t.Fatal("Bearer 和 API Key 必须是 OR 关系")
		}
	}
	for path, item := range c.at(t, "/paths") {
		for method, value := range item.(map[string]any) {
			if method == "parameters" {
				continue
			}
			operation := value.(map[string]any)
			local, overrides := operation["security"]
			if path == "/healthz" || path == "/readyz" {
				if !overrides || len(local.([]any)) != 0 {
					t.Errorf("%s 应声明免认证", path)
				}
			} else if overrides {
				t.Errorf("%s %s 应使用全局业务认证", method, path)
			}
		}
	}
	media := c.at(t, "/paths/~1v1~1convert/post/requestBody/content/multipart~1form-data")
	if _, encoded := media["encoding"]; encoded {
		t.Fatal("multipart 的显式编码会使 Swagger UI 发送带 filename 的 Blob，服务端无法按文本字段读取")
	}
	if c.at(t, "/paths/~1v1~1convert/post/requestBody/content/multipart~1form-data/schema/properties/request")["type"] != "string" {
		t.Fatal("multipart request 必须是 JSON 文本表单字段")
	}
}

// 从真实路由声明提取方法，新增路由却漏写规范、或规范声明不存在的方法时失败。
func TestOpenAPICoversRegisteredRoutes(t *testing.T) {
	c := loadAPIContract(t)
	f, err := parser.ParseFile(token.NewFileSet(), "server.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]map[string]bool{}
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			t.Fatal("路由不是固定字符串，需要更新契约检查")
		}
		path, _ := strconv.Unquote(literal.Value)
		if path == "/" || path == "/docs" || path == "/docs/" || path == "/openapi.json" {
			return true
		}
		methods, ok := call.Args[1].(*ast.CallExpr)
		if !ok {
			t.Fatalf("%s 没有方法声明", path)
		}
		routes[path] = map[string]bool{}
		for _, arg := range methods.Args[1:] {
			method := arg.(*ast.SelectorExpr).Sel.Name
			routes[path][strings.ToLower(strings.TrimPrefix(method, "Method"))] = true
		}
		return true
	})
	paths := c.at(t, "/paths")
	ids := map[string]bool{}
	for path, value := range paths {
		operations := value.(map[string]any)
		for method, operation := range operations {
			if method == "parameters" {
				continue
			}
			if !routes[path][method] {
				t.Errorf("规范声明未注册的路由: %s %s", method, path)
			}
			delete(routes[path], method)
			id := operation.(map[string]any)["operationId"].(string)
			if ids[id] || id == "" {
				t.Errorf("operationId 为空或重复: %s", id)
			}
			ids[id] = true
		}
	}
	for path, methods := range routes {
		for method := range methods {
			t.Errorf("路由缺少规范: %s %s", method, path)
		}
	}
}

// Swagger UI 会把 example/default 填入请求，范围或文件名示例不能改变默认调用。
func TestOpenAPIOptionalParametersDoNotChangeRequests(t *testing.T) {
	c := loadAPIContract(t)
	for _, pointer := range []string{
		"/paths/~1v1~1convert/post",
		"/paths/~1v1~1jobs~1{id}~1content/get",
	} {
		for _, value := range c.at(t, pointer)["parameters"].([]any) {
			param := value.(map[string]any)
			if param["name"] != "Range" && param["name"] != "input_file_name" {
				continue
			}
			for _, obj := range []map[string]any{param, param["schema"].(map[string]any)} {
				for _, key := range []string{"example", "examples", "default"} {
					if _, ok := obj[key]; ok {
						t.Errorf("%s 的示例应写入描述，不能通过 %s 自动填入请求", param["name"], key)
					}
				}
			}
		}
	}
}

func TestDocsRedirectPreservesProxyPrefix(t *testing.T) {
	s, _, _ := newTestServer(t)
	proxy := http.NewServeMux()
	proxy.Handle("/prefix/", http.StripPrefix("/prefix", s.Handler()))
	client := newPipeServer(t, proxy)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			got := doAuth(t, client, method, "/prefix/docs", "", "")
			if got.status != http.StatusOK || !strings.HasPrefix(got.contentType, "text/html") {
				t.Fatalf("反向代理前缀下的文档跳转失败: %d %s", got.status, got.contentType)
			}
		})
	}
	noRedirect := *client.Client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Get(client.baseURL + "/prefix/docs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != "docs/" {
		t.Fatalf("应使用相对跳转: %d Location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestOpenAPIDocsArePublicAndOffline(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	for path, media := range map[string]string{
		"/openapi.json":              "application/json",
		"/docs/":                     "text/html",
		"/docs/ofd-initializer.js":   "text/javascript",
		"/docs/swagger-ui-bundle.js": "text/javascript",
		"/docs/swagger-ui.css":       "text/css",
		"/docs/favicon-32x32.png":    "image/png",
	} {
		t.Run(path, func(t *testing.T) {
			get := doAuth(t, client, http.MethodGet, path, "", "")
			if get.status != http.StatusOK || len(get.body) == 0 {
				t.Fatalf("公开文档不可用: %d", get.status)
			}
			if !strings.HasPrefix(get.contentType, media) || get.header("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("文档响应头不正确: %v", get.headers)
			}
			head := doAuth(t, client, http.MethodHead, path, "", "")
			if head.status != http.StatusOK || head.body != "" {
				t.Fatalf("HEAD = %d %q", head.status, head.body)
			}
			post := doAuth(t, client, http.MethodPost, path, "", "")
			if post.status != http.StatusMethodNotAllowed || post.header("Allow") != "GET, HEAD" {
				t.Fatalf("POST = %d, Allow=%q", post.status, post.header("Allow"))
			}
		})
	}
	if got := doAuth(t, client, http.MethodGet, "/docs", "", ""); got.status != http.StatusOK {
		t.Fatal("/docs 应自动跳转到 /docs/")
	}
	for _, path := range []string{"/docs/missing", "/docs/index.html", "/docs/swagger-initializer.js"} {
		if got := doAuth(t, client, http.MethodGet, path, "", ""); got.status != http.StatusNotFound {
			t.Errorf("不应提供 %s: %d", path, got.status)
		}
	}
	if got := doAuth(t, client, http.MethodGet, "/v1/formats", "", ""); got.status != http.StatusUnauthorized {
		t.Fatal("业务接口仍须认证")
	}
	if strings.Contains(docsHTML, "https://") || !strings.Contains(docsInitializer, "validatorUrl: null") {
		t.Fatal("文档页面不应依赖外部资源或外部规范校验服务")
	}
}

func TestOpenAPIResponsesMatchContract(t *testing.T) {
	c := loadAPIContract(t)
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	for _, path := range []string{"/healthz", "/readyz", "/v1/formats", "/v1/jobs", "/v1/stats", "/metrics"} {
		c.response(t, path, http.MethodGet, http.StatusOK, do(t, client, http.MethodGet, path, ""))
	}
	c.response(t, "/v1/formats", http.MethodGet, 401, doAuth(t, client, http.MethodGet, "/v1/formats", "", ""))
	c.response(t, "/v1/convert", http.MethodPost, 400, do(t, client, http.MethodPost, "/v1/convert", `{}`))
	c.response(t, "/v1/jobs/{id}", http.MethodGet, 404, do(t, client, http.MethodGet, "/v1/jobs/missing", ""))
	s.SetReadyFunc(func() bool { return false })
	c.response(t, "/readyz", http.MethodGet, 503, do(t, client, http.MethodGet, "/readyz", ""))

	// 假转换器隔离渲染依赖，保护实际 HTTP 解码、状态记录和文件返回的契约。
	s.inlineRun = func(context.Context, convertersvc.Spec) (convertersvc.Result, error) {
		return convertersvc.Result{Kind: "stream", Format: "pdf", MIME: "application/pdf",
			FileName: "output.pdf", Bytes: []byte("%PDF-contract"), InputFormat: "ofd", InputBytes: 4}, nil
	}
	body := `{"input":{"bytes":"UEsDBA=="},"output":{"format":"pdf"}}`
	sync := do(t, client, http.MethodPost, "/v1/convert", body)
	c.response(t, "/v1/convert", http.MethodPost, 200, sync)
	if sync.jobID == "" || sync.body != "%PDF-contract" {
		t.Fatal("同步返回缺少文件或任务 ID")
	}
	c.response(t, "/v1/jobs/{id}", http.MethodGet, 200, do(t, client, http.MethodGet, "/v1/jobs/"+sync.jobID, ""))

	var multipartBody bytes.Buffer
	mw := multipart.NewWriter(&multipartBody)
	if err := mw.WriteField("request", `{"output":{"kind":"dir","format":"pdf"}}`); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "sample.ofd")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("PK\x03\x04"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	for _, upload := range []struct {
		name, path, contentType string
		body                    io.Reader
	}{
		{"json", "/v1/convert", "application/json", strings.NewReader(strings.Replace(body, `"format":"pdf"`, `"kind":"dir","format":"pdf"`, 1))},
		{"multipart", "/v1/convert", mw.FormDataContentType(), &multipartBody},
		{"raw", "/v1/convert?to=pdf&kind=dir&input_file_name=sample.ofd", "application/octet-stream", strings.NewReader("PK\x03\x04")},
	} {
		t.Run(upload.name, func(t *testing.T) {
			got := doRequest(t, client, http.MethodPost, upload.path, upload.body, map[string]string{"Content-Type": upload.contentType})
			c.response(t, "/v1/convert", http.MethodPost, 202, got)
			id := got.decode(t)["id"].(string)
			c.response(t, "/v1/jobs/{id}", http.MethodGet, 200, do(t, client, http.MethodGet, "/v1/jobs/"+id, ""))
			c.response(t, "/v1/jobs/{id}/content", http.MethodGet, 409, do(t, client, http.MethodGet, "/v1/jobs/"+id+"/content", ""))
			c.response(t, "/v1/jobs/{id}/cancel", http.MethodPost, 200, do(t, client, http.MethodPost, "/v1/jobs/"+id+"/cancel", ""))
			c.response(t, "/v1/jobs/{id}", http.MethodGet, 200, do(t, client, http.MethodGet, "/v1/jobs/"+id, ""))
			c.response(t, "/v1/jobs/{id}", http.MethodDelete, 204, do(t, client, http.MethodDelete, "/v1/jobs/"+id, ""))
		})
	}
	dir := filepath.Join(cfg.OutputDir, "contract")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	seedFinished(t, store, "contract", dir, map[string]string{"output.pdf": "%PDF-contract"})
	path := "/v1/jobs/contract/content"
	c.response(t, "/v1/jobs/{id}/content", http.MethodGet, 200, do(t, client, http.MethodGet, path, ""))
	c.response(t, "/v1/jobs/{id}/content", http.MethodHead, 200, do(t, client, http.MethodHead, path, ""))
	c.response(t, "/v1/jobs/{id}/content", http.MethodGet, 206, doRequest(t, client, http.MethodGet, path, nil, map[string]string{"Range": "bytes=0-3"}))
	c.response(t, "/v1/jobs/{id}/content", http.MethodGet, 416, doRequest(t, client, http.MethodGet, path, nil, map[string]string{"Range": "bytes=9999-"}))
	c.response(t, "/v1/jobs", http.MethodGet, 200, do(t, client, http.MethodGet, "/v1/jobs", ""))
	c.response(t, "/v1/stats", http.MethodGet, 200, do(t, client, http.MethodGet, "/v1/stats", ""))
}

type contractConverter func(context.Context, convertersvc.Spec) (convertersvc.Result, error)

func (fn contractConverter) Run(ctx context.Context, spec convertersvc.Spec) (convertersvc.Result, error) {
	return fn(ctx, spec)
}

func TestOpenAPIWebhookMatchesDeliveredNotification(t *testing.T) {
	for _, event := range []string{"succeeded", "failed"} {
		t.Run(event, func(t *testing.T) {
			c := loadAPIContract(t)
			s, store, _ := newTestServer(t)
			type delivery struct {
				header http.Header
				body   []byte
			}
			received := make(chan delivery, 1)
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				received <- delivery{r.Header.Clone(), body}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer receiver.Close()
			registry := notify.NewRegistry(map[string]notify.Target{"erp": {URL: receiver.URL, Secret: "contract-secret"}})
			convert := contractConverter(func(context.Context, convertersvc.Spec) (convertersvc.Result, error) {
				if event == "failed" {
					return convertersvc.Result{}, fmt.Errorf("%w: 测试失败通知", convertersvc.ErrBadRequest)
				}
				return convertersvc.Result{Kind: "dir", Format: "pdf", InputFormat: "ofd", InputBytes: 4}, nil
			})
			worker := runner.New(store, convert, notify.NewDeliverer(registry, nil), runner.Config{
				FastWorkers: 1, HeavyWorkers: 1, NotifyWorkers: 1, PollInterval: time.Millisecond,
			}, testLogger())
			if err := worker.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer worker.Stop()
			client := newPipeServer(t, s.Handler())
			body := `{"input":{"bytes":"UEsDBA=="},"output":{"kind":"dir","format":"pdf"},"notify":{"target":"erp"}}`
			c.response(t, "/v1/convert", http.MethodPost, 202, do(t, client, http.MethodPost, "/v1/convert", body))
			select {
			case got := <-received:
				var payload map[string]any
				if err := json.Unmarshal(got.body, &payload); err != nil {
					t.Fatal(err)
				}
				c.validate(t, "/components/schemas/JobNotification", payload)
				for i, param := range c.at(t, "/webhooks/jobFinished/post")["parameters"].([]any) {
					name := param.(map[string]any)["name"].(string)
					var value any = got.header.Get(name)
					if name == notify.HeaderTimestamp {
						n, err := strconv.ParseInt(value.(string), 10, 64)
						if err != nil {
							t.Fatal(err)
						}
						value = n
					}
					c.validate(t, fmt.Sprintf("/webhooks/jobFinished/post/parameters/%d/schema", i), value)
				}
				if payload["event"] != event || got.header.Get(notify.HeaderEvent) != event {
					t.Fatal("通知事件不一致")
				}
				seconds, _ := strconv.ParseInt(got.header.Get(notify.HeaderTimestamp), 10, 64)
				if !notify.Verify("contract-secret", got.header.Get(notify.HeaderSignature), got.body, time.Unix(seconds, 0)) {
					t.Fatal("通知签名不正确")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("未收到任务通知")
			}
		})
	}
}
