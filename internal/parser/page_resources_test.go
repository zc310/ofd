package parser

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
)

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
	ofd, err := NewOFDWithOptions(input, Options{PageCacheCapacity: 1})
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
