package report

import (
	"reflect"
	"strings"
	"testing"

	reporting "github.com/zc310/ofd/internal/report"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

func TestValidationReportCalloutReflectsStatusAndWarnings(t *testing.T) {
	font := testFont(t)
	for _, test := range []struct {
		name     string
		status   validator.Status
		warnings int
		color    creator.Color
	}{
		{"通过", validator.StatusValid, 0, reporting.Green},
		{"通过但有警告", validator.StatusValid, 1, reporting.Orange},
		{"检查不完整", validator.StatusPartial, 0, reporting.Orange},
		{"校验不通过", validator.StatusInvalid, 0, reporting.Red},
		{"执行错误", validator.StatusError, 0, reporting.Red},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := sampleReport()
			r.Status, r.Summary.Warnings = test.status, test.warnings
			doc, err := Build(t.Context(), r, Options{Font: font})
			if err != nil {
				t.Fatal(err)
			}
			text := modelText(doc)
			for _, want := range []string{"校验状态：" + test.status.Label(), "未执行项不代表通过", "文件 / XML 路径", "引用的页面文件不存在"} {
				if !strings.Contains(text, want) {
					t.Fatalf("报告缺少 %q", want)
				}
			}
			icon, stripe := false, false
			for _, item := range doc.Pages[0].Items {
				path, ok := item.(creator.Path)
				if !ok || !reflect.DeepEqual(path.FillColor, &test.color) {
					continue
				}
				icon = icon || path.CTM != nil
				stripe = stripe || (path.CTM == nil && path.Width <= 1 && path.Height > 5)
			}
			if !icon || !stripe {
				t.Fatalf("状态提示框缺少语义图标或左色条: icon=%v stripe=%v", icon, stripe)
			}
			if len(doc.Outlines) != 1 || len(doc.Outlines[0].Children) != 2 {
				t.Fatal("提示框改变了章节大纲")
			}
		})
	}
}
