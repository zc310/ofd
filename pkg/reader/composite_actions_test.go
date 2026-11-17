package reader

import (
	"bytes"
	"encoding/xml"
	"reflect"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

// 空外观保留完整单元坐标，不依赖字体、系统媒体解码或墨迹裁边的兼容分支。
func compositeActionsPackage(t *testing.T, clipped bool) []byte {
	t.Helper()
	entries := map[string]string{"OFD.xml": `<OFD><DocBody><DocRoot>A/Document.xml</DocRoot></DocBody><DocBody><DocRoot>B/Document.xml</DocRoot></DocBody></OFD>`}
	region := `<Region><Area Start="0 0"><Line Point1="4 0"/><Line Point1="0 4"/><Close/></Area></Region>`
	actions := `<Action Event="CLICK">` + region + `<URI URI="guide" Base="https://example.test/docs/"/></Action>` +
		`<Action Event="CLICK">` + region + `<Goto><Bookmark Name="local"/></Goto></Action>` +
		`<Action Event="CLICK">` + region + `<GotoA AttachID="attachment"/></Action>` +
		`<Action Event="CLICK">` + region + `<Sound ResourceID="7" Volume="35" Synchronous="true"/></Action>` +
		`<Action Event="CLICK">` + region + `<Movie ResourceID="8" Operator="Pause"/></Action>`
	for _, dir := range []string{"A", "B"} {
		entries[dir+"/Document.xml"] = `<Document><CommonData><PageArea><PhysicalBox>0 0 100 100</PhysicalBox></PageArea><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="90" BaseLoc="Template.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/><Page ID="2" BaseLoc="Blank.xml"/></Pages><Bookmarks><Bookmark Name="local"><Dest Type="XYZ" PageID="2" Top="4" Zoom="2"/></Bookmark></Bookmarks><Annotations>Annotations.xml</Annotations></Document>`
		clip := ""
		if clipped {
			// 只显示局部坐标的左半边，内层 Region 不得越过此父裁剪。
			clip = `<Clips><Clip><Area><Path Boundary="0 0 20 20"><AbbreviatedData>M 0 0 L 4 0 L 4 20 L 0 20 C</AbbreviatedData></Path></Area></Clip></Clips>`
		}
		entries[dir+"/Page.xml"] = `<Page><Template TemplateID="90"/><Content><Layer ID="10"><CompositeObject ID="11" ResourceID="100" Boundary="10 10 40 40" CTM="2 0 0 2 0 0">` + clip + `</CompositeObject><CompositeObject ID="12" ResourceID="101" Boundary="60 10 10 10"/></Layer></Content></Page>`
		entries[dir+"/Template.xml"] = `<Page><Content><Layer ID="20"><CompositeObject ID="21" ResourceID="101" Boundary="50 40 10 10"/></Layer></Content></Page>`
		entries[dir+"/Blank.xml"] = `<Page/>`
		entries[dir+"/Res.xml"] = `<Res BaseLoc="."><CompositeGraphicUnits>` +
			`<CompositeGraphicUnit ID="100" Width="20" Height="20"><Content><CompositeObject ID="110" ResourceID="101" Boundary="2 3 10 10"/></Content></CompositeGraphicUnit>` +
			`<CompositeGraphicUnit ID="101" Width="10" Height="10"><Content><PathObject ID="111" Boundary="1 1 4 4" Stroke="false"><Actions>` + actions + `</Actions><AbbreviatedData/></PathObject></Content></CompositeGraphicUnit>` +
			`</CompositeGraphicUnits></Res>`
		entries[dir+"/Annotations.xml"] = `<Annotations><Page PageID="1"><FileLoc>Annot.xml</FileLoc></Page></Annotations>`
		entries[dir+"/Annot.xml"] = `<PageAnnot><Annot ID="30" Type="Link" Creator="test" LastModDate="2026-01-01"><Appearance Boundary="5 60 30 30"><CompositeObject ID="31" ResourceID="101" Boundary="0 0 10 10"/></Appearance></Annot></PageAnnot>`
	}
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, value := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestCompositeActionsPreserveSequenceRegionsBookmarksAndScope(t *testing.T) {
	r, err := Open(compositeActionsPackage(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	wantBoxes := []AnnotationBoundary{{X: 16, Y: 18, Width: 8, Height: 8}, {X: 61, Y: 11, Width: 4, Height: 4}, {X: 51, Y: 41, Width: 4, Height: 4}, {X: 6, Y: 61, Width: 4, Height: 4}}
	for scope, page := range []int{0, 2} {
		links, err := r.PageLinksForPage(page)
		if err != nil || len(links) != 4 {
			t.Fatalf("scope=%d 热区=%+v %v", scope, links, err)
		}
		for i, link := range links {
			if link.Boundary != wantBoxes[i] || link.Scope != scope || link.Page != page || len(link.Actions) != 5 {
				t.Fatalf("映射、序列或作用域错误：%+v", link)
			}
			for _, action := range link.Actions {
				if action.Scope != scope || action.Page != page || action.Boundary != wantBoxes[i] {
					t.Fatalf("内部动作未映射：%+v", action)
				}
			}
			a := link.Actions
			if a[0].URI != "https://example.test/docs/guide" || a[1].TargetPage != page+1 || a[1].Dest == nil || *a[1].Dest.Top != 4 || a[2].AttachmentID != "attachment" || a[3].MediaID != 7 || !a[3].Synchronous || a[3].Volume == nil || *a[3].Volume != 35 || a[4].MediaID != 8 || a[4].Operator != "Pause" {
				t.Fatalf("动作类型、书签或 XML 顺序丢失：%+v", a)
			}
			if a[4].AppearanceBoundary == nil || *a[4].AppearanceBoundary != wantBoxes[i] {
				t.Fatalf("影片播放窗口未映射：%+v", a[4])
			}
			path := geom.MustParseSVGPath(link.Region)
			if !path.ContainsPoint(.1, .1, geom.NonZero) || path.ContainsPoint(.9, .9, geom.NonZero) {
				t.Fatal("复合图元的三角形热区退化为矩形")
			}
		}
		// 重复读取不改写缓存的资源坐标或动作切片。
		again, err := r.PageLinksForPage(page)
		if err != nil || !reflect.DeepEqual(links, again) {
			t.Fatalf("重复读取改变共享动作：%v", err)
		}
	}
}

func TestCompositeActionsHonorParentClipsAndHiddenLayer(t *testing.T) {
	r, err := Open(compositeActionsPackage(t, true))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	links, err := r.PageLinksForPage(0)
	if err != nil || len(links) != 4 {
		t.Fatalf("裁剪热区=%+v %v", links, err)
	}
	if box := links[0].Boundary; box.X != 16 || box.Y != 18 || box.Width != 2 || box.Height != 8 {
		t.Fatalf("父裁剪未映射：%+v", box)
	}
	for _, action := range links[0].Actions {
		if action.Boundary != links[0].Boundary {
			t.Fatalf("动作绕过父裁剪：%+v", action)
		}
	}
	if err := r.SetHiddenLayers(0, []string{"10"}); err != nil {
		t.Fatal(err)
	}
	links, err = r.PageLinksForPage(0)
	if err != nil || len(links) != 2 {
		t.Fatalf("隐藏父图层仍返回复合热区：%+v %v", links, err)
	}
	if links[0].Boundary.X != 51 || links[1].Boundary.X != 6 {
		t.Fatalf("隐藏正文不应隐藏模板与 Link 注解：%+v", links)
	}
	if err := r.SetHiddenLayers(0, []string{"10", "template:90"}); err != nil {
		t.Fatal(err)
	}
	links, err = r.PageLinksForPage(0)
	if err != nil || len(links) != 1 || links[0].Boundary.X != 6 {
		t.Fatalf("隐藏模板仍有复合热区：%+v %v", links, err)
	}
}

func TestCompositeActionPlacementClipsEachActionWithoutMutatingResource(t *testing.T) {
	var object spec.PathObject
	data := `<PathObject ID="1" Boundary="0 0 20 20"><Actions><Action Event="CLICK"><Region><Area Start="0 0"><Line Point1="4 0"/><Line Point1="4 4"/><Line Point1="0 4"/><Close/></Area></Region><URI URI="https://example.test/left"/></Action><Action Event="CLICK"><Region><Area Start="10 0"><Line Point1="14 0"/><Line Point1="14 4"/><Line Point1="10 4"/><Close/></Area></Region><GotoA AttachID="right"/></Action></Actions><AbbreviatedData/></PathObject>`
	if err := xml.Unmarshal([]byte(data), &object); err != nil {
		t.Fatal(err)
	}
	link, ok := graphicActionLink(object.CTGraphicUnit, nil, nil)
	if !ok || len(link.Actions) != 2 {
		t.Fatalf("初始动作=%+v", link)
	}
	matrix := geom.Identity.Translate(20, 30).Scale(2, 2)
	clip := geom.Rectangle(8, 8).Translate(40, 30)
	if !placePageLink(&link, 2, 5, matrix, clip) || len(link.Actions) != 1 {
		t.Fatalf("裁剪后动作=%+v", link)
	}
	if action := link.Actions[0]; action.AttachmentID != "right" || action.Scope != 2 || action.Page != 5 || action.Boundary != (AnnotationBoundary{X: 40, Y: 30, Width: 8, Height: 8}) {
		t.Fatalf("未逐动作映射和裁剪：%+v", action)
	}
	// 另一个复合引用不能拿到上一次裁剪后的动作子集。
	other, ok := graphicActionLink(object.CTGraphicUnit, nil, nil)
	if !ok || len(other.Actions) != 2 || other.Actions[0].URI != "https://example.test/left" {
		t.Fatalf("资源被上次引用修改：%+v", other)
	}
}
