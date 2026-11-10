package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/font"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

// CLI 的独立 SVG 和 HTML 内嵌 SVG 都必须按页裁剪字体，而不是嵌入完整字体或跨页字形。
func TestSVGAndHTMLSVGEmbedOnlyPageFontSubsets(t *testing.T) {
	document := creator.Document{
		ID: "svg-subset", PageSize: creator.PageSize{Width: 60, Height: 30},
		Fonts: []creator.Font{{Name: "GoRegular", Format: "ttf", Data: goregular.TTF}},
	}
	for _, value := range []string{"AÁ", "Z"} {
		document.Pages = append(document.Pages, creator.Page{Items: []creator.Item{
			creator.Text{X: 5, Y: 5, Width: 40, Height: 15, Font: "GoRegular", Size: 6, Value: value},
		}})
	}
	data, err := creator.MarshalWithOptions(document, creator.CreateOptions{PreserveEmbeddedFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ofd")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`@font-face\{[^}]*base64,([^']+)`)
	for _, test := range []struct {
		name   string
		args   []string
		values []string
	}{
		{"page1.svg", []string{"--page", "1"}, []string{"AÁ"}},
		{"page2.svg", []string{"--page", "2"}, []string{"Z"}},
		{"pages.html", []string{"--html-format", "svg"}, []string{"AÁ", "Z"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(dir, test.name)
			opts, err := parseArgs(append(test.args, input, output))
			if err != nil {
				t.Fatal(err)
			}
			if err := run(opts); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			programs := pattern.FindAllSubmatch(data, -1)
			if len(programs) != len(test.values) {
				t.Fatalf("内嵌字体数量 = %d，期望 %d", len(programs), len(test.values))
			}
			for i, match := range programs {
				program, err := base64.StdEncoding.DecodeString(string(match[1]))
				if err != nil {
					t.Fatal(err)
				}
				if len(program) >= len(goregular.TTF)/2 {
					t.Fatalf("字体未缩减：%d 字节，完整字体 %d 字节", len(program), len(goregular.TTF))
				}
				subset, err := font.ParseSFNT(program, 0)
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range "AÁZB" {
					if used := strings.ContainsRune(test.values[i], r); used != (subset.GlyphIndex(r) != 0) {
						t.Errorf("第 %d 页字符 %c：使用=%v，子集字形=%d", i+1, r, used, subset.GlyphIndex(r))
					}
				}
				t.Logf("字体 %d：%d → %d 字节", i+1, len(goregular.TTF), len(program))
			}
		})
	}
}
