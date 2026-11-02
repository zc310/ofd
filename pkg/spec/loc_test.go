package spec

import (
	"encoding/xml"
	"testing"
)

func TestStLoc(t *testing.T) {
	tests := []struct {
		name     string
		base     StLoc
		target   StLoc
		expected StLoc
	}{
		// 基本路径解析
		{
			name:     "相对路径解析",
			base:     "/OFD/Doc_0",
			target:   "Pages/Page_0.xml",
			expected: "/OFD/Doc_0/Pages/Page_0.xml",
		},
		{
			name:     "绝对路径保持不变",
			base:     "/OFD/Doc_0",
			target:   "/Res/font.ttf",
			expected: "/Res/font.ttf",
		},
		// OFD 规范中的路径处理
		{
			name:     "处理 ..",
			base:     "/OFD/Doc_0/Pages",
			target:   "../Res/image.png",
			expected: "/OFD/Doc_0/Res/image.png",
		},
		{
			name:     "处理 .",
			base:     "/OFD/Doc_0",
			target:   "./Content.xml",
			expected: "/OFD/Doc_0/Content.xml",
		},
		{
			name:     "多个 ..",
			base:     "/OFD/Doc_0/Pages/SubPages",
			target:   "../../Res/fonts/1.ttf",
			expected: "/OFD/Doc_0/Res/fonts/1.ttf",
		},
		// 特殊路径
		{
			name:     "根路径",
			base:     "/",
			target:   "OFD.xml",
			expected: "/OFD.xml",
		},
		{
			name:     "空目标路径",
			base:     "/OFD/Doc_0",
			target:   "",
			expected: "/OFD/Doc_0",
		},
		{
			name:     "空基础路径",
			base:     "",
			target:   "Pages/Page_0.xml",
			expected: "Pages/Page_0.xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.target.Resolve(tt.base)
			if result != tt.expected {
				t.Errorf("期望 %q, 得到 %q", tt.expected, result)
			}
		})
	}
}

func TestStLocMethods(t *testing.T) {
	// 测试 Dir
	if got := StLoc("/OFD/Doc_0/Pages/Page_0.xml").Dir(); got != "/OFD/Doc_0/Pages" {
		t.Errorf("Dir 失败: 期望 /OFD/Doc_0/Pages, 得到 %s", got)
	}

	// 测试 Base
	if got := StLoc("/OFD/Doc_0/Pages/Page_0.xml").Base(); got != "Page_0.xml" {
		t.Errorf("Base 失败: 期望 Page_0.xml, 得到 %s", got)
	}

	// 测试 Ext
	if got := StLoc("/Res/fonts/1.ttf").Ext(); got != ".ttf" {
		t.Errorf("Ext 失败: 期望 .ttf, 得到 %s", got)
	}

	// 测试 Clean
	if got := StLoc("/OFD//Doc_0/./Pages/../Pages/Page_0.xml").Clean(); got != "/OFD/Doc_0/Pages/Page_0.xml" {
		t.Errorf("Clean 失败: 期望 /OFD/Doc_0/Pages/Page_0.xml, 得到 %s", got)
	}

	// 测试 HasPrefix
	path := StLoc("/OFD/Doc_0/Pages/Page_0.xml")
	if !path.HasPrefix("/OFD/Doc_0") {
		t.Errorf("HasPrefix 失败: 应为 true")
	}
	if path.HasPrefix("/Other") {
		t.Errorf("HasPrefix 失败: 应为 false")
	}
}

func TestStLocXMLPreservesOriginalElementAndAttributeText(t *testing.T) {
	var value struct {
		BaseLoc StLoc `xml:"BaseLoc,attr"`
		FileLoc StLoc `xml:"FileLoc"`
	}
	if err := xml.Unmarshal([]byte(`<Reference BaseLoc="../Res/./Font.TTF"><FileLoc>/Doc_0/../Doc_1/Content.xml</FileLoc></Reference>`), &value); err != nil {
		t.Fatal(err)
	}
	if value.BaseLoc != "../Res/./Font.TTF" || value.FileLoc != "/Doc_0/../Doc_1/Content.xml" {
		t.Fatalf("XML解码提前规范化了路径：%+v", value)
	}
	if got := value.BaseLoc.Resolve("/Doc_0/Pages"); got != "/Doc_0/Res/Font.TTF" {
		t.Fatalf("包内路径解析改变：%s", got)
	}
	if !StLoc("").IsEmpty() || StLoc("").String() != "" || StLoc("/x").IsEmpty() {
		t.Fatal("空路径语义改变")
	}
	if !StLoc("/Doc_0/Page.xml").IsAbsolute() || StLoc("Doc_0/Page.xml").IsAbsolute() {
		t.Fatal("绝对路径语义改变")
	}
	if got := StLoc("/Doc_0").Join("Pages", "../Res", "a.png"); got != "/Doc_0/Res/a.png" {
		t.Fatalf("Join语义改变：%s", got)
	}
	if !StLoc("/Doc_0/Page.xml").In("/Doc_0") || StLoc("/Doc_01/Page.xml").In("/Doc_0") {
		t.Fatal("目录前缀语义改变")
	}
	if got := StLoc("/Doc_0/Page.xml").TrimPrefix("/Doc_0/"); got != "Page.xml" {
		t.Fatalf("TrimPrefix语义改变：%s", got)
	}
}
