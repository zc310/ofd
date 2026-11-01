//go:build js && wasm

package main

import (
	"testing"

	ofdreader "github.com/zc310/ofd/pkg/reader"
)

func TestPageLinkValuePreservesRegionSequenceAndMovieAppearance(t *testing.T) {
	link := ofdreader.PageLink{Scope: 2, Page: 3, URI: "https://example.test/", Boundary: ofdreader.AnnotationBoundary{X: 1, Y: 2, Width: 3, Height: 4}, Region: "M0 0L1 0L0 1Z",
		Actions: []ofdreader.PageLink{
			{Scope: 2, Page: 3, MediaID: 7, MediaKind: "sound", Synchronous: true, TargetPage: -1},
			{Scope: 2, Page: 3, MediaID: 8, MediaKind: "movie", AppearanceBoundary: &ofdreader.AnnotationBoundary{Width: 20, Height: 30}},
		},
	}
	value := objectValue(pageLinkValue(link))
	if value.Get("region").String() != link.Region || value.Get("actions").Length() != 2 {
		t.Fatalf("热区或动作列表未传给浏览器：%v", value)
	}
	first := value.Get("actions").Index(0)
	if !first.Get("synchronous").Bool() || first.Get("scope").Int() != 2 || first.Get("page").Int() != 3 {
		t.Fatalf("声音同步与作用域丢失：%v", first)
	}
	second := value.Get("actions").Index(1)
	if second.Get("appearance_boundary").Get("width").Int() != 20 || second.Get("media_id").Int() != 8 {
		t.Fatalf("影片外观丢失：%v", second)
	}
}
