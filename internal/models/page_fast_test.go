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

// TestParsePageContentXMLKeepsSelfClosedPageBlock 回归：AreaHolder 占位用的
// <PageBlock ID="10"/> 是自闭合元素，词法器必须返回一个非空 PageBlock，
// 否则按 Kind 遍历内容的渲染路径会解引用空指针。
func TestParsePageContentXMLKeepsSelfClosedPageBlock(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">
  <ofd:Content><ofd:Layer ID="2">
    <ofd:PageBlock ID="10"/>
    <ofd:PageBlock ID="21"><ofd:TextObject ID="22" Boundary="0 0 10 10" Font="1" Size="3"/></ofd:PageBlock>
  </ofd:Layer></ofd:Content>
</ofd:Page>`)

	pc, err := ParsePageContentXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Content == nil || len(pc.Content.Layer) != 1 {
		t.Fatalf("未解析到图层: %+v", pc.Content)
	}
	items := pc.Content.Layer[0].Items
	if len(items) != 2 {
		t.Fatalf("页面对象数量 = %d, want 2", len(items))
	}
	for i, want := range []StID{10, 21} {
		if items[i].Kind != PageItemBlock {
			t.Fatalf("第 %d 个对象 Kind = %v, want PageItemBlock", i, items[i].Kind)
		}
		if items[i].Block == nil {
			t.Fatalf("第 %d 个 PageBlock 为空指针", i)
		}
		if items[i].Block.ID != want {
			t.Fatalf("第 %d 个 PageBlock ID = %d, want %d", i, items[i].Block.ID, want)
		}
	}
	if len(items[0].Block.Items) != 0 {
		t.Fatalf("自闭合 PageBlock 子对象 = %d, want 0", len(items[0].Block.Items))
	}
	if len(items[1].Block.Items) != 1 || items[1].Block.Items[0].Text == nil {
		t.Fatalf("非空 PageBlock 子对象未解析: %+v", items[1].Block.Items)
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
