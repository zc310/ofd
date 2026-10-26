package report

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/parser"
	reporting "github.com/zc310/ofd/internal/report"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

func testFont(t *testing.T) string {
	t.Helper()
	paths := []string{os.Getenv("OFD_REPORT_TEST_FONT")}
	if paths[0] == "" {
		paths = []string{"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"}
	}
	for _, path := range paths {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Skipf("本地中文测试字体不存在（%q），可设置 OFD_REPORT_TEST_FONT", paths)
	return ""
}

func sampleReport() validator.Report {
	return validator.Report{Input: validator.InputInfo{Path: "中文文档.ofd"}, Status: validator.StatusInvalid,
		StartedAt: time.Date(2026, 10, 26, 10, 20, 30, 0, time.UTC), DurationMS: 123,
		Summary: validator.Summary{Errors: 1, Warnings: 1, Files: 12},
		Checks:  []validator.CheckResult{{Name: "zip", Status: "passed"}, {Name: "xsd", Status: "failed"}, {Name: "semantic", Status: "warning"}},
		Issues: []validator.Issue{{Severity: validator.SeverityError, Stage: validator.StageReference, Code: "reference.missing",
			File: "Doc_0/Document.xml", Line: 3, Column: 5, Path: "/Document/Pages/Page",
			Message: "引用的页面文件不存在", Hint: "请检查页面路径", EngineCode: "REF001", Clause: []string{"GB/T 33190—2016 §7.5"}}},
	}
}

func modelText(doc *creator.Document) string {
	var out strings.Builder
	for _, page := range doc.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				for _, code := range text.TextCodes {
					out.WriteString(code.Value)
				}
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}

// 多页长诊断须完整输出，表格重复表头，大纲及页脚使用最终页码。
func TestReportFlowPaginatesLongDiagnosticsAndWritesStrictOFD(t *testing.T) {
	opts := Options{Font: testFont(t)}
	r := sampleReport()
	r.Input.Path = strings.Repeat("超长目录/", 50) + strings.Repeat("W", 300) + ".ofd"
	r.Issues[0].Message += "\x00\uffff\U0010ffff"
	r.Issues[0].Path = "/" + strings.Repeat("Element", 100)
	for i := 0; i < 40; i++ {
		r.Checks = append(r.Checks, validator.CheckResult{Name: "semantic", Status: "passed"})
	}
	for i := 0; i < 10; i++ {
		issue := r.Issues[0]
		issue.Message = fmt.Sprintf("诊断编号%02d：%s", i, strings.Repeat("长诊断需要自动分页。", 80))
		issue.Hint = fmt.Sprintf("诊断结束%02d", i)
		r.Issues = append(r.Issues, issue)
	}
	doc, err := Build(t.Context(), r, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) < 3 || len(doc.Outlines) != 1 || len(doc.Outlines[0].Children) != 2 {
		t.Fatalf("缺少分页或章节大纲: pages=%d outlines=%+v", len(doc.Pages), doc.Outlines)
	}
	text := modelText(doc)
	for _, want := range []string{"OFD 校验报告", "2026-10-26 10:20:30", "REF001", "请检查页面路径", `\u0000`, `\uFFFF`, `\U0010FFFF`, "诊断结束09"} {
		if !strings.Contains(text, want) {
			t.Fatalf("报告遗漏文字 %q", want)
		}
	}
	if strings.Count(text, "检查项") < 2 {
		t.Fatal("跨页检查表未重复表头")
	}
	for i, page := range doc.Pages {
		if !strings.Contains(modelText(&creator.Document{Pages: []creator.Page{page}}), fmt.Sprintf("第 %d 页 / 共 %d 页", i+1, len(doc.Pages))) {
			t.Fatalf("第 %d 页缺少最终页码", i+1)
		}
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok && (text.X < 17.99 || text.X+text.Width > 192.01 || text.Y < 0 || text.Y+text.Height > 297) {
				t.Fatalf("文字越界: %+v", text)
			}
		}
	}
	var output bytes.Buffer
	if err := RenderOFD(t.Context(), &output, r, opts); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	checked := v.ValidateReader(t.Context(), bytes.NewReader(output.Bytes()), "report.ofd")
	if checked.HasErrors() || checked.HasWarnings() {
		t.Fatalf("报告严格校验失败: %+v", checked.Issues)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].PageCount() != len(doc.Pages) {
		t.Fatal("写出改变了报告页数")
	}
	for _, font := range ofd.Documents[0].Fonts() {
		if font.FontFile == "" {
			t.Fatalf("报告字体未嵌入: %+v", font)
		}
		embedded, err := ofd.Documents[0].FileCache.Read(font.FontFile.String())
		if err != nil {
			t.Fatal(err)
		}
		face, err := sfnt.Parse(embedded)
		if err != nil {
			t.Fatal(err)
		}
		original, err := reporting.LoadFont(opts.Font)
		if err != nil {
			t.Fatal(err)
		}
		if face.NumGlyphs() >= original.Face.NumGlyphs() || len(embedded) >= len(original.Data) {
			t.Fatalf("报告嵌入了完整字体: glyphs=%d/%d bytes=%d/%d", face.NumGlyphs(), original.Face.NumGlyphs(), len(embedded), len(original.Data))
		}
	}
}

func TestReportPDFKeepsOFDPaginationAndSearchableChinese(t *testing.T) {
	opts := Options{Font: testFont(t)}
	r := sampleReport()
	r.Issues[0].Message = strings.Repeat("跨页内容保持一致。\n", 80)
	doc, err := Build(t.Context(), r, opts)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderPDF(t.Context(), &output, r, opts); err != nil {
		t.Fatal(err)
	}
	pdf, err := api.ReadAndValidate(t.Context(), bytes.NewReader(output.Bytes()), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if pdf.PageCount != len(doc.Pages) || pdf.PageCount < 2 {
		t.Fatalf("OFD/PDF 页数不一致: ofd=%d pdf=%d", len(doc.Pages), pdf.PageCount)
	}
	if tool, err := exec.LookPath("pdftotext"); err == nil {
		path := filepath.Join(t.TempDir(), "report.pdf")
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		text, err := exec.CommandContext(t.Context(), tool, "-enc", "UTF-8", path, "-").CombinedOutput()
		if err != nil {
			t.Fatalf("提取 PDF 文字: %v %s", err, text)
		}
		for _, want := range []string{"OFD 校验报告", "中文文档.ofd", "跨页内容保持一致。", "请检查页面路径"} {
			if !strings.Contains(string(text), want) {
				t.Fatalf("PDF 文字层缺少 %q: %s", want, text)
			}
		}
		if !strings.Contains(strings.Join(strings.Fields(string(text)), ""), "第2页/共") {
			t.Fatal("PDF 缺少第二页页码")
		}
	} else {
		t.Log("未安装 pdftotext，省略外部文字提取检查")
	}
}

func TestReportNoIssuesAndPartialStatus(t *testing.T) {
	opts := Options{Font: testFont(t)}
	r := sampleReport()
	r.Status, r.Issues = validator.StatusPartial, nil
	doc, err := Build(t.Context(), r, opts)
	if err != nil {
		t.Fatal(err)
	}
	text := modelText(doc)
	if !strings.Contains(text, "部分通过") || !strings.Contains(text, "未发现问题。") {
		t.Fatalf("报告缺少状态或空结果: %s", text)
	}
}

// 文字提取无法发现 CID 指向错误：同时比较 OFD 与 PDF 的实际字形画面。
func TestReportPDFRasterMatchesOFDChineseGlyphs(t *testing.T) {
	opts := Options{Font: testFont(t)}
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("未安装 pdftoppm，跳过 PDF 栅格比较")
	}
	r := sampleReport()
	var ofd, pdf, raster bytes.Buffer
	if err := RenderOFD(t.Context(), &ofd, r, opts); err != nil {
		t.Fatal(err)
	}
	if err := RenderPDF(t.Context(), &pdf, r, opts); err != nil {
		t.Fatal(err)
	}
	if err := converter.Convert(t.Context(), "ofd", "png", ofd.Bytes(), &raster, converter.Page(1), converter.DPI(96), converter.BgColor(color.White)); err != nil {
		t.Fatal(err)
	}
	source, err := png.Decode(bytes.NewReader(raster.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path, base := filepath.Join(root, "report.pdf"), filepath.Join(root, "page")
	if err := os.WriteFile(path, pdf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), tool, "-f", "1", "-l", "1", "-singlefile", "-r", "96", "-png", path, base).CombinedOutput(); err != nil {
		t.Fatalf("PDF 栅格化: %v %s", err, output)
	}
	data, err := os.ReadFile(base + ".png")
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	dark := func(img image.Image, x, y int) bool {
		return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y < 150
	}
	ink, bad := 0, 0
	for y := 1; y < min(source.Bounds().Dy(), got.Bounds().Dy())-1; y++ {
		for x := 1; x < min(source.Bounds().Dx(), got.Bounds().Dx())-1; x++ {
			a, b := dark(source, x, y), dark(got, x, y)
			if !a && !b {
				continue
			}
			ink++
			matched := a == b
			for yy := y - 1; yy <= y+1 && !matched; yy++ {
				for xx := x - 1; xx <= x+1 && !matched; xx++ {
					matched = a && dark(got, xx, yy) || b && dark(source, xx, yy)
				}
			}
			if !matched {
				bad++
			}
		}
	}
	if ink < 100 || bad > ink/50 {
		t.Fatalf("OFD/PDF 字形画面不一致: mismatches=%d ink=%d", bad, ink)
	}
}

func TestReportRejectsInvalidFontAndPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, render := range []func(context.Context, io.Writer, validator.Report, Options) error{RenderOFD, RenderPDF} {
		var output bytes.Buffer
		if err := render(ctx, &output, validator.Report{}, Options{}); !errors.Is(err, context.Canceled) || output.Len() != 0 {
			t.Fatalf("取消未传递或产生输出: err=%v bytes=%d", err, output.Len())
		}
		if err := render(t.Context(), nil, validator.Report{}, Options{}); err == nil {
			t.Fatal("未拒绝空输出")
		}
	}
	if _, err := Build(nil, validator.Report{}, Options{}); err == nil {
		t.Fatal("未拒绝空上下文")
	}
	path := filepath.Join(t.TempDir(), "invalid.ttf")
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RenderOFD(t.Context(), &output, validator.Report{}, Options{Font: path}); err == nil || output.Len() != 0 {
		t.Fatalf("无效字体未报错或提前输出: err=%v bytes=%d", err, output.Len())
	}
}

// 即使没有中文系统字体，也能检查诊断转义及超长 ASCII、组合字符的安全换行。
func TestPrepareTextEscapesControlsAndWrapsWithoutSplittingGraphemes(t *testing.T) {
	face, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	// Go 字体缺少组合重音符，为其绑定一个现有字形以覆盖字素边界逻辑。
	var buffer sfnt.Buffer
	glyph, err := face.GlyphIndex(&buffer, '\'')
	if err != nil {
		t.Fatal(err)
	}
	data, err := fontrepair.RepairWithGlyphs(goregular.TTF, []fontrepair.GlyphMapping{{Rune: '\u0301', Glyph: uint16(glyph)}})
	if err != nil {
		t.Fatal(err)
	}
	face, err = sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	f := reporting.Font{Data: data, Face: face}
	input := strings.Repeat("W", 200) + "\n" + strings.Repeat("a\u0301", 100) + "\x00\uffff\U0010ffff"
	got, err := f.PrepareText(t.Context(), input, 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `\u0000`) || !strings.Contains(got, `\uFFFF`) || !strings.Contains(got, `\U0010FFFF`) {
		t.Fatalf("诊断未转义: %q", got)
	}
	if strings.Contains(got, "\n\u0301") || strings.ContainsRune(got, '\u200b') {
		t.Fatal("换行拆开了组合字符或插入不可见字符")
	}
	if !strings.HasPrefix(strings.ReplaceAll(got, "\n", ""), strings.Repeat("W", 200)+strings.Repeat("a\u0301", 100)) {
		t.Fatal("换行丢失或改写了诊断字符")
	}
}
