package webreader

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/zip"
)

func pageResourcePackage(t *testing.T) []byte {
	t.Helper()
	entries := map[string]string{
		"OFD.xml": `<OFD Version="1.0"><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea></CommonData><Pages>
  <Page ID="1" BaseLoc="Pages/P0/Content.xml"/><Page ID="2" BaseLoc="Pages/P1/Content.xml"/>
</Pages></Document>`,
		"Doc/Pages/P0/Content.xml": `<Page><PageRes>Res.xml</PageRes><Content/></Page>`,
		"Doc/Pages/P1/Content.xml": `<Page><PageRes>Res.xml</PageRes><Content/></Page>`,
		"Doc/Pages/P0/Res.xml": `<Res><Fonts><Font ID="10" FontName="Page Zero"><FontFile>f0.ttf</FontFile></Font></Fonts><MultiMedias>
  <MultiMedia ID="11" Type="Image" Format="BIN"><MediaFile>m0.bin</MediaFile></MultiMedia>
</MultiMedias></Res>`,
		"Doc/Pages/P1/Res.xml": `<Res><Fonts><Font ID="20" FontName="Page One"><FontFile>f1.ttf</FontFile></Font></Fonts><MultiMedias>
  <MultiMedia ID="21" Type="Audio" Format="BIN"><MediaFile>m1.bin</MediaFile></MultiMedia>
</MultiMedias></Res>`,
		"Doc/Pages/P0/f0.ttf": "font-zero",
		"Doc/Pages/P1/f1.ttf": "font-one",
		"Doc/Pages/P0/m0.bin": "media-zero",
		"Doc/Pages/P1/m1.bin": "media-one",
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

func TestPageResourcesAreStableAcrossCacheEvictions(t *testing.T) {
	r, err := Open(pageResourcePackage(t), OpenOptions{PageCacheCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	fontList, err := r.FontList()
	if err != nil {
		t.Fatal(err)
	}
	if len(fontList) != 2 || fontList[0].ID != 10 || fontList[1].ID != 20 {
		t.Fatalf("页面字体目录 = %+v，期望包含 10、20", fontList)
	}
	fonts, err := r.Fonts()
	if err != nil {
		t.Fatal(err)
	}
	if len(fonts) != 2 || string(fonts[0].Data) != "font-zero" || string(fonts[1].Data) != "font-one" {
		t.Fatalf("页面字体数据不完整: %+v", fonts)
	}
	media, err := r.Media()
	if err != nil {
		t.Fatal(err)
	}
	if len(media) != 2 || media[0].ID != 21 || media[1].ID != 11 {
		t.Fatalf("页面媒体目录 = %+v，期望包含 11、21", media)
	}
	stats, err := r.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Fonts != 2 || stats.Media != 2 {
		t.Fatalf("资源统计 = %+v，期望字体和媒体均为 2", stats)
	}

	// 目录建立后再次交替加载页面，确保底层动态索引发生淘汰也不影响读取。
	for _, page := range []int{0, 1, 0, 1} {
		if err := r.ofd.Documents[0].Pages[page].EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[uint64]string{11: "media-zero", 21: "media-one"} {
		data, err := r.MediaData(0, id, 0)
		if err != nil {
			t.Fatalf("读取媒体 %d 失败: %v", id, err)
		}
		if string(data) != want {
			t.Errorf("媒体 %d 数据 = %q，期望 %q", id, data, want)
		}
	}
}
