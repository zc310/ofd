package htmlimport_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/browser"
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/htmlimport"
	"github.com/zc310/ofd/pkg/validator"
)

func TestHTMLImporterRegistered(t *testing.T) {
	for _, name := range []string{"mhtml", "html"} {
		if _, ok := converter.ImporterByName(name); !ok {
			t.Fatalf("未注册 HTML 导入器: %s", name)
		}
	}
	if imp, ok := converter.ImporterByExtension(".mht"); !ok || imp.Name() != "mhtml" {
		t.Fatalf("按扩展名查找 mhtml 导入器失败: %v %v", imp, ok)
	}
	if imp, ok := converter.ImporterByExtension(".htm"); !ok || imp.Name() != "html" {
		t.Fatalf("按扩展名查找 html 导入器失败: %v %v", imp, ok)
	}
}

func TestHTMLTransformerRegistered(t *testing.T) {
	for _, name := range []string{"html", "mhtml"} {
		if _, ok := converter.TransformerFor(name, "pdf"); !ok {
			t.Fatalf("未注册 %s→pdf 直接转换器", name)
		}
	}
}

func TestFindChromeReportsMissingPath(t *testing.T) {
	if _, err := browser.FindChrome(filepath.Join(t.TempDir(), "no-such-chrome")); err == nil {
		t.Fatal("指定的不存在的 chrome 路径应报错")
	}
}

func TestPaperByName(t *testing.T) {
	cases := map[string][2]float64{
		"A4":     {210, 297},
		"A3":     {297, 420},
		"A5":     {148, 210},
		"Letter": {215.9, 279.4},
		"Legal":  {215.9, 355.6},
		"B5":     {176, 250},
		"16开":    {185, 260},
	}
	for name, want := range cases {
		paper, err := converter.PaperByName(name)
		if err != nil {
			t.Fatalf("PaperByName(%q) 失败: %v", name, err)
		}
		if paper.Width != want[0] || paper.Height != want[1] {
			t.Fatalf("PaperByName(%q) = %v, 期望 %v", name, paper, want)
		}
	}
	if _, err := converter.PaperByName("NoSuchPaper"); err == nil {
		t.Fatal("未知纸张名应报错")
	}
}

func TestConvertHTMLToPDFAndOFD(t *testing.T) {
	if chrome, err := browser.FindChrome(""); err != nil || chrome == "" {
		t.Skip("未安装 Chrome/Chromium，跳过")
	}
	html := []byte(`<html><head><style>body{background:#eef}</style></head>
<body><h1>HTML 转换测试</h1><p>Hello HTML</p>
<img src="https://example.com/remote.png" width="100" height="50"></body></html>`)
	var pdf bytes.Buffer
	if err := converter.Convert("html", "pdf", html, &pdf, converter.WithChromeNoSandbox(true)); err != nil {
		t.Fatalf("html→pdf 失败: %v", err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("html→pdf 输出不是 PDF")
	}
	var ofd bytes.Buffer
	if err := converter.Convert("html", "ofd", html, &ofd, converter.WithChromeNoSandbox(true)); err != nil {
		t.Fatalf("html→ofd 失败: %v", err)
	}
	if !bytes.HasPrefix(ofd.Bytes(), []byte("PK")) {
		t.Fatalf("html→ofd 输出不是 OFD")
	}
	validatorInstance, err := validator.New()
	if err != nil {
		t.Fatalf("创建校验器失败: %v", err)
	}
	report := validatorInstance.ValidateReader(context.Background(), bytes.NewReader(ofd.Bytes()), "converted.ofd")
	if report.Summary.Errors != 0 {
		t.Fatalf("OFD 校验存在错误: %d", report.Summary.Errors)
	}
}

func TestConvertMHTMLToPDF(t *testing.T) {
	if chrome, err := browser.FindChrome(""); err != nil || chrome == "" {
		t.Skip("未安装 Chrome/Chromium，跳过")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mht")
	if err := os.WriteFile(path, []byte(mhtmlSample), 0600); err != nil {
		t.Fatal(err)
	}
	var pdf bytes.Buffer
	if err := converter.Convert("mhtml", "pdf", path, &pdf, converter.WithChromeNoSandbox(true)); err != nil {
		t.Fatalf("mhtml→pdf 失败: %v", err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("mhtml→pdf 输出不是 PDF")
	}
}

const mhtmlSample = "From: <Saved by Blink>\n" +
	"MIME-Version: 1.0\n" +
	"Content-Type: multipart/related; boundary=\"----=_NextPart_01\"\n\n" +
	"------=_NextPart_01\n" +
	"Content-Type: text/html\n" +
	"Content-Transfer-Encoding: quoted-printable\n\n" +
	"<html><body><h1>MHTML =E6=B5=8B=E8=AF=95</h1></body></html>\n" +
	"------=_NextPart_01--\n"
