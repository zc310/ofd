package webreader

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/klauspost/compress/zip"
)

// 两个文档体复用页 ID 1、2，且顺序不同；10、20 分别只存在于其中一个文档体。
func duplicatePageIDsPackage(t *testing.T) []byte {
	t.Helper()
	path := func(id, target int) string {
		return fmt.Sprintf(`<PathObject ID="%d" Boundary="0 0 10 10"><Actions><Action Event="CLICK"><Goto><Dest Type="Fit" PageID="%d"/></Goto></Action></Actions><AbbreviatedData>M 0 0 L 10 10</AbbreviatedData></PathObject>`, id, target)
	}
	entries := map[string]string{
		"OFD.xml": `<OFD Version="1.0"><DocBody><DocRoot>A/Document.xml</DocRoot><Signatures>A/Signatures.xml</Signatures></DocBody><DocBody><DocRoot>B/Document.xml</DocRoot><Signatures>B/Signatures.xml</Signatures></DocBody></OFD>`,
	}
	for _, doc := range []struct {
		dir         string
		pages       [3]int
		foreignPage int
	}{
		{"A", [3]int{1, 2, 10}, 20},
		{"B", [3]int{2, 1, 20}, 10},
	} {
		pages := ""
		for i, id := range doc.pages {
			pages += fmt.Sprintf(`<Page ID="%d" BaseLoc="P%d.xml"/>`, id, i)
		}
		entries[doc.dir+"/Document.xml"] = fmt.Sprintf(`<Document>
<CommonData><MaxUnitID>400</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><TemplatePage ID="300" BaseLoc="Template.xml"/></CommonData>
<Pages>%s</Pages><Annotations>Annotations.xml</Annotations>
<Bookmarks><Bookmark Name="local"><Dest Type="Fit" PageID="2"/></Bookmark><Bookmark Name="foreign"><Dest Type="Fit" PageID="%d"/></Bookmark></Bookmarks>
<Actions><Action Event="CLICK"><Goto><Bookmark Name="local"/></Goto></Action><Action Event="CLICK"><Goto><Bookmark Name="foreign"/></Goto></Action></Actions>
</Document>`, pages, doc.foreignPage)
		entries[doc.dir+"/P0.xml"] = `<Page><Template TemplateID="300"/><Content><Layer ID="100">` + path(101, 2) + path(102, doc.foreignPage) + `
<TextObject ID="103" Boundary="10 20 20 5" Font="9" Size="2"><TextCode X="0" Y="2">local</TextCode></TextObject>
<TextObject ID="104" Boundary="10 30 20 5" Font="9" Size="2"><TextCode X="0" Y="2">foreign</TextCode></TextObject>
</Layer></Content></Page>`
		entries[doc.dir+"/P1.xml"] = `<Page><Content><Layer ID="110"/></Content></Page>`
		entries[doc.dir+"/P2.xml"] = `<Page><Content><Layer ID="120"/></Content></Page>`
		entries[doc.dir+"/Template.xml"] = `<Page><Content><Layer ID="310">` + path(301, 2) + path(302, doc.foreignPage) + `</Layer></Content></Page>`
		entries[doc.dir+"/Annotations.xml"] = fmt.Sprintf(`<Annotations><Page PageID="%d"><FileLoc>Annot.xml</FileLoc></Page></Annotations>`, doc.pages[0])
		entries[doc.dir+"/Annot.xml"] = `<PageAnnot><Annot ID="201" Type="Link" Creator="test" LastModDate="2026-01-01"><Appearance Boundary="10 40 10 10">` + path(211, 2) + `</Appearance></Annot><Annot ID="202" Type="Link" Creator="test" LastModDate="2026-01-01"><Appearance Boundary="10 50 10 10">` + path(212, doc.foreignPage) + `</Appearance></Annot></PageAnnot>`
		entries[doc.dir+"/Signatures.xml"] = `<Signatures><Signature ID="s1" BaseLoc="Signature.xml"/></Signatures>`
		entries[doc.dir+"/Signature.xml"] = fmt.Sprintf(`<Signature><SignedInfo><Provider ProviderName="test"/><StampAnnot ID="local" PageRef="2" Boundary="10 10 10 10"/><StampAnnot ID="foreign" PageRef="%d" Boundary="10 20 10 10"/></SignedInfo></Signature>`, doc.foreignPage)
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
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
	return data.Bytes()
}

func TestPageLinksResolveDuplicatePageIDsWithinDocBody(t *testing.T) {
	r, err := Open(duplicatePageIDsPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	check := func(t *testing.T, links []PageLink, scope int) {
		t.Helper()
		want := map[string]int{
			"101": 1, "102": -1, // 正文动作。
			"103": 1, "104": -1, // 文档书签动作。
			"301": 1, "302": -1, // 模板动作。
			"201": 1, // 注解动作；无法解析的注解链接不生成热区。
		}
		if scope == 1 {
			for id, target := range want {
				if target >= 0 {
					want[id] = 3
				}
			}
		}
		if len(links) != len(want) {
			t.Fatalf("scope=%d 链接数量=%d，期望 %d: %+v", scope, len(links), len(want), links)
		}
		for _, link := range links {
			target, ok := want[link.ID]
			if !ok || link.Scope != scope || link.Page != scope*3 || link.TargetPage != target || link.Dest == nil {
				t.Errorf("scope=%d 链接=%+v，期望目标页 %d（存在=%v）", scope, link, target, ok)
			}
			delete(want, link.ID)
		}
	}
	t.Run("PageLinksForPage", func(t *testing.T) {
		for index := 0; index < r.PageCount(); index++ {
			links, err := r.PageLinksForPage(index)
			if err != nil {
				t.Fatal(err)
			}
			if index%3 == 0 {
				check(t, links, index/3)
			} else if len(links) != 0 {
				t.Errorf("空白页 %d 出现链接: %+v", index, links)
			}
		}
	})
	t.Run("PageLinks", func(t *testing.T) {
		links, err := r.PageLinks()
		if err != nil {
			t.Fatal(err)
		}
		if len(links) != 14 {
			t.Fatalf("链接数量=%d，期望 14: %+v", len(links), links)
		}
		for scope := 0; scope < 2; scope++ {
			var scoped []PageLink
			for _, link := range links {
				if link.Scope == scope {
					scoped = append(scoped, link)
				}
			}
			check(t, scoped, scope)
		}
	})
}

func TestAnnotationsResolveDuplicatePageIDsWithinDocBody(t *testing.T) {
	r, err := Open(duplicatePageIDsPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	infos, err := r.Annotations()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 4 {
		t.Fatalf("注解数量=%d，期望 4: %+v", len(infos), infos)
	}
	for i, info := range infos {
		scope := i / 2
		target := -1
		if i%2 == 0 {
			target = []int{1, 3}[scope]
		}
		if info.Scope != scope || info.Page != scope*3 || info.TargetPage != target || info.Dest == nil {
			t.Errorf("注解=%+v，期望 scope=%d page=%d target=%d", info, scope, scope*3, target)
		}
	}
}

func TestSignaturesResolveDuplicatePageIDsWithinDocBody(t *testing.T) {
	r, err := Open(duplicatePageIDsPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	infos, err := r.Signatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("签名数量=%d，期望 2: %+v", len(infos), infos)
	}
	for scope, info := range infos {
		if info.Scope != scope || len(info.Stamps) != 2 {
			t.Fatalf("签名=%+v，期望 scope=%d 和两个签章", info, scope)
		}
		for _, stamp := range info.Stamps {
			target := -1
			if stamp.ID == "local" {
				target = []int{1, 3}[scope]
			}
			if stamp.Page != target {
				t.Errorf("scope=%d 签章=%+v，期望 page=%d", scope, stamp, target)
			}
		}
	}
}
