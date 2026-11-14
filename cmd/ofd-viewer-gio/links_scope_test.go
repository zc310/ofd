package main

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/klauspost/compress/zip"
)

// 两个文档体复用页ID及书签名，正文、模板和批注的书签引用不能跨作用域。
func TestInternalLinksResolveBookmarksAndPageIDsWithinDocumentBody(t *testing.T) {
	entries := map[string]string{
		"OFD.xml": `<OFD Version="1.0"><DocBody><DocRoot>A/Document.xml</DocRoot></DocBody><DocBody><DocRoot>B/Document.xml</DocRoot></DocBody></OFD>`,
	}
	path := func(id int, target string) string {
		return fmt.Sprintf(`<PathObject ID="%d" Boundary="10 10 30 10"><Actions><Action Event="CLICK"><Goto>%s</Goto></Action></Actions><AbbreviatedData>M 0 0 L 30 0 L 30 10 C</AbbreviatedData></PathObject>`, id, target)
	}
	for _, doc := range []struct {
		dir          string
		first, last  int
		bookmarkPage int
	}{{"A", 1, 2, 2}, {"B", 2, 1, 1}} {
		entries[doc.dir+"/Document.xml"] = fmt.Sprintf(`<Document><CommonData><MaxUnitID>1000</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><TemplatePage ID="500" BaseLoc="Template.xml"/></CommonData><Pages><Page ID="%d" BaseLoc="P0.xml"/><Page ID="%d" BaseLoc="P1.xml"/></Pages><Bookmarks><Bookmark Name="chapter"><Dest Type="XYZ" PageID="%d" Top="50" Zoom="0"/></Bookmark></Bookmarks><Annotations>Annotations.xml</Annotations></Document>`, doc.first, doc.last, doc.bookmarkPage)
		entries[doc.dir+"/P0.xml"] = `<Page><Template TemplateID="500"/><Content><Layer ID="100">` +
			path(101, `<Bookmark Name="chapter"/>`) + path(102, `<Dest Type="Fit" PageID="2"/>`) +
			path(103, `<Bookmark Name="missing"/>`) + path(104, `<Dest Type="Fit" PageID="999"/>`) + `</Layer></Content></Page>`
		entries[doc.dir+"/P1.xml"] = `<Page><Content><Layer ID="200"/></Content></Page>`
		entries[doc.dir+"/Template.xml"] = `<Page><Content><Layer ID="501">` + path(502, `<Bookmark Name="chapter"/>`) + `</Layer></Content></Page>`
		entries[doc.dir+"/Annotations.xml"] = fmt.Sprintf(`<Annotations><Page PageID="%d"><FileLoc>Annot.xml</FileLoc></Page></Annotations>`, doc.first)
		entries[doc.dir+"/Annot.xml"] = `<PageAnnot><Annot ID="600" Type="Link" Creator="test" LastModDate="2026-01-01"><Appearance Boundary="10 80 30 10">` + path(601, `<Bookmark Name="chapter"/>`) + `</Appearance></Annot></PageAnnot>`
	}
	var encoded bytes.Buffer
	archive := zip.NewWriter(&encoded)
	for name, content := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := openDocumentInput("scoped-links.ofd", encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	for scope := range 2 {
		scene, err := m.sceneAt(scope * 2)
		if err != nil {
			t.Fatal(err)
		}
		linked, ok := scene.(*linkedPageScene)
		if !ok || len(linked.links) != 4 {
			t.Fatalf("文档体%d链接数量错误: %#v", scope, scene)
		}
		for _, link := range linked.links {
			want := scope*2 + 1
			if scope == 1 && link.box.ObjectID == 102 {
				want = 2
			}
			if link.target == nil || link.target.Page != want {
				t.Fatalf("文档体%d目标串到其他作用域: %+v，期望页%d", scope, link.target, want)
			}
			if link.box.ObjectID != 102 && link.target.Top != 50 {
				t.Fatalf("丢失书签位置: %+v", link.target)
			}
		}
	}
}
