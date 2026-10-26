package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// 为报告标签构造可度量的映射字体，布局回归不依赖机器安装中文字体。
func issueTableFont(t *testing.T) string {
	t.Helper()
	labels := "中文OFD 校验报告输入文件检测时间未知校验规范校验状态无效有效部分通过错误警告提示耗时检查项结果问题明细条未发现问题序号级别阶段文件引用XML 路径问题说明信息代码引擎代码依据提示容器语义摘要签章验证已通过未通过有警告未完成已跳过未执行第页共（）：、"
	face, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	glyph, err := face.GlyphIndex(&buffer, 'H')
	if err != nil {
		t.Fatal(err)
	}
	mappings := []fontrepair.GlyphMapping{{Rune: 'H', Glyph: uint16(glyph)}}
	seen := map[rune]bool{}
	for _, r := range labels {
		if r > 127 && !seen[r] {
			seen[r] = true
			mappings = append(mappings, fontrepair.GlyphMapping{Rune: r, Glyph: uint16(glyph)})
		}
	}
	data, err := fontrepair.RepairWithGlyphs(goregular.TTF, mappings)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "report.ttf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func issueText(text creator.Text) string {
	value := text.Value
	for _, code := range text.TextCodes {
		value += code.Value
	}
	return value
}

func TestIssueTableKeepsFieldsColumnsAndSeverityColors(t *testing.T) {
	r := sampleReport()
	r.Input.Path = "input.ofd"
	r.Checks = nil
	r.Issues = []validator.Issue{
		{Severity: validator.SeverityError, Stage: validator.StageReference, File: "Doc_0/Document.xml", Line: 3, Column: 5, Path: "/Document/Pages", Message: "MISSING-PAGE", Code: "ref.missing", EngineCode: "REF001", Clause: []string{"Clause-A", "Clause-B"}, Hint: "CHECK-PATH"},
		{Severity: validator.SeverityWarning, Stage: validator.StageSemantic, Message: "REPAIRED-FONT"},
		{Severity: validator.SeverityInfo, Stage: validator.StageXML, File: "OFD.xml", Message: "INFO-ONLY"},
	}
	before, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Build(t.Context(), r, Options{Font: issueTableFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("排表修改了原始报告")
	}
	if len(doc.Pages) != 1 || len(doc.Outlines) != 1 || len(doc.Outlines[0].Children) != 2 {
		t.Fatal("短明细分页或章节结构错误")
	}
	text := strings.ReplaceAll(modelText(doc), "\n", "")
	for _, want := range []string{"序号", "级别 / 阶段", "文件 / XML 路径", "问题说明", "Doc_0/Document.xml:3:5", "/Document/Pages", "MISSING-PAGE", "ref.missing", "REF001", "Clause-A、Clause-B", "CHECK-PATH", "REPAIRED-FONT", "INFO-ONLY"} {
		if !strings.Contains(text, want) {
			t.Fatalf("表格遗漏字段 %q: %s", want, modelText(doc))
		}
	}
	if strings.Count(text, "代码：") != 2 || strings.Count(text, "XML 路径：") != 1 || strings.Count(text, "提示：") != 1 {
		t.Fatal("可选空字段产生了多余标签")
	}
	headerTop := 0.0
	for _, item := range doc.Pages[0].Items {
		if v, ok := item.(creator.Text); ok && issueText(v) == "问题说明" {
			headerTop = v.Y
		}
	}
	for _, item := range doc.Pages[0].Items {
		v, ok := item.(creator.Text)
		if !ok || math.Abs(v.Y-headerTop) > 0.001 {
			continue
		}
		switch issueText(v) {
		case "序号":
			if math.Abs(v.X+v.Width/2-24) > 0.001 {
				t.Fatal("第一列表头没有水平居中")
			}
		case "级别 / 阶段":
			if math.Abs(v.X+v.Width/2-42.5) > 0.001 {
				t.Fatal("第二列表头没有水平居中")
			}
		}
	}
	seen := map[string]bool{}
	for _, item := range doc.Pages[0].Items {
		v, ok := item.(creator.Text)
		if !ok || v.Y <= headerTop {
			continue
		}
		value := issueText(v)
		switch value {
		case "1", "2", "3":
			if math.Abs(v.X+v.Width/2-24) > 0.001 {
				t.Fatal("序号没有在第一列水平居中")
			}
			seen[value] = true
		case "错误", "警告", "提示":
			severity := map[string]validator.Severity{"错误": validator.SeverityError, "警告": validator.SeverityWarning, "提示": validator.SeverityInfo}[value]
			color := severityColor(severity)
			if math.Abs(v.X+v.Width/2-42.5) > 0.001 || v.FillColor == nil || v.FillColor.R != color.R || v.FillColor.G != color.G || v.FillColor.B != color.B {
				t.Fatalf("级别颜色或列位置错误: %+v", v)
			}
			seen[value] = true
		case "文件引用", "语义", "XML":
			if math.Abs(v.X+v.Width/2-42.5) > 0.001 {
				t.Fatal("阶段没有在第二列水平居中")
			}
		case "MISSING-PAGE", "REPAIRED-FONT", "INFO-ONLY":
			if v.X < 112 || v.X+v.Width > 192.001 {
				t.Fatal("问题说明没有放入宽列")
			}
			seen[value] = true
		}
	}
	if len(seen) != 9 {
		t.Fatalf("未找到全部明细行: %v", seen)
	}
}

func TestIssueTableLongRowRepeatsHeaderAndPreservesEveryLine(t *testing.T) {
	r := sampleReport()
	r.Input.Path = "input.ofd"
	r.Checks = nil
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("DETAIL-%03d", i)
	}
	r.Issues = []validator.Issue{
		{Severity: validator.SeverityError, Stage: validator.StageXSD, File: "Doc_0/Page.xml", Path: "/Page/Content", Message: strings.Join(lines, "\n"), Code: "LONG001", Hint: "LONG-END"},
		{Severity: validator.SeverityWarning, Stage: validator.StageSemantic, Message: "FOLLOWING-ISSUE"},
	}
	opts := Options{Font: issueTableFont(t)}
	doc, err := Build(t.Context(), r, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) < 3 {
		t.Fatal("超高单条诊断没有跨页续排")
	}
	counts := map[string]int{}
	sequences := map[string]int{}
	for p, page := range doc.Pages {
		foundHeader := false
		for _, item := range page.Items {
			if v, ok := item.(creator.Text); ok {
				value := issueText(v)
				counts[value]++
				if value == "问题说明" {
					foundHeader = true
				}
				if foundHeader && v.X >= 18 && v.X < 30 && (value == "1" || value == "2") {
					sequences[value]++
				}
				if strings.HasPrefix(value, "DETAIL-") && (v.X < 112 || v.X+v.Width > 192.001 || v.Y < 18 || v.Y+v.Height > 279.001) {
					t.Fatalf("长明细越出表格版心: %+v", v)
				}
			}
			if v, ok := item.(creator.Path); ok && (v.Y < 17.999 || v.Y+v.Height > 279.001) {
				t.Fatalf("表格边框越界: %+v", v)
			}
		}
		if !foundHeader {
			t.Fatalf("第 %d 页丢失重复表头", p+1)
		}
	}
	for _, value := range append(lines, "FOLLOWING-ISSUE") {
		if counts[value] != 1 {
			t.Fatalf("明细丢失或重复 %q: %d", value, counts[value])
		}
	}
	if strings.Count(strings.ReplaceAll(modelText(doc), "\n", ""), "LONG-END") != 1 {
		t.Fatal("跨页明细遗漏了最后的提示")
	}
	if sequences["1"] != 1 || sequences["2"] != 1 {
		t.Fatalf("续排重复了逻辑问题序号: %v", sequences)
	}
	var output bytes.Buffer
	if err := RenderOFD(t.Context(), &output, r, opts); err != nil {
		t.Fatal(err)
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	checked := v.ValidateReader(t.Context(), bytes.NewReader(output.Bytes()), "issues.ofd")
	if checked.HasErrors() || checked.HasWarnings() {
		t.Fatalf("明细表格严格校验失败: %+v", checked.Issues)
	}
}
