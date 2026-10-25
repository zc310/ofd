package flow_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/sfnt"
)

func textValue(text creator.Text) string {
	value := text.Value
	for _, code := range text.TextCodes {
		value += code.Value
	}
	return value
}

func pageText(page creator.Page) string {
	var result strings.Builder
	for _, item := range page.Items {
		if text, ok := item.(creator.Text); ok {
			result.WriteString(textValue(text))
			result.WriteByte('\n')
		}
	}
	return result.String()
}

func smallOptions() flow.Options {
	opts := flow.DefaultOptions()
	opts.PageSize = creator.PageSize{Width: 100, Height: 90}
	opts.MarginTop, opts.MarginBottom, opts.MarginLeft, opts.MarginRight = 10, 10, 10, 10
	return opts
}

// 测试真实标准 OFD 写出与确定性，覆盖分页表格、混合字体、链接、图片图注和页码。
func TestReportProducesStrictStandardOFD(t *testing.T) {
	opts := smallOptions()
	opts.PageNumbers = true
	d := flow.New(opts)
	d.Metadata.ID = "report"
	d.Metadata.Title = "Report"
	d.AddHeading(1, "Report")
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Regular "}, {Text: "bold", Bold: true}, {Text: " and "}, {Text: "link", Href: "https://example.com"}}})
	d.AddList(true, []string{"A long item that wraps across several lines and retains a hanging indent.", "Second item."})
	rows := make([][]string, 15)
	for i := range rows {
		rows[i] = []string{fmt.Sprint(i + 1), "Measured table text"}
	}
	d.AddTable(flow.Table{Header: []string{"No.", "Description"}, Rows: rows, ColumnWidths: []float64{15, 65}})
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	img.Set(0, 0, color.White)
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	d.AddImage(flow.Image{Data: data.Bytes(), Width: 40, Caption: "Figure 1. Sample image"})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 3 {
		t.Fatalf("页数过少: %d", len(model.Pages))
	}
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				if text.X < opts.MarginLeft-0.001 || text.X+text.Width > opts.PageSize.Width-opts.MarginRight+0.001 {
					t.Fatalf("文字水平越界: %+v", text)
				}
				if text.Y < opts.MarginTop-0.001 || text.Y+text.Height > opts.PageSize.Height+0.001 {
					t.Fatalf("文字垂直越界: %+v", text)
				}
				for _, code := range text.TextCodes {
					if len([]rune(code.Value)) > 1 && len(code.DeltaX) != len([]rune(code.Value))-1 {
						t.Fatalf("缺少精确字符位置: %+v", code)
					}
				}
			}
		}
	}
	var first, second bytes.Buffer
	options := creator.CreateOptions{Deterministic: true}
	if err := d.Write(context.Background(), &first, options); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(context.Background(), &second, options); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("相同输入未产生确定性输出")
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "flow.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("严格校验失败: %+v", report.Issues)
	}
}

func TestPaginationKeepsHeadingAndAvoidsTrailingBlankPage(t *testing.T) {
	opts := smallOptions()
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("line\n", 7))
	d.AddHeading(2, "Heading")
	d.AddParagraph("First line\nSecond line\nThird line")
	d.AddPageBreak()
	d.AddPageBreak()
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 {
		t.Fatalf("页数 = %d，期望 2", len(model.Pages))
	}
	if strings.Contains(pageText(model.Pages[0]), "Heading") {
		t.Fatal("标题被留在前页尾部")
	}
	if !strings.Contains(pageText(model.Pages[1]), "Heading\nFirst line\nSecond line") {
		t.Fatalf("标题与正文未同页: %s", pageText(model.Pages[1]))
	}
}

func TestWidowsAndOrphansSplitParagraphIntoTwoAndTwo(t *testing.T) {
	opts := smallOptions()
	opts.PageSize.Height = 60
	opts.Theme.Body.SpaceAfter = 0
	d := flow.New(opts)
	d.AddParagraph("prefix\nprefix\nprefix")
	d.AddParagraph("one\ntwo\nthree\nfour")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 {
		t.Fatalf("页数 = %d", len(model.Pages))
	}
	if !strings.Contains(pageText(model.Pages[0]), "one\ntwo\n") || !strings.Contains(pageText(model.Pages[1]), "three\nfour\n") {
		t.Fatalf("未按 2+2 分页: %q / %q", pageText(model.Pages[0]), pageText(model.Pages[1]))
	}
}

func TestTablesRepeatHeadersAndFollowingTextDoesNotOverlap(t *testing.T) {
	opts := smallOptions()
	d := flow.New(opts)
	rows := make([][]string, 18)
	for i := range rows {
		rows[i] = []string{fmt.Sprintf("row %d", i)}
	}
	d.AddTable(flow.Table{Header: []string{"Header"}, Rows: rows})
	d.AddParagraph("Following paragraph")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, page := range model.Pages {
		text := pageText(page)
		if strings.Contains(text, "row ") {
			seen++
			if strings.Count(text, "Header") != 1 {
				t.Fatalf("续页表头错误: %q", text)
			}
		}
		lastBottom := 0.0
		for _, item := range page.Items {
			if p, ok := item.(creator.Path); ok {
				lastBottom = math.Max(lastBottom, p.Y+p.Height)
			}
			if text, ok := item.(creator.Text); ok && textValue(text) == "Following paragraph" && text.Y < lastBottom-0.001 {
				t.Fatal("表格后正文与表格重叠")
			}
		}
	}
	if seen < 2 {
		t.Fatal("表格没有跨页")
	}
}

func TestHeadersAndFootersResolveFinalPageCount(t *testing.T) {
	opts := smallOptions()
	opts.Header = "Report {page}"
	opts.Footer = "Internal"
	opts.PageNumbers = true
	d := flow.New(opts)
	d.AddParagraph("First page")
	d.AddPageBreak()
	d.AddParagraph("Second page")
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i, page := range model.Pages {
		text := pageText(page)
		if !strings.Contains(text, fmt.Sprintf("Report %d", i+1)) || !strings.Contains(text, fmt.Sprintf("Internal %d / 2", i+1)) {
			t.Fatalf("页眉页脚占位符错误: %q", text)
		}
		for _, item := range page.Items {
			if text, ok := item.(creator.Text); ok {
				value := textValue(text)
				if strings.HasPrefix(value, "Report ") && text.Y+text.Height > opts.MarginTop {
					t.Fatal("页眉侵入正文")
				}
				if strings.HasPrefix(value, "Internal ") && (text.Y < opts.PageSize.Height-opts.MarginBottom || text.Y+text.Height > opts.PageSize.Height) {
					t.Fatal("页脚超出页边距")
				}
			}
		}
	}
}

func TestFlowRejectsInvalidContentAndPreservesCancellation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*flow.Document)
		want   string
	}{
		{"missing glyph", func(d *flow.Document) { d.AddParagraph("中文") }, "缺少字符"},
		{"unknown font", func(d *flow.Document) { d.AddParagraph("text", flow.TextStyle{Font: "unknown"}) }, "未注册"},
		{"oversize word", func(d *flow.Document) { d.AddParagraph(strings.Repeat("W", 100)) }, "超过行宽"},
		{"invalid margins", func(d *flow.Document) { d.Options.MarginLeft = 1000 }, "版心"},
		{"invalid style", func(d *flow.Document) { d.AddParagraph("text", flow.TextStyle{Size: math.NaN()}) }, "有限"},
		{"mixed text", func(d *flow.Document) { d.Blocks = []flow.Block{{Text: "one", Runs: []flow.Run{{Text: "two"}}}} }, "不能同时"},
		{"bad image", func(d *flow.Document) { d.AddImage(flow.Image{Data: []byte("bad")}) }, "图片无法解析"},
		{"bad utf8", func(d *flow.Document) { d.AddParagraph(string([]byte{0xff})) }, "UTF-8"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := flow.New()
			test.change(d)
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("错误 = %v，期望包含 %q", err, test.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := flow.New().Build(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误丢失: %v", err)
	}
}

func fixtureCJKFont(t *testing.T) ([]byte, rune) {
	t.Helper()
	archive, err := zip.OpenReader("../../../testdata/ofdrw/intro.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 testdata/ofdrw/intro.ofd")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var data []byte
	var character rune
	for _, file := range archive.File {
		if !strings.HasSuffix(file.Name, ".ttf") {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := io.ReadAll(r)
		closeErr := r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		repaired, repairErr := fontrepair.Repair(candidate)
		if repairErr != nil {
			continue
		}
		face, err := sfnt.Parse(repaired)
		if err != nil {
			continue
		}
		var buf sfnt.Buffer
		for ch := rune(0x4e00); ch <= 0x9fff; ch++ {
			g, err := face.GlyphIndex(&buf, ch)
			if err == nil && g != 0 {
				data, character = repaired, ch
				break
			}
		}
		if len(data) > 0 {
			break
		}
	}
	if len(data) == 0 {
		t.Fatal("fixture 中没有可用的中文字体")
	}
	return data, character
}

func TestFontFallbackEmbedsActualCJKFace(t *testing.T) {
	data, character := fixtureCJKFont(t)
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "CJK", Data: data})
	opts.FallbackFonts = []string{"CJK"}
	d := flow.New(opts)
	d.AddParagraph("Latin " + string(character))
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range model.Pages[0].Items {
		if text, ok := item.(creator.Text); ok && strings.Contains(textValue(text), string(character)) {
			found = true
			if text.Font != "CJK" {
				t.Fatalf("实际字体 = %q", text.Font)
			}
		}
	}
	if !found {
		t.Fatal("回退文字丢失")
	}
	var out bytes.Buffer
	if err := d.Write(context.Background(), &out, creator.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(out.Bytes()), "cjk.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("中文字体写出校验失败: %+v", report.Issues)
	}
}
