package parser

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestTemplatePageResourcesAreIsolatedAndResolveRelativePaths(t *testing.T) {
	files := map[string]string{
		"OFD.xml":                    `<OFD><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc/Document.xml":           `<Document><CommonData><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="10" BaseLoc="Templates/One/Page.xml"/><TemplatePage ID="20" BaseLoc="Templates/Two/Page.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc/Res.xml":                `<Res><Fonts><Font ID="1" FontName="body"/></Fonts><DrawParams><DrawParam ID="2" LineWidth="0.5"/></DrawParams><MultiMedias><MultiMedia ID="3" Type="Image"><MediaFile>body.png</MediaFile></MultiMedia></MultiMedias></Res>`,
		"Doc/Page.xml":               `<Page><PageRes>PageRes.xml</PageRes></Page>`,
		"Doc/PageRes.xml":            `<Res><Fonts><Font ID="8" FontName="page-only"/></Fonts></Res>`,
		"Doc/Templates/One/Page.xml": `<Page><PageRes>Res.xml</PageRes></Page>`,
		"Doc/Templates/Two/Page.xml": `<Page><PageRes>/Shared/Res.xml</PageRes></Page>`,
		"Doc/Templates/One/Res.xml":  `<Res BaseLoc="Assets"><Fonts><Font ID="1" FontName="one"><FontFile>a.ttf</FontFile></Font></Fonts><DrawParams><DrawParam ID="4" Relative="2"/></DrawParams><MultiMedias><MultiMedia ID="3" Type="Image"><MediaFile>a.png</MediaFile></MultiMedia></MultiMedias><ColorSpaces><ColorSpace ID="5" Type="RGB" Profile="a.icc"/></ColorSpaces><CompositeGraphicUnits><CompositeGraphicUnit ID="6" Width="10" Height="20"><Content/></CompositeGraphicUnit></CompositeGraphicUnits></Res>`,
		"Shared/Res.xml":             `<Res BaseLoc="Assets"><Fonts><Font ID="1" FontName="two"><FontFile>b.ttf</FontFile></Font></Fonts><MultiMedias><MultiMedia ID="3" Type="Image"><MediaFile>b.png</MediaFile></MultiMedia></MultiMedias></Res>`,
	}
	ofd, err := NewOFD(parserTestArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	lease, err := doc.Pages[0].AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	_, one, err := doc.LoadTemplateResources(10)
	if err != nil {
		t.Fatal(err)
	}
	_, two, err := doc.LoadTemplateResources(20)
	if err != nil {
		t.Fatal(err)
	}
	if one == nil || two == nil || one == two {
		t.Fatal("模板没有独立资源视图")
	}
	if one.GetFont(1).FontName != "one" || two.GetFont(1).FontName != "two" || doc.GetFont(1).FontName != "body" {
		t.Fatal("模板字体串用或污染正文")
	}
	if one.GetFont(8) != nil || two.GetFont(8) != nil {
		t.Fatal("模板借用了页面专属资源")
	}
	if one.GetFont(1).FontFile != "Doc/Templates/One/Assets/a.ttf" || one.GetMedia(3).MediaFile != "Doc/Templates/One/Assets/a.png" || two.GetFont(1).FontFile != "/Shared/Assets/b.ttf" {
		t.Fatalf("模板资源文件路径基准错误：%s %s %s", one.GetFont(1).FontFile, one.GetMedia(3).MediaFile, two.GetFont(1).FontFile)
	}
	if one.GetColorSpace(5).Profile != "Doc/Templates/One/Assets/a.icc" || one.GetCompositeUnit(6).Height != 20 || one.GetDrawParam(4).LineWidth != .5 {
		t.Fatal("颜色空间、复合图元或跨资源继承丢失")
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			_, scope, loadErr := doc.LoadTemplateResources(10)
			if loadErr != nil || scope != one || scope.GetFont(1).FontName != "one" {
				t.Errorf("并发模板资源不一致：%v", loadErr)
			}
		})
	}
	workers.Wait()
}

func TestTemplateResourceErrorsPreserveMissingPathAndDoNotPublishPartialResources(t *testing.T) {
	doc := templateTestDocument(t, 1, `<Page><PageRes>missing.xml</PageRes></Page>`)
	for range 2 {
		content, scope, err := doc.LoadTemplateResources(1)
		if content != nil || scope != nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("损坏模板资源未保留错误：%p %p %v", content, scope, err)
		}
	}
}

func TestTemplateResourceViewSurvivesCacheEvictionWithoutLeakingIndexes(t *testing.T) {
	files := map[string]string{"OFD.xml": `<OFD><DocBody><DocRoot>Document.xml</DocRoot></DocBody></OFD>`, "Res.xml": `<Res><Fonts><Font ID="7" FontName="private"/></Fonts></Res>`}
	var document strings.Builder
	document.WriteString(`<Document><CommonData>`)
	for id := 1; id <= 33; id++ {
		fmt.Fprintf(&document, `<TemplatePage ID="%d" BaseLoc="T%d.xml"/>`, id, id)
		files[fmt.Sprintf("T%d.xml", id)] = `<Page><PageRes>Res.xml</PageRes></Page>`
	}
	document.WriteString(`</CommonData><Pages/></Document>`)
	files["Document.xml"] = document.String()
	ofd, err := NewOFD(parserTestArchive(t, files))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	_, scope, err := doc.LoadTemplateResources(1)
	if err != nil {
		t.Fatal(err)
	}
	for id := models.StID(2); id <= 33; id++ {
		if _, err := doc.LoadTemplate(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := doc.templateResources[1]; ok {
		t.Fatal("淘汰模板仍保留资源索引")
	}
	if scope.GetFont(7).FontName != "private" || doc.GetFont(7) != nil {
		t.Fatal("淘汰破坏了调用方视图或污染正文")
	}
}
