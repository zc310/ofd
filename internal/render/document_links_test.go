package render

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func textItem(boundary models.StBox, actions *models.Actions) models.PageItem {
	return models.PageItem{Kind: models.PageItemText, Text: &models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: boundary, Actions: actions},
	}}}
}

func staticResolver(pageID models.StID, ref LinkTargetRef) PageLinkResolver {
	return func(id models.StID) (LinkTargetRef, bool) {
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
