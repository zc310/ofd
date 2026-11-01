package reader

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

func TestPreferencesExposeTitleAndUIFlagsWithTable9Defaults(t *testing.T) {
	for _, name := range []string{"无偏好", "缺省标题", "显式标题", "显式文件名"} {
		t.Run(name, func(t *testing.T) {
			var preferences *creator.ViewPreferences
			want := "FileName"
			if name != "无偏好" {
				hide := true
				preferences = &creator.ViewPreferences{HideToolbar: &hide, HideMenubar: &hide, HideWindowUI: &hide}
				if name == "显式标题" {
					preferences.TabDisplay = "DocTitle"
					want = "DocTitle"
				}
				if name == "显式文件名" {
					preferences.TabDisplay = "FileName"
				}
			}
			data, err := creator.Marshal(creator.Document{ID: "preferences", Title: "文档标题", Preferences: preferences, Pages: []creator.Page{{}}})
			if err != nil {
				t.Fatal(err)
			}
			r, err := Open(data)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			got, err := r.Preferences()
			if err != nil {
				t.Fatal(err)
			}
			if got.TabDisplay != want || got.HideToolbar != (preferences != nil) || got.HideMenubar != (preferences != nil) || got.HideWindowUI != (preferences != nil) {
				t.Fatalf("偏好=%+v", got)
			}
			if preferences != nil && got.DocumentTitle != "文档标题" {
				t.Fatalf("标题丢失：%+v", got)
			}
		})
	}
}

func TestPreferencesTitleUsesSameDocumentBodyAsPreferences(t *testing.T) {
	mode := models.TabDisplay("DocTitle")
	first, second := "第一文档体", "第二文档体"
	r := &Reader{ofd: &parser.OFD{
		OFD:       spec.OFD{DocBodies: []spec.DocBody{{DocInfo: spec.DocInfo{Title: &first}}, {DocInfo: spec.DocInfo{Title: &second}}}},
		Documents: []*parser.Document{{}, {Document: models.Document{VPreferences: &models.CT_VPreferences{TabDisplay: &mode}}}},
	}}
	got := r.computePreferences()
	if got.DocumentTitle != second || got.TabDisplay != "DocTitle" {
		t.Fatalf("跨文档体串用标题：%+v", got)
	}
}
