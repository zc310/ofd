package reader

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/creator"
)

func TestObjectAndAnnotationBookmarkLinksUseDocumentScope(t *testing.T) {
	data := duplicatePageIDsPackage(t)
	source, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, file := range source.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(file.Name, "/Document.xml") {
			content = bytes.ReplaceAll(content, []byte(`<Dest Type="Fit" PageID="2"/>`), []byte(`<Bookmark Name="local"/>`))
		}
		w, err := archive.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for scope, page := range []int{0, 3} {
		links, err := r.PageLinksForPage(page)
		if err != nil {
			t.Fatal(err)
		}
		seen := make(map[string]bool)
		for _, link := range links {
			if link.ID == "101" || link.ID == "301" || link.ID == "201" {
				seen[link.ID] = true
				if link.Scope != scope || link.TargetPage != []int{1, 3}[scope] || link.Dest == nil {
					t.Errorf("对象书签未按文档体解析：%+v", link)
				}
			}
		}
		if len(seen) != 3 {
			t.Errorf("正文、模板和注解书签链接未全部返回：%v", seen)
		}
	}
	annotations, err := r.Annotations()
	if err != nil {
		t.Fatal(err)
	}
	for _, annot := range annotations {
		if annot.ID == "201" && annot.TargetPage != []int{1, 3}[annot.Scope] {
			t.Errorf("注解元数据书签目标错误：%+v", annot)
		}
	}
}

func TestPageMediaActionsOnlyReturnPageOpenEvents(t *testing.T) {
	click := creator.Action{Event: creator.ActionEventClick, Sound: &creator.SoundAction{ResourceID: 40}}
	open := creator.Action{Event: creator.ActionEventPO, Movie: &creator.MovieAction{ResourceID: 41}}
	data, err := creator.Marshal(creator.Document{
		ID: "page-media-events",
		Media: []creator.Media{
			{ID: 40, Type: "Audio", Format: "mp3", Data: []byte("audio")},
			{ID: 41, Type: "Video", Format: "mp4", Data: []byte("video")},
		},
		Pages: []creator.Page{
			{Actions: []creator.Action{click}},
			{Actions: []creator.Action{click, open}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for page := 0; page < 2; page++ {
		actions, err := r.PageMediaActionsForPage(page)
		if err != nil {
			t.Fatal(err)
		}
		if len(actions) != page {
			t.Fatalf("第 %d 页媒体动作数量=%d，期望 %d: %+v", page, len(actions), page, actions)
		}
		if page == 1 && (actions[0].MediaID != 41 || actions[0].Event != "PO") {
			t.Errorf("应跳过 CLICK 并继续寻找 PO：%+v", actions)
		}
	}
	actions, err := r.PageMediaActions()
	if err != nil || len(actions) != 1 || actions[0].Page != 1 || actions[0].MediaID != 41 || actions[0].Event != "PO" {
		t.Errorf("全量页面媒体动作=%+v err=%v", actions, err)
	}
}

func TestPageEntryActionsIncludePageOpenURI(t *testing.T) {
	pages := make([]creator.Page, 27)
	pages[26].Actions = []creator.Action{{
		Event: creator.ActionEventPO,
		URI:   &creator.URIAction{URI: "https://www.ofdspec.org/"},
	}}
	data, err := creator.Marshal(creator.Document{ID: "page-entry-uri", Pages: pages})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.PageCount() != 27 {
		t.Fatalf("creator 生成页数=%d，期望 27", r.PageCount())
	}
	actions, err := r.PageEntryActionsForPage(26)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Event != "PO" || actions[0].URI != "https://www.ofdspec.org/" || actions[0].Page != 26 {
		t.Fatalf("第 27 页页面打开 URI 动作=%+v", actions)
	}
	previous, err := r.PageEntryActionsForPage(25)
	if err != nil {
		t.Fatal(err)
	}
	if len(previous) != 0 {
		t.Fatalf("第 26 页不应返回第 27 页动作：%+v", previous)
	}
}

func TestPageNavigationIndexesFollowVersionSwitch(t *testing.T) {
	r, err := Open(historicalVersionPackage(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.PageLinksForPage(2); err != nil {
		t.Fatal(err)
	}
	if r.firstPageOfScope(1) != 2 {
		t.Fatal("主版本第二文档体首页应为 2")
	}
	if err := r.SelectVersion(0, "v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PageLinksForPage(1); err != nil {
		t.Fatal(err)
	}
	if r.firstPageOfScope(1) != 1 || r.pageIndexesByScope()[1][10] != 1 {
		t.Fatal("历史版本页数变化后保留了旧的全局页索引")
	}
}

func TestOutlineUsesReaderPageIndexesAfterFiltering(t *testing.T) {
	for _, fixture := range []struct {
		name string
		data []byte
	}{
		{"outlines", historicalVersionPackage(t, false)},
		{"bookmarks", duplicatePageIDsPackage(t)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			r, err := Open(fixture.data)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			original, err := r.Outline()
			if err != nil {
				t.Fatal(err)
			}
			// 模拟按页过滤：解析文档的 Pages 不变，阅读器的全局页序减少一页。
			r.mu.Lock()
			r.pages = r.pages[1:]
			r.resetPageNavigationLocked()
			r.metadata = &readerMetadata{}
			r.mu.Unlock()
			filtered, err := r.Outline()
			if err != nil {
				t.Fatal(err)
			}
			wantPage := func(page int) int {
				if page <= 0 {
					return -1
				}
				return page - 1
			}
			if len(original.Nodes) != len(filtered.Nodes) || len(original.Bookmarks) != len(filtered.Bookmarks) {
				t.Fatal("页过滤改变了大纲或书签数量")
			}
			for i, node := range filtered.Nodes {
				if node.Page != wantPage(original.Nodes[i].Page) {
					t.Errorf("大纲目标=%d，期望 %d", node.Page, wantPage(original.Nodes[i].Page))
				}
			}
			for i, bookmark := range filtered.Bookmarks {
				if bookmark.Page != wantPage(original.Bookmarks[i].Page) {
					t.Errorf("书签目标=%d，期望 %d", bookmark.Page, wantPage(original.Bookmarks[i].Page))
				}
			}
		})
	}
}
