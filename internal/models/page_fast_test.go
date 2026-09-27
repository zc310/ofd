package models

import (
	"encoding/xml"
	"testing"
)

// TestParsePageContentXMLStripsCDATA 回归：TextCode 的 CDATA 正文必须取 lexer
// 的语义文本，不能把含 "<![CDATA[" / "]]>" 包装的原始词素写进值里，否则渲染
// 会把包装当正文显示。
func TestParsePageContentXMLStripsCDATA(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">
  <ofd:Content><ofd:Layer ID="1">
    <ofd:TextObject ID="2" Boundary="0 0 10 10" Font="1" Size="3">
      <ofd:TextCode X="0" Y="3"><![CDATA[<b>甲&乙</b>]]></ofd:TextCode>
    </ofd:TextObject>
  </ofd:Layer></ofd:Content>
</ofd:Page>`)

	pc, err := ParsePageContentXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Content == nil || len(pc.Content.Layer) == 0 || len(pc.Content.Layer[0].Items) == 0 {
		t.Fatal("未解析到页面对象")
	}
	item := pc.Content.Layer[0].Items[0]
	if item.Kind != PageItemText || item.Text == nil || len(item.Text.TextCode) == 0 {
		t.Fatalf("首个对象不是文字对象: %+v", item)
	}
	if got, want := item.Text.TextCode[0].Value, "<b>甲&乙</b>"; got != want {
		t.Fatalf("TextCode 值 = %q, want %q", got, want)
	}
}

// TestStBoxAllowsNegativeWidth 回归：右对齐文字会把 Boundary 写成负宽度并让
// TextCode 的 X 偏移到负值，解析不应因此报错。
func TestStBoxAllowsNegativeWidth(t *testing.T) {
	var box StBox
	if err := box.UnmarshalXMLAttr(xml.Attr{Value: "90 58 -3 5"}); err != nil {
		t.Fatalf("负宽度 Boundary 解析失败: %v", err)
	}
	if box.X != 90 || box.Y != 58 || box.Width != -3 || box.Height != 5 {
		t.Fatalf("解析结果 = %+v", box)
	}
}
