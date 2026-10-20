package models

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPageBlockDepthLimitMatchesFastAndStandardDecoders(t *testing.T) {
	for _, depth := range []int{maxPageBlockDepth, maxPageBlockDepth + 1, 10000} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			data := []byte(`<Page><Content><Layer ID="1">` + strings.Repeat(`<PageBlock ID="2">`, depth) + `<PathObject ID="3" Boundary="0 0 1 1"><AbbreviatedData>M 0 0 L 1 1</AbbreviatedData></PathObject>` + strings.Repeat(`</PageBlock>`, depth) + `</Layer></Content></Page>`)
			for _, decode := range []struct {
				name string
				fn   func([]byte) (*PageContent, error)
			}{
				{"fast", ParsePageContentXML},
				{"standard", func(data []byte) (*PageContent, error) {
					var page PageContent
					err := xml.Unmarshal(data, &page)
					return &page, err
				}},
			} {
				t.Run(decode.name, func(t *testing.T) {
					page, err := decode.fn(data)
					if depth > maxPageBlockDepth {
						if !errors.Is(err, errPageBlockDepth) {
							t.Fatalf("超深页面应返回嵌套限制错误，得到 %v", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					items := page.Content.Layer[0].Items
					for level := 0; level < depth; level++ {
						items = items[0].Block.Items
					}
					if len(items) != 1 || items[0].Path == nil || items[0].Path.ID != 3 {
						t.Fatal("允许深度内的最深图元没有保留")
					}
				})
			}
		})
	}
}
