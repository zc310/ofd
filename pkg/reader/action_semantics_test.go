package reader

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/render/geom"
)

func actionSemanticsPackage(t *testing.T, actions, pageActions string) []byte {
	t.Helper()
	entries := map[string]string{
		"OFD.xml":          `<OFD><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 40 40</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/><Page ID="2" BaseLoc="Blank.xml"/></Pages></Document>`,
		"Doc/Page.xml":     `<Page><Content><Layer ID="10"><PathObject ID="11" Boundary="10 20 20 20"><Actions>` + actions + `</Actions><AbbreviatedData>M 0 0 L 20 20</AbbreviatedData></PathObject></Layer></Content>` + pageActions + `</Page>`,
		"Doc/Blank.xml":    `<Page/>`,
	}
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, content := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestPageLinksPreserveRegionsRelativeURIsAndActionSequence(t *testing.T) {
	region := `<Region><Area Start="1 2"><Line Point1="5 2"/><Line Point1="1 6"/><Close/></Area></Region>`
	data := actionSemanticsPackage(t, `<Action Event="CLICK">`+region+`<URI URI="../guide" Base="https://example.test/docs/a.ofd"/></Action><Action Event="CLICK">`+region+`<Goto><Dest Type="Fit" PageID="2"/></Goto></Action>`, "")
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	links, err := r.PageLinksForPage(0)
	if err != nil || len(links) != 1 {
		t.Fatalf("热区=%+v %v", links, err)
	}
	link := links[0]
	if link.Boundary != (AnnotationBoundary{X: 11, Y: 22, Width: 4, Height: 4}) || link.URI != "https://example.test/guide" || len(link.Actions) != 2 {
		t.Fatalf("区域或动作丢失：%+v", link)
	}
	if link.Actions[0].URI != link.URI || link.Actions[1].TargetPage != 1 || link.Actions[1].Scope != 0 || link.Actions[1].Page != 0 {
		t.Fatalf("动作顺序或作用域=%+v", link.Actions)
	}
	path := geom.MustParseSVGPath(link.Region)
	if !path.ContainsPoint(.1, .1, geom.NonZero) || path.ContainsPoint(.9, .9, geom.NonZero) {
		t.Fatal("复杂 Region 被矩形替代")
	}
}

func TestPageEntryActionsPreserveGotoAndAttachmentOrder(t *testing.T) {
	data := actionSemanticsPackage(t, "", `<Actions><Action Event="PO"><Goto><Dest Type="XYZ" PageID="2" Left="3" Top="4" Zoom="2"/></Goto></Action><Action Event="PO"><URI URI="help" Base="https://example.test/"/></Action><Action Event="PO"><GotoA AttachID="a"/></Action></Actions>`)
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	actions, err := r.PageEntryActionsForPage(0)
	if err != nil || len(actions) != 3 {
		t.Fatalf("进入动作=%+v %v", actions, err)
	}
	if actions[0].TargetPage != 1 || actions[0].Dest == nil || *actions[0].Dest.Top != 4 || actions[1].URI != "https://example.test/help" || actions[2].AttachmentID != "a" {
		t.Fatalf("顺序与目标=%+v", actions)
	}
}

func TestMovieRegionDoesNotShrinkAppearanceBoundary(t *testing.T) {
	data := actionSemanticsPackage(t, `<Action Event="CLICK"><Region><Area Start="1 2"><Line Point1="3 2"/><Line Point1="1 4"/><Close/></Area></Region><Movie ResourceID="7"/></Action>`, "")
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	links, err := r.PageLinksForPage(0)
	if err != nil || len(links) != 1 {
		t.Fatalf("影片动作=%+v %v", links, err)
	}
	link := links[0]
	if link.Boundary.Width != 2 || link.AppearanceBoundary == nil || link.AppearanceBoundary.Width != 20 || link.AppearanceBoundary.X != 10 {
		t.Fatalf("启动区覆盖了影片外观：%+v", link)
	}
}
