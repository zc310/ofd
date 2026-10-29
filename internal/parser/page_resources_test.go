package parser

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestPageResourceLocationsStayStableAcrossEvictions(t *testing.T) {
	input := buildPageResPackage(t, "First.xml", map[string]string{
		"Doc_0/Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><Pages>
  <Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/>
  <Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/>
  <Page ID="3" BaseLoc="Pages/Page_2/Content.xml"/>
</Pages></Document>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016">
  <PageRes>First.xml</PageRes><PageRes>Second.xml</PageRes><Content/>
</Page>`,
		"Doc_0/Pages/Page_1/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016">
  <PageRes>../Page_0/Second.xml</PageRes><Content/>
</Page>`,
		"Doc_0/Pages/Page_2/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016"><Content/></Page>`,
		"Doc_0/Pages/Page_0/First.xml":   `<Res xmlns="http://www.ofdspec.org/2016"/>`,
		"Doc_0/Pages/Page_0/Second.xml":  `<Res xmlns="http://www.ofdspec.org/2016"/>`,
	})
	ofd, err := NewOFD(input, Options{PageCacheCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	want := [][]models.StLoc{
		{"Doc_0/Pages/Page_0/First.xml", "Doc_0/Pages/Page_0/Second.xml"},
		{"Doc_0/Pages/Page_0/Second.xml"},
		nil,
	}
	doc := ofd.Documents[0]
	for round := 0; round < 4; round++ {
		for index, page := range doc.Pages {
			if err := page.EnsureLoaded(); err != nil {
				t.Fatal(err)
			}
			for read := 0; read < 2; read++ {
				if got := page.PageResourceLocations(); !slices.Equal(got, want[index]) {
					t.Fatalf("第 %d 轮页面 %d 的资源路径 = %v，期望 %v", round+1, index, got, want[index])
				}
			}
		}
	}
}

// GB/T 33190—2016 §7.3 表 2 规定 ST_Loc 的 / 表示包根节点；
// §7.7 表 12 规定 PageRes 的类型为 ST_Loc。
func TestAbsolutePageResourceResolvesFromPackageRoot(t *testing.T) {
	for _, test := range []struct {
		name     string
		resource string
		want     models.StLoc
	}{
		{name: "document_directory", resource: "/Doc_0/PageRes.xml", want: "/Doc_0/PageRes.xml"},
		{name: "shared_directory", resource: "/Shared/PageRes.xml", want: "/Shared/PageRes.xml"},
		{name: "normalized_path", resource: "/Shared/Res/../PageRes.xml", want: "/Shared/PageRes.xml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			entryPath := strings.TrimPrefix(test.want.String(), "/")
			entries := map[string]string{
				entryPath:                         `<Res xmlns="http://www.ofdspec.org/2016"><DrawParams><DrawParam ID="7" LineWidth="0.5"/></DrawParams></Res>`,
				"Doc_0/Pages/Page_0/" + entryPath: `<Res xmlns="http://www.ofdspec.org/2016"><DrawParams><DrawParam ID="9" LineWidth="9"/></DrawParams></Res>`,
			}
			doc := openPageResDocument(t, buildPageResPackage(t, test.resource, entries))
			if err := doc.Pages[0].EnsureLoaded(); err != nil {
				t.Fatal(err)
			}
			locations := doc.Pages[0].PageResourceLocations()
			if len(locations) != 1 || locations[0] != test.want {
				t.Fatalf("页面资源路径 = %v，期望 [%s]", locations, test.want)
			}
			if param := doc.GetDrawParam(7); param == nil || param.LineWidth != 0.5 {
				t.Fatalf("包根节点下的页面资源未加载: %+v", param)
			}
			if doc.GetDrawParam(9) != nil {
				t.Fatal("绝对路径错误加载了页面目录下的同名资源")
			}
		})
	}
}

func TestMissingAbsolutePageResourceDoesNotUsePageDirectory(t *testing.T) {
	entries := map[string]string{
		"Doc_0/Pages/Page_0/Shared/PageRes.xml": `<Res xmlns="http://www.ofdspec.org/2016"/>`,
	}
	doc := openPageResDocument(t, buildPageResPackage(t, "/Shared/PageRes.xml", entries))
	err := doc.Pages[0].EnsureLoaded()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("缺失绝对路径资源时的错误 = %v，期望文件不存在", err)
	}
	if !strings.Contains(err.Error(), "Shared/PageRes.xml") || strings.Contains(err.Error(), "Pages/Page_0") {
		t.Fatalf("错误未指向包根节点下的资源: %v", err)
	}
}

func TestSharedPageMediaPathsStayStableAfterReload(t *testing.T) {
	input := buildPageResPackage(t, "PageRes.xml", map[string]string{
		"Doc_0/Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><Pages>
  <Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/>
  <Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/>
</Pages></Document>`,
		"Doc_0/Pages/Page_1/Content.xml": `<Page xmlns="http://www.ofdspec.org/2016">
  <PageRes>../Page_0/PageRes.xml</PageRes><Content/>
</Page>`,
		"Doc_0/Pages/Page_0/PageRes.xml": `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Res"><MultiMedias>
  <MultiMedia ID="7" Type="Image"><MediaFile>image.png</MediaFile></MultiMedia>
  <MultiMedia ID="8" Type="Audio"><MediaFile>audio.mp3</MediaFile></MultiMedia>
  <MultiMedia ID="9" Type="Video"><MediaFile>/Doc_0/video.mp4</MediaFile></MultiMedia>
</MultiMedias></Res>`,
		"Doc_0/Pages/Page_0/Res/image.png": "image",
		"Doc_0/Pages/Page_0/Res/audio.mp3": "audio",
		"Doc_0/video.mp4":                  "video",
	})
	ofd, err := NewOFD(input, Options{PageCacheCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	wantPaths := map[models.StID]models.StLoc{
		7: "Doc_0/Pages/Page_0/Res/image.png",
		8: "Doc_0/Pages/Page_0/Res/audio.mp3",
		9: "/Doc_0/video.mp4",
	}
	previous := make(map[models.StID]*models.MultiMedia)
	for _, pageIndex := range []int{0, 1, 0} {
		if err := doc.Pages[pageIndex].EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
		for id, want := range wantPaths {
			if media := previous[id]; media != nil && media.MediaFile != want {
				t.Errorf("加载第 %d 页后，先前媒体 %d 的路径被修改为 %q", pageIndex, id, media.MediaFile)
			}
			media := doc.GetMedia(id)
			if media == nil {
				t.Fatalf("加载第 %d 页后媒体 %d 丢失", pageIndex, id)
			}
			if media.MediaFile != want {
				t.Errorf("加载第 %d 页后媒体 %d 的路径 = %q，期望 %q", pageIndex, id, media.MediaFile, want)
			}
			if _, err := doc.FileCache.Read(media.MediaFile.String()); err != nil {
				t.Errorf("加载第 %d 页后媒体 %d 不可读取: %v", pageIndex, id, err)
			}
			previous[id] = media
		}
	}
}

func TestPagesWithoutPageResourcesKeepDocumentResourcesAcrossEvictions(t *testing.T) {
	input := buildPageResPackage(t, "", map[string]string{
		"Doc_0/Document.xml": `<Document><CommonData><DocumentRes>Res.xml</DocumentRes></CommonData><Pages>
  <Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/>
  <Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/>
</Pages></Document>`,
		"Doc_0/Res.xml":                  `<Res><DrawParams><DrawParam ID="7" LineWidth="0.5"/></DrawParams></Res>`,
		"Doc_0/Pages/Page_0/Content.xml": `<Page><Content/></Page>`,
		"Doc_0/Pages/Page_1/Content.xml": `<Page><Content/></Page>`,
	})
	ofd, err := NewOFD(input, Options{PageCacheCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	for _, index := range []int{0, 1, 0} {
		if err := doc.Pages[index].EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
		if got := doc.GetDrawParam(7); got == nil || got.LineWidth != 0.5 {
			t.Fatalf("加载页面 %d 后文档资源丢失：%+v", index, got)
		}
	}
}

func TestPageCacheRestoresShadowedDocumentResources(t *testing.T) {
	input := buildPageResPackage(t, "PageRes.xml", map[string]string{
		"Doc_0/Document.xml": `<Document><CommonData><DocumentRes>Res.xml</DocumentRes></CommonData><Pages>
  <Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/><Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/>
</Pages></Document>`,
		"Doc_0/Res.xml": `<Res><DrawParams><DrawParam ID="1" LineWidth="1"/></DrawParams>
  <Fonts><Font ID="2" FontName="document"/></Fonts>
  <ColorSpaces><ColorSpace ID="3" Type="RGB"/></ColorSpaces>
  <MultiMedias><MultiMedia ID="4" Type="Image"><MediaFile>document.bin</MediaFile></MultiMedia></MultiMedias>
  <CompositeGraphicUnits><CompositeGraphicUnit ID="5" Width="5" Height="5"><Content/></CompositeGraphicUnit></CompositeGraphicUnits>
</Res>`,
		"Doc_0/Pages/Page_0/PageRes.xml": `<Res><DrawParams><DrawParam ID="1" LineWidth="9"/></DrawParams>
  <Fonts><Font ID="2" FontName="page"/></Fonts>
  <ColorSpaces><ColorSpace ID="3" Type="GRAY"/></ColorSpaces>
  <MultiMedias><MultiMedia ID="4" Type="Image"><MediaFile>page.bin</MediaFile></MultiMedia></MultiMedias>
  <CompositeGraphicUnits><CompositeGraphicUnit ID="5" Width="9" Height="9"><Content/></CompositeGraphicUnit></CompositeGraphicUnits>
</Res>`,
		"Doc_0/Pages/Page_1/Content.xml": `<Page><Content/></Page>`,
	})
	ofd, err := NewOFD(input, Options{PageCacheCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	if err := doc.Pages[0].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if got := doc.GetFont(2); got == nil || got.FontName != "page" {
		t.Fatalf("页面资源未覆盖文档字体: %+v", got)
	}
	if err := doc.Pages[1].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if got := doc.GetDrawParam(1); got == nil || got.LineWidth != 1 {
		t.Errorf("文档绘制参数未恢复: %+v", got)
	}
	if got := doc.GetFont(2); got == nil || got.FontName != "document" {
		t.Errorf("文档字体未恢复: %+v", got)
	}
	if got := doc.GetColorSpace(3); got == nil || got.Type != "RGB" {
		t.Errorf("文档颜色空间未恢复: %+v", got)
	}
	if got := doc.GetMedia(4); got == nil || got.MediaFile != "Doc_0/document.bin" {
		t.Errorf("文档媒体未恢复: %+v", got)
	}
	if got := doc.GetCompositeUnit(5); got == nil || got.Width != 5 {
		t.Errorf("文档复合图元未恢复: %+v", got)
	}
}
