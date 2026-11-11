package session

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func (d *optionsDocument) Info() (reader.DocumentInfo, error)                         { return reader.DocumentInfo{}, nil }
func (d *optionsDocument) FontList() ([]reader.FontInfo, error)                       { return nil, nil }
func (d *optionsDocument) Stats() (reader.DocumentStats, error)                       { return reader.DocumentStats{}, nil }
func (d *optionsDocument) Attachments() ([]reader.AttachmentInfo, error)              { return nil, nil }
func (d *optionsDocument) Media() ([]reader.MediaInfo, error)                         { return nil, nil }
func (d *optionsDocument) MediaData(int, uint64, int64) ([]byte, error)               { return nil, nil }
func (d *optionsDocument) CheckDocumentPermission(reader.PermissionAction, int) error { return nil }
func (d *optionsDocument) AttachmentData(int, string, int64) ([]byte, error)          { return nil, nil }
func (d *optionsDocument) AnnotationsForPage(int) ([]reader.AnnotationInfo, error)    { return nil, nil }
func (d *optionsDocument) PageLinksForPage(int) ([]reader.PageLink, error)            { return nil, nil }
func (d *optionsDocument) Signatures() ([]reader.SignatureInfo, error)                { return nil, nil }
func (d *optionsDocument) SignatureValue(int, string) ([]byte, error)                 { return nil, nil }
func (d *optionsDocument) SignatureSeal(int, string, int) ([]byte, string, error) {
	return nil, "", nil
}
func (d *optionsDocument) SignatureCertificate(int, string, string) ([]byte, error) { return nil, nil }
func (d *optionsDocument) Layers(int) ([]reader.LayerInfo, error)                   { return nil, nil }
func (d *optionsDocument) SetHiddenLayers(int, []string) error                      { return nil }
func (d *optionsDocument) FontUsage(context.Context, reader.FontRef, reader.FontUsageOptions) (reader.FontUsage, error) {
	return reader.FontUsage{}, nil
}
func (d *optionsDocument) FontUsageAll(context.Context, reader.FontUsageOptions) (reader.FontUsageReport, error) {
	return reader.FontUsageReport{}, nil
}

func TestPageHandlerRendersBinaryAndRejectsInvalidRequests(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "handler-test", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	h, err := NewPageHandler(s)
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewPageHandler(s)
	if err != nil || h.Prefix() == other.Prefix() {
		t.Fatalf("会话路径应随机生成: %v", err)
	}
	for _, test := range []struct {
		name, method, path string
		status             int
		contentType        string
	}{
		{"PNG", "GET", h.Prefix() + "0?dpi=24", 200, "image/png"},
		{"JPG", "GET", h.Prefix() + "0?format=jpg&dpi=24", 200, "image/jpeg"},
		{"SVG", "GET", h.Prefix() + "0?format=svg&dpi=24", 200, "image/svg+xml"},
		{"其他会话", "GET", other.Prefix() + "0", 404, ""},
		{"POST", "POST", h.Prefix() + "0", 405, ""},
		{"负页码", "GET", h.Prefix() + "-1", 400, ""},
		{"非规范页码", "GET", h.Prefix() + "00", 400, ""},
		{"不存在的页", "GET", h.Prefix() + "5", 422, ""},
		{"非有限 DPI", "GET", h.Prefix() + "0?dpi=NaN", 400, ""},
		{"过高 DPI", "GET", h.Prefix() + "0?dpi=601", 400, ""},
		{"重复参数", "GET", h.Prefix() + "0?dpi=24&dpi=48", 400, ""},
		{"未知参数", "GET", h.Prefix() + "0?unknown=export", 400, ""},
		{"无效用途", "GET", h.Prefix() + "0?purpose=preview", 400, ""},
		{"无效颜色", "GET", h.Prefix() + "0?background=white", 400, ""},
		{"不支持 PDF", "GET", h.Prefix() + "0?format=pdf", 400, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("状态 = %d，期望 %d", response.Code, test.status)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("响应缺少安全头")
			}
			if test.contentType == "" {
				return
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("Content-Type = %s，期望 %s", got, test.contentType)
			}
			var decodeErr error
			switch test.contentType {
			case "image/png":
				_, decodeErr = png.Decode(bytes.NewReader(response.Body.Bytes()))
			case "image/jpeg":
				_, decodeErr = jpeg.Decode(bytes.NewReader(response.Body.Bytes()))
			default:
				var root struct {
					XMLName xml.Name `xml:"svg"` //nolint:tagliatelle // SVG 标准根元素为小写。
				}
				decodeErr = xml.Unmarshal(response.Body.Bytes(), &root)
				if decodeErr == nil && root.XMLName.Local != "svg" {
					t.Fatalf("SVG 根元素 = %v", root.XMLName)
				}
				if !strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox") {
					t.Fatal("SVG 缺少沙箱安全头")
				}
			}
			if decodeErr != nil {
				t.Fatalf("无效图片: %v", decodeErr)
			}
		})
	}
}

type optionsDocument struct {
	options reader.RenderOptions
}

func (d *optionsDocument) Pages() ([]reader.PageInfo, error)                    { return nil, nil }
func (d *optionsDocument) Page(int) (reader.PageInfo, error)                    { return reader.PageInfo{}, nil }
func (d *optionsDocument) Text(int) ([]reader.TextRun, error)                   { return nil, nil }
func (d *optionsDocument) Permissions() ([]reader.DocumentPermissions, error)   { return nil, nil }
func (d *optionsDocument) CheckPermission(reader.PermissionAction, []int) error { return nil }
func (d *optionsDocument) Outline() (reader.OutlineTree, error)                 { return reader.OutlineTree{}, nil }
func (d *optionsDocument) Preferences() (reader.ViewPreferences, error) {
	return reader.ViewPreferences{}, nil
}
func (d *optionsDocument) Search(context.Context, string, reader.SearchOptions) (reader.SearchReport, error) {
	return reader.SearchReport{}, nil
}
func (d *optionsDocument) Close() error { return nil }
func (d *optionsDocument) RenderPDF(ctx context.Context, output io.Writer, _ []int, options reader.RenderOptions) error {
	return d.RenderPage(ctx, output, 0, options)
}

func (d *optionsDocument) RenderPage(_ context.Context, _ io.Writer, _ int, options reader.RenderOptions) error {
	d.options = options
	return nil
}

func TestPageHandlerPassesPurposeAndBackgroundToReader(t *testing.T) {
	doc := &optionsDocument{}
	s := newSession(doc, 1)
	t.Cleanup(func() { _ = s.Close() })
	h, err := NewPageHandler(s)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		h.Prefix()+"0?dpi=144&format=jpg&background=%2311223380&purpose=export", nil)
	h.ServeHTTP(response, request)
	if response.Code != http.StatusOK || doc.options.Purpose != reader.PermissionExport ||
		doc.options.Background != (color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0x80}) ||
		doc.options.DPI != 144 || doc.options.Format != reader.RenderJPG {
		t.Fatalf("状态=%d 渲染参数=%+v", response.Code, doc.options)
	}
}

func TestPageHandlerSVGContainsRenderedPageContent(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "svg-page", Pages: []creator.Page{{Items: []creator.Item{
		creator.Path{X: 1, Y: 2, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	h, err := NewPageHandler(s)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, h.Prefix()+"0?format=svg", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<path") {
		t.Fatalf("SVG 未包含绘制路径，状态 = %d，输出 = %s", response.Code, response.Body.String())
	}
}

func TestPageHandlerSVGRealDocumentAndTextLayer(t *testing.T) {
	path := "../../testdata/ofdrw/intro.ofd"
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少测试文档 %s", path)
		}
		t.Fatal(err)
	}
	s, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	h, err := NewPageHandler(s)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := s.Pages()
	if err != nil {
		t.Fatal(err)
	}
	pageIndex := -1
	for index := range pages {
		runs, err := s.Text(index)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) > 0 {
			pageIndex = index
			break
		}
	}
	if pageIndex < 0 {
		t.Fatal("测试文档没有可提取的文字")
	}
	for _, format := range []string{"svg", "png"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("%s%d?format=%s&dpi=24", h.Prefix(), pageIndex, format), nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s 渲染状态 = %d", format, response.Code)
		}
		if format == "svg" {
			var root struct {
				XMLName xml.Name `xml:"svg"` //nolint:tagliatelle // SVG 标准根元素为小写。
			}
			if err := xml.Unmarshal(response.Body.Bytes(), &root); err != nil || root.XMLName.Local != "svg" || !strings.Contains(response.Body.String(), "<path") {
				t.Fatalf("SVG 输出无效或没有矢量内容: %v", err)
			}
		} else if _, err := png.Decode(bytes.NewReader(response.Body.Bytes())); err != nil {
			t.Fatalf("PNG 输出无效: %v", err)
		}
	}
}

func TestPageHandlerEnforcesExportAndPrintPermissions(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "restricted", Pages: []creator.Page{{}},
		Permissions: &creator.Permissions{Export: &denied, Print: &creator.PrintSettings{Printable: false}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	h, err := NewPageHandler(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"png", "svg"} {
		for _, test := range []struct {
			purpose string
			status  int
		}{{"", http.StatusOK}, {"&purpose=export", http.StatusUnprocessableEntity}, {"&purpose=print", http.StatusUnprocessableEntity}} {
			response := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, h.Prefix()+"0?dpi=24&format="+format+test.purpose, nil)
			h.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("格式 %q 用途 %q 状态 = %d，期望 %d", format, test.purpose, response.Code, test.status)
			}
		}
	}
}

func TestPageHandlerDoesNotReturnPartialDataAfterCancellation(t *testing.T) {
	for _, format := range []string{"png", "svg"} {
		t.Run(format, func(t *testing.T) {
			doc := &blockingDocument{started: make(chan struct{}, 1), canceled: make(chan struct{}), release: make(chan struct{})}
			s := newSession(doc, 1)
			h, err := NewPageHandler(s)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				h.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, h.Prefix()+"0?format="+format, nil))
				close(done)
			}()
			<-doc.started
			cancel()
			<-doc.canceled
			close(doc.release)
			<-done
			if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Header().Get("Content-Type"), "image/") {
				t.Fatalf("取消后响应状态或类型错误: %d %s", response.Code, response.Header().Get("Content-Type"))
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
