package flow

import (
	"strings"
	"testing"
)

// 固定度量字体并启用逻辑缺字估算，不依赖系统字体也能保护 CJK 闭标点禁则。
func TestParagraphWrappingAvoidsLeadingCJKClosingPunctuation(t *testing.T) {
	e := codeWrapEngine(t)
	e.state.opts.AllowLogicalFonts = true
	resource := e.state.fontResources["Mono"]
	resource.Data = nil
	e.state.fontResources["Mono"] = resource
	style := TextStyle{Font: "Mono", Size: 12, LineHeight: 1.5}
	const source = "一二三四五六七八九。十十一十二十三，十四十五十六！十七十八十九？二十（二十一）二十二。"
	lines := e.breakLines([]Run{{Text: source}}, style, ptToMM(style.Size)*4, 0)
	if e.state.err != nil {
		t.Fatal(e.state.err)
	}
	if len(lines) < 3 {
		t.Fatalf("测试没有触发足够的中文换行: %d", len(lines))
	}
	var restored strings.Builder
	for _, line := range lines {
		var text strings.Builder
		for _, item := range line.atoms {
			text.WriteString(item.text)
		}
		value := text.String()
		if value == "" {
			t.Fatal("自动换行产生了空行")
		}
		if strings.ContainsRune("。，！？）", []rune(value)[0]) {
			t.Fatalf("闭标点出现在行首: %q", value)
		}
		restored.WriteString(value)
	}
	if restored.String() != source {
		t.Fatalf("中文断行改写了内容: %q", restored.String())
	}
}
