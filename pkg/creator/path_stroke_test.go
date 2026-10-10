package creator_test

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

// pageXMLFrom 取包内第一份页面内容 XML。
func pageXMLFrom(t *testing.T, data []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("打开 ZIP 失败: %v", err)
	}
	for _, file := range reader.File {
		if !strings.HasSuffix(file.Name, "Content.xml") {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", file.Name, err)
		}
		content, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", file.Name, err)
		}
		return string(content)
	}
	t.Fatal("包内没有页面内容 XML")
	return ""
}

// CT_Path 的 Stroke 在 schema 里缺省为 true（internal/schema/xsd/Page.xsd），
// 省略该属性等于声明描边。填充路径若不显式写出 Stroke="false"，就会被套上缺省的
// 0.25mm 黑边，把压在它上面的浅色边框线盖成深色粗线。
func TestFillOnlyPathWritesStrokeFalse(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "stroke-default",
		Area: &creator.PageArea{
			PhysicalBox: &creator.Box{Width: 210, Height: 297},
		},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Path{
				X: 10, Y: 10, Width: 40, Height: 8,
				Data: "M 0 0 L 40 0 L 40 8 L 0 8 C",
				Fill: true,
			},
		}}},
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if !strings.Contains(pageXMLFrom(t, data), `Stroke="false"`) {
		t.Fatal("填充路径没有写出 Stroke=\"false\"，按 schema 会继承到描边")
	}
}

// 显式声明描边的路径照旧写 true。
func TestStrokedPathKeepsStrokeTrue(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "stroke-true",
		Area: &creator.PageArea{
			PhysicalBox: &creator.Box{Width: 210, Height: 297},
		},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Path{
				X: 10, Y: 10, Width: 40, Height: 0.2,
				Data:   "M 0 0 L 40 0 C",
				Stroke: true, LineWidth: 0.2,
				StrokeColor: &creator.Color{R: 0xc4, G: 0xcb, B: 0xd1},
			},
		}}},
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	page := pageXMLFrom(t, data)
	if !strings.Contains(page, `Stroke="true"`) {
		t.Fatal("描边路径没有写出 Stroke=\"true\"")
	}
	if strings.Contains(page, `Stroke="false"`) {
		t.Fatal("描边路径不应写出 Stroke=\"false\"")
	}
}
