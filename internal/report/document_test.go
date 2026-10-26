package report

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/zc310/canvas"
	fontfile "github.com/zc310/font"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// 表头须引用实际粗体字体面，不能只给常规字体附上 Bold 标志。
func TestReportUsesDefaultMaterialThemeAndActualBoldHeader(t *testing.T) {
	path := os.Getenv("OFD_REPORT_TEST_FONT")
	if path == "" {
		path = "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Skipf("本地中文字体不存在: %s", path)
	} else if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, path string }{{"explicit", path}, {"automatic", ""}} {
		t.Run(test.name, func(t *testing.T) {
			b, err := New(t.Context(), creator.Document{ID: "default-theme"}, test.path)
			if err != nil {
				t.Fatal(err)
			}
			if b.Document.Options.BoldFont == "" {
				family := fontFamily(b.fonts["Flow-Regular"].Face)
				candidate, _ := canvas.FindSystemFont(family, canvas.FontBold)
				_, err := loadFontFamilyFile(candidate, family)
				t.Logf("常规字体族=%q 系统粗体候选=%q 加载错误=%v", family, candidate, err)
				t.Skip("本地没有可匹配的同族中文粗体")
			}
			if !reflect.DeepEqual(b.Document.Options.Theme, flow.MaterialTheme()) {
				t.Fatal("报告覆盖了默认 Material 主题")
			}
			b.Heading(1, "报告标题", nil)
			b.Paragraph("正文段落", nil)
			b.Table([]string{"表头"}, [][]flow.TableCell{{{Text: "单元格"}}}, []float64{Width})
			doc, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}
			bold := false
			for _, resource := range doc.Fonts {
				if resource.Name != "Flow-Bold" {
					continue
				}
				face, err := fontfile.ParseSFNT(resource.Data, 0)
				if err != nil {
					t.Fatal(err)
				}
				if !resource.Bold || face.OS2 == nil || face.OS2.UsWeightClass < 600 {
					t.Fatal("粗体资源没有使用实际粗体字体面")
				}
				bold = true
				t.Logf("报告粗体: %s（%s），字重 %d", resource.FontName, resource.FamilyName, face.OS2.UsWeightClass)
			}
			if !bold {
				t.Fatal("输出模型丢失粗体资源")
			}
			seen := map[string]bool{}
			for _, page := range doc.Pages {
				for _, item := range page.Items {
					text, ok := item.(creator.Text)
					if !ok {
						continue
					}
					value := ""
					for _, code := range text.TextCodes {
						value += code.Value
					}
					switch value {
					case "报告标题", "表头":
						if text.Font != "Flow-Bold" || text.Weight != 700 {
							t.Fatalf("标题或表头没有选择粗体: %+v", text)
						}
						seen[value] = true
					case "正文段落", "单元格":
						if text.Font != "Flow-Regular" || text.Weight != 400 {
							t.Fatalf("正文或数据单元格被误设为粗体: %+v", text)
						}
						seen[value] = true
					}
				}
			}
			if len(seen) != 4 {
				t.Fatalf("未检查全部标题、表头和正文: %v", seen)
			}
		})
	}
}
