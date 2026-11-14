package analyzer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/crypt"
	reporting "github.com/zc310/ofd/internal/report"
	"github.com/zc310/ofd/pkg/creator"
)

func TestAnalysisReportCalloutDistinguishesExecutionFromFindings(t *testing.T) {
	font := testReportFont(t)
	for _, test := range []struct {
		name     string
		status   Status
		warnings []string
		errors   []string
		color    creator.Color
	}{
		{"完成仅为信息提示", StatusComplete, nil, nil, reporting.Blue},
		{"完成但有警告", StatusComplete, []string{"发现警告"}, nil, reporting.Orange},
		{"完成但有错误", StatusComplete, nil, []string{"发现错误"}, reporting.Red},
		{"分析不完整", StatusPartial, nil, nil, reporting.Orange},
		{"执行失败", StatusFailed, nil, nil, reporting.Red},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Report{Status: test.status, Warnings: test.warnings, Errors: test.errors}
			doc, err := BuildReport(t.Context(), r, DocumentOptions{Font: font})
			if err != nil {
				t.Fatal(err)
			}
			text := analysisModelText(doc)
			for _, want := range []string{"分析状态：" + reportStatusLabel(test.status), "完成仅表示分析执行完成，不代表文档无问题。"} {
				if !strings.Contains(text, want) {
					t.Fatalf("报告缺少 %q", want)
				}
			}
			found := false
			for _, item := range doc.Pages[0].Items {
				if path, ok := item.(creator.Path); ok && path.CTM != nil && reflect.DeepEqual(path.FillColor, &test.color) {
					found = true
				}
			}
			if !found {
				t.Fatal("状态提示框缺少对应颜色的语义图标")
			}
		})
	}
}

func TestAnalysisReportCalloutsPreserveEncryptedScopeAndStageTable(t *testing.T) {
	r := sampleAnalysisReport()
	r.Encryption = &crypt.Info{State: "locked"}
	doc, err := BuildReport(t.Context(), r, DocumentOptions{Font: testReportFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	text := analysisModelText(doc)
	for _, want := range []string{"检查范围受限", "统计零值不代表原文没有内容。", "执行状态说明", "未执行项不代表通过。", "检查项目", "用户关闭此阶段", "分析结论"} {
		if !strings.Contains(text, want) {
			t.Fatalf("提示框或原有表格缺少 %q", want)
		}
	}
	if strings.Count(text, "完成仅表示分析执行完成") != 1 {
		t.Fatal("状态提示框与结论摘要重复说明")
	}
}
