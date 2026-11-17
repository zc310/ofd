package render

import (
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func textItem(boundary spec.StBox, actions *spec.Actions) spec.PageItem {
	return spec.PageItem{Kind: spec.PageItemText, Text: &spec.TextObject{CtText: spec.CtText{
		CTGraphicUnit: spec.CTGraphicUnit{Boundary: boundary, Actions: actions},
	}}}
}

func staticResolver(pageID spec.StID, ref LinkTargetRef) PageLinkResolver {
	return func(id spec.StID) (LinkTargetRef, bool) {
		if id != pageID {
			return LinkTargetRef{}, false
		}
		return ref, true
	}
}

func TestPageExternalLinksIgnoresInternalTargets(t *testing.T) {
	empty := &Document{}
	if links := empty.PageExternalLinks(nil); links != nil {
		t.Fatalf("nil 页面应返回 nil，实际 %+v", links)
	}
	var nilDocument *Document
	if links := nilDocument.PageLinks(nil, nil); links != nil {
		t.Fatalf("nil 文档应返回 nil，实际 %+v", links)
	}
}
