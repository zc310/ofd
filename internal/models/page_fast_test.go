package models

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

func TestParseTextCodePreservesMixedTextAndCDATA(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		value string
	}{
		{"plain", "中文正文", "中文正文"},
		{"entities", "甲&amp;&#x1F600;乙", "甲&😀乙"},
		{"mixed", "甲&amp;<![CDATA[&lt;]]><!--split-->&#x1F600;", "甲&&lt;😀"},
		{"comments", "a<!--split-->b&amp;c", "ab&c"},
		{"empty", "", ""},
		{"empty CDATA before text", "<![CDATA[]]>甲&amp;", "甲&"},
		{"empty CDATA after text", "甲&amp;<![CDATA[]]>", "甲&"},
		{"CDATA first", "<![CDATA[&amp;]]>甲&amp;", "&amp;甲&"},
		{"split entity stays literal", "&am<!--split-->p;", "&amp;"},
		{"split surrogate pair", `\D83D<!--split--><![CDATA[\DE00]]>`, "😀"},
		{"single after comment", "<!--split-->甲&amp;", "甲&"},
		{"single before ignored child", "甲&amp;<Unknown>ignored</Unknown>", "甲&"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPageLexer([]byte("<TextCode>" + tc.body + "</TextCode>"))
			p.Next()
			var text TextCode
			if err := p.parseTextCode(&text); err != nil {
				t.Fatal(err)
			}
			if text.Value != tc.value {
				t.Fatalf("TextCode 值 = %q, want %q", text.Value, tc.value)
			}
		})
	}
}

func TestParseTextCodeDoesNotRetainInputBytes(t *testing.T) {
	for _, body := range []string{"plain text", "甲&amp;乙", "<![CDATA[甲&amp;乙]]>", "甲<!--split-->乙"} {
		input := "<TextCode>" + body + "</TextCode>"
		// 预留终止符空间，让底层 lexer 复用输入而不是先复制整个 XML。
		data := make([]byte, len(input), len(input)+1)
		copy(data, input)
		p := newPageLexer(data)
		p.Next()
		var text TextCode
		if err := p.parseTextCode(&text); err != nil {
			t.Fatal(err)
		}
		want := strings.Clone(text.Value)
		clear(data)
		if text.Value != want {
			t.Fatalf("输入修改影响了 TextCode: got %q, want %q", text.Value, want)
		}
	}
}

func TestPageNumericAndTextParsingMatchesEncodingXML(t *testing.T) {
	data := []byte(`<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:Content><ofd:Layer ID="1">
<ofd:TextObject ID="2" Boundary="1 2 -30 40" CTM="1 0 0 1 2 3" Font="3" Size="4">
<ofd:FillColor Value="1 2 3 4 5"/>
<ofd:TextCode X="0" Y="3" DeltaX="1 g 3 2">甲&amp;<![CDATA[&lt;]]><!--split-->\D83D<![CDATA[\DE00]]></ofd:TextCode>
<ofd:TextCode DeltaY="1 2 3">正文</ofd:TextCode><ofd:TextCode><![CDATA[&amp;]]></ofd:TextCode>
</ofd:TextObject>
<ofd:PathObject ID="4" Boundary="1 2 30 40" CTM="1 0 0 1 2 3" DashPattern="g 3 2"><ofd:AbbreviatedData>M 0 0 L 10 20 C</ofd:AbbreviatedData></ofd:PathObject>
</ofd:Layer></ofd:Content></ofd:Page>`)
	var want PageContent
	if err := xml.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	got, err := ParsePageContentXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("页面解析结果不一致\n got=%#v\nwant=%#v", got, want)
	}
}

var benchTextCodeOutput TextCode

func BenchmarkParseTextCode(b *testing.B) {
	for _, input := range []struct {
		name  string
		value string
	}{
		{"ASCII14", "benchmark text"},
		{"CJK", "中文正文与标点，这是用于基准测试的文本内容。"},
		{"Plain1KiB", strings.Repeat("a", 1024)},
		{"Entities", "a&amp;b&lt;c&gt;d&#x1F600;"},
		{"Plain16KiB", strings.Repeat("a", 16<<10)},
		{"Mixed", "甲&amp;<![CDATA[&lt;]]><!--split-->&#x1F600;"},
		{"CDATA", "<![CDATA[甲&lt;乙]]>"},
		{"Escapes", `甲\0020乙\D83D\DE00`},
	} {
		b.Run(input.name, func(b *testing.B) {
			data := []byte("<TextCode>" + input.value + "</TextCode>")
			b.ReportAllocs()
			for b.Loop() {
				p := newPageLexer(data)
				p.Next()
				var text TextCode
				if err := p.parseTextCode(&text); err != nil {
					b.Fatal(err)
				}
				benchTextCodeOutput = text
			}
		})
	}
}

var benchPageContentOutput *PageContent

func BenchmarkParsePageContentXML(b *testing.B) {
	for _, input := range []struct{ name, object string }{
		{"Text", `<ofd:TextObject ID="2" Boundary="1 2 30 40" CTM="1 0 0 1 12.5 20.5" Font="3" Size="4"><ofd:TextCode X="0" Y="3">benchmark text 中文</ofd:TextCode></ofd:TextObject>`},
		{"PositionedText", `<ofd:TextObject ID="2" Boundary="1 2 30 40" CTM="1 0 0 1 12.5 20.5" Font="3" Size="4"><ofd:TextCode X="0" Y="3" DeltaX="1.5 0 0 0 1.5 0 0 0">正文甲乙丙丁戊己</ofd:TextCode></ofd:TextObject>`},
		{"MixedText", `<ofd:TextObject ID="2" Boundary="1 2 30 40" Font="3" Size="4"><ofd:TextCode X="0" Y="3">甲&amp;<![CDATA[&lt;]]><!--split-->&#x1F600;</ofd:TextCode></ofd:TextObject>`},
		{"Paths", `<ofd:PathObject ID="2" Boundary="1 2 30 40" CTM="1 0 0 1 12.5 20.5"><ofd:AbbreviatedData>M 0 0 L 10 20 L 30 40 C</ofd:AbbreviatedData></ofd:PathObject>`},
	} {
		b.Run(input.name, func(b *testing.B) {
			data := []byte(`<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:Content><ofd:Layer ID="1">` + strings.Repeat(input.object, 256) + `</ofd:Layer></ofd:Content></ofd:Page>`)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				var err error
				benchPageContentOutput, err = ParsePageContentXML(data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

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

// TestParsePageContentXMLNonNumericIDsMatchAcrossItemKinds 回归：快速解析器里
// PageBlock 走 UnmarshalText、Text/Path/Image 走 UnmarshalXMLAttr，非数字 ID 曾
// 分别得到 CRC64 哈希和 0。同一字符串 ID 在不同图元类型上必须一致。
func TestParsePageContentXMLNonNumericIDsMatchAcrossItemKinds(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">
  <ofd:Content><ofd:Layer ID="2">
    <ofd:PageBlock ID="item_1"><ofd:TextObject ID="item_1" Boundary="0 0 10 10" Font="1" Size="3"/></ofd:PageBlock>
  </ofd:Layer></ofd:Content>
</ofd:Page>`)

	pc, err := ParsePageContentXML(data)
	if err != nil {
		t.Fatal(err)
	}
	items := pc.Content.Layer[0].Items
	if len(items) != 1 || items[0].Block == nil || len(items[0].Block.Items) != 1 {
		t.Fatalf("未解析到 PageBlock/TextObject: %+v", items)
	}
	blockID := items[0].Block.ID
	textID := items[0].Block.Items[0].Text.ID
	if blockID == 0 {
		t.Fatal("非数字 ID 不应落到 0")
	}
	if blockID != textID {
		t.Fatalf("PageBlock ID = %d 与 TextObject ID = %d 不一致", blockID, textID)
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
