package reader

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/zip"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func TestTemplateFontsAndMediaDoNotBorrowBodyResourceIDs(t *testing.T) {
	files := map[string][]byte{
		"OFD.xml":          []byte(`<OFD><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc/Document.xml": []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 40 40</PhysicalBox></PageArea><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="10" BaseLoc="T/Page.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`),
		"Doc/Page.xml":     []byte(`<Page><Template TemplateID="10"/><Content><Layer ID="2"><TextObject ID="3" Boundary="0 20 30 10" Font="9" Size="3"><TextCode X="0" Y="3">iiii</TextCode></TextObject></Layer></Content></Page>`),
		"Doc/T/Page.xml":   []byte(`<Page><PageRes>Res.xml</PageRes><Content><Layer ID="4"><TextObject ID="5" Boundary="0 0 30 10" Font="9" Size="3"><TextCode X="0" Y="3">iiii</TextCode></TextObject><PathObject ID="6" Boundary="0 10 10 10"><Actions><Action Event="CLICK"><Sound ResourceID="7"/></Action></Actions><AbbreviatedData>M 0 0 L 10 10</AbbreviatedData></PathObject></Layer></Content></Page>`),
		"Doc/Res.xml":      []byte(`<Res><Fonts><Font ID="9" FontName="Body"><FontFile>font.ttf</FontFile></Font></Fonts><MultiMedias><MultiMedia ID="7" Type="Audio"><MediaFile>audio.bin</MediaFile></MultiMedia></MultiMedias></Res>`),
		"Doc/T/Res.xml":    []byte(`<Res><Fonts><Font ID="9" FontName="Template"><FontFile>font.ttf</FontFile></Font></Fonts><MultiMedias><MultiMedia ID="7" Type="Audio"><MediaFile>audio.bin</MediaFile></MultiMedia></MultiMedias></Res>`),
		"Doc/font.ttf":     goregular.TTF, "Doc/T/font.ttf": gomono.TTF, "Doc/audio.bin": []byte("body"), "Doc/T/audio.bin": []byte("template"),
	}
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, value := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	runs, err := r.Text(0)
	if err != nil || len(runs) != 2 {
		t.Fatalf("文字=%+v %v", runs, err)
	}
	if runs[0].TemplateID != 10 || runs[1].TemplateID != 0 || runs[0].FontFamily == runs[1].FontFamily {
		t.Fatalf("模板文字层字体串用：%+v", runs)
	}
	fonts, err := r.Fonts()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, font := range fonts {
		seen[font.Family] = true
	}
	if !seen[runs[0].FontFamily] || !seen[runs[1].FontFamily] {
		t.Fatalf("浏览器字体未提供：%v", seen)
	}
	list, err := r.FontList()
	if err != nil || len(list) != 2 || list[0].TemplateID != 0 || list[1].TemplateID != 10 {
		t.Fatalf("模板字体目录未隔离：%+v %v", list, err)
	}
	for _, templateID := range []uint64{0, 10} {
		usage, usageErr := r.FontUsage(FontRef{Scope: 0, TemplateID: templateID, ID: 9}, FontUsageOptions{})
		if usageErr != nil || len(usage.Pages) != 1 || usage.Pages[0] != 0 {
			t.Fatalf("字体使用页丢失：%+v %v", usage, usageErr)
		}
	}
	missing, err := r.FontUsage(FontRef{Scope: 0, TemplateID: 20, ID: 9}, FontUsageOptions{})
	if err != nil || len(missing.Pages) != 0 {
		t.Fatalf("不存在的模板借用了正文字体：%+v %v", missing, err)
	}
	report, err := r.FontUsageAll(FontUsageOptions{})
	if err != nil || len(report.Fonts) != 2 || report.Fonts[0].TemplateID != 0 || report.Fonts[1].TemplateID != 10 {
		t.Fatalf("批量字体使用页混淆：%+v %v", report, err)
	}
	links, err := r.PageLinksForPage(0)
	if err != nil || len(links) != 1 || links[0].TemplateID != 10 {
		t.Fatalf("模板媒体动作=%+v %v", links, err)
	}
	media, err := r.TemplateMediaData(0, links[0].TemplateID, 7, 0)
	if err != nil || string(media) != "template" {
		t.Fatalf("模板媒体读取=%q %v", media, err)
	}
	media, err = r.MediaData(0, 7, 0)
	if err != nil || string(media) != "body" {
		t.Fatalf("正文媒体读取=%q %v", media, err)
	}
}
