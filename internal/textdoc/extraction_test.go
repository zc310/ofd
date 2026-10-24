package textdoc

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image"
	"image/png"
	"io"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// extractionDocument 在内存中构造带模板、批注、字体、图片及复合对象的最小 OFD，
// 便于精确验证提取结果，不依赖字体安装或外部样本文件。
func extractionDocument(t *testing.T, overrides map[string]string) (*parser.Document, []byte) {
	t.Helper()
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"OFD.xml":            `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml": `<Document><CommonData><MaxUnitID>100</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="10" BaseLoc="Template.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Annotations>Annotations.xml</Annotations></Document>`,
		"Doc_0/Res.xml": `<Res BaseLoc="Res"><Fonts><Font ID="20" FontName=" Demo Font "/><Font ID="21" FontName=" " FamilyName=" Family Name "/></Fonts><MultiMedias>
<MultiMedia ID="30" Type="Image" Format="PNG"><MediaFile>pixel.png</MediaFile></MultiMedia>
<MultiMedia ID="31" Type="Image" Format="SVG"><MediaFile>vector.svg</MediaFile></MultiMedia>
<MultiMedia ID="32" Type="Image" Format="PNG"><MediaFile>empty.png</MediaFile></MultiMedia>
<MultiMedia ID="33" Type="Image" Format="PNG"><MediaFile>missing.png</MediaFile></MultiMedia>
</MultiMedias><CompositeGraphicUnits>
<CompositeGraphicUnit ID="40" Width="20" Height="20"><Content><TextObject ID="41" Font="20" Size="4" Boundary="0 0 10 5"><TextCode>composite</TextCode></TextObject><ImageObject ID="42" ResourceID="30" Boundary="10 15 3 4"/></Content></CompositeGraphicUnit>
<CompositeGraphicUnit ID="50" Width="20" Height="20"><Content><CompositeObject ID="51" ResourceID="50" Boundary="0 0 20 20"/></Content></CompositeGraphicUnit>
</CompositeGraphicUnits></Res>`,
		"Doc_0/Template.xml": `<Page><Content><Layer ID="11"><TextObject ID="12" Font="20" Size="4" Boundary="0 0 10 5"><TextCode>template</TextCode></TextObject><ImageObject ID="13" ResourceID="30" Boundary="5 2 3 4"/></Layer></Content></Page>`,
		"Doc_0/Page.xml": `<Page><Area><PhysicalBox>0 0 100 200</PhysicalBox></Area><Template TemplateID="10"/><Content>
<Layer ID="1"><TextObject ID="2" Font="20" Size="4" Weight="700" Italic="true" Boundary="10 20 30 5" CTM="2 0 0 3 4 5"><TextCode X="1" Y="2">Hello</TextCode><TextCode> world</TextCode></TextObject>
<TextObject ID="3" Font="20" Size="4" Visible="false"><TextCode>hidden</TextCode></TextObject>
<TextObject ID="4" Font="20" Size="4" Fill="false"><TextCode>unfilled</TextCode></TextObject><TextObject ID="5" Font="20" Size="4"><TextCode/></TextObject>
<PageBlock ID="6"><TextObject ID="7" Font="21" Size="4" Boundary="1 2 0 0"><TextCode>nested</TextCode></TextObject><ImageObject ID="8" ResourceID="30" Boundary="20 10 3 4"/></PageBlock>
<CompositeObject ID="9" ResourceID="40" Boundary="0 0 20 20"/><CompositeObject ID="14" ResourceID="40" Visible="false"/><CompositeObject ID="15" ResourceID="999"/>
<ImageObject ID="16" ResourceID="30" Boundary="40 30 12 8"/><ImageObject ID="17" ResourceID="999" Substitution="30" Boundary="30 30 3 4"/>
<ImageObject ID="18" ResourceID="30" Visible="false" Boundary="0 0 3 4"/><ImageObject ID="19" ResourceID="31" Boundary="0 0 3 4"/>
<ImageObject ID="22" ResourceID="32" Boundary="0 0 3 4"/><ImageObject ID="23" ResourceID="33" Boundary="0 0 3 4"/>
<ImageObject ID="24" ResourceID="999" Boundary="0 0 3 4"/><ImageObject ID="25" ResourceID="999" Substitution="998" Boundary="0 0 3 4"/>
</Layer><Layer ID="26" Type="Background"><TextObject ID="27" Font="20" Size="4"><TextCode>background</TextCode></TextObject><ImageObject ID="28" ResourceID="30" Boundary="5 9 3 4"/></Layer></Content></Page>`,
		"Doc_0/Annotations.xml": `<Annotations><Page PageID="1"><FileLoc>Annot.xml</FileLoc></Page></Annotations>`,
		"Doc_0/Annot.xml":       `<PageAnnot><Annot ID="a1" Type="Watermark"><Appearance><TextObject ID="60" Font="20" Size="4"><TextCode>annotation</TextCode></TextObject><ImageObject ID="61" ResourceID="30" Boundary="5 40 3 4"/></Appearance></Annot><Annot ID="a2" Visible="false"><Appearance><TextObject ID="62" Font="20" Size="4"><TextCode>hidden annotation</TextCode></TextObject></Appearance></Annot><Annot ID="a3"/></PageAnnot>`,
		"Doc_0/Res/pixel.png":   picture.String(),
		"Doc_0/Res/vector.svg":  `<svg/>`,
		"Doc_0/Res/empty.png":   "",
	}
	for name, value := range overrides {
		files[name] = value
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ofd.Close(); err != nil {
			t.Error(err)
		}
	})
	if len(ofd.Documents) != 1 || len(ofd.Documents[0].Pages) != 1 {
		t.Fatal("测试 OFD 应包含一个文档体和一页")
	}
	return ofd.Documents[0], picture.Bytes()
}

func TestExtractPagePreservesOrderSourcesAndTextFormatting(t *testing.T) {
	doc, _ := extractionDocument(t, nil)
	page, err := ExtractPage(t.Context(), doc, doc.Pages[0])
	if err != nil {
		t.Fatal(err)
	}
	if page.Width != 100 || page.Height != 200 {
		t.Fatalf("页面尺寸应为 100x200，实际 %gx%g", page.Width, page.Height)
	}
	var texts []string
	for _, entry := range page.Entries {
		texts = append(texts, entry.Text)
	}
	want := []string{"template", "background", "Hello world", "nested", "composite", "annotation"}
	if !slices.Equal(texts, want) {
		t.Fatalf("文字顺序或可见性错误，实际 %q，期望 %q", texts, want)
	}
	for i, entry := range page.Entries {
		source := EntrySourcePage
		if i == 0 {
			source = EntrySourceTemplate
		}
		if i == len(page.Entries)-1 {
			source = EntrySourceAnnotation
		}
		if entry.Source != source {
			t.Errorf("条目 %q 来源=%s，期望 %s", entry.Text, entry.Source, source)
		}
	}
	entry := page.Entries[2]
	if entry.X != 16 || entry.Y != 31 || entry.Width != 30 || entry.Size != 4 || entry.FontName != "Demo Font" || !entry.Bold || !entry.Italic {
		t.Fatalf("文字位置、宽度或格式丢失: %+v", entry)
	}
	if entry := page.Entries[3]; entry.FontName != "Family Name" || entry.Width != 12 {
		t.Fatalf("字体族或估算宽度回退错误: %+v", entry)
	}
}

func TestExtractImagesUsesReadingOrderSubstitutionAndLimits(t *testing.T) {
	doc, picture := extractionDocument(t, nil)
	for _, limit := range []int{0, -1, 2} {
		images, err := ExtractImages(t.Context(), doc, doc.Pages[0], limit)
		if err != nil {
			t.Fatal(err)
		}
		want := [][2]float64{{5, 2}, {5, 9}, {20, 10}, {10, 15}, {30, 30}, {40, 30}, {5, 40}}
		if limit > 0 {
			want = want[:limit]
		}
		if len(images) != len(want) {
			t.Fatalf("limit=%d，图片数量=%d，期望 %d", limit, len(images), len(want))
		}
		for i, image := range images {
			if image.X != want[i][0] || image.Y != want[i][1] || image.Extension != "png" || !bytes.Equal(image.Data, picture) {
				t.Errorf("第 %d 张图片位置、格式或原始数据不匹配: %+v", i, image)
			}
		}
	}
	if image, ok := imageOf(doc, nil); ok || len(image.Data) != 0 {
		t.Fatal("空图片对象不应被提取")
	}
}

func TestExtractPageAndImagesBoundCompositeCycles(t *testing.T) {
	doc, _ := extractionDocument(t, nil)
	var content models.PageContent
	if err := xml.Unmarshal([]byte(`<Page><Content><Layer ID="1"><CompositeObject ID="2" ResourceID="50"/></Layer></Content></Page>`), &content); err != nil {
		t.Fatal(err)
	}
	page := parser.NewPage(content)
	text, err := ExtractPage(t.Context(), doc, page)
	if err != nil || len(text.Entries) != 0 {
		t.Fatalf("循环复合对象应有界结束，实际 %+v, %v", text, err)
	}
	images, err := ExtractImages(t.Context(), doc, page, 0)
	if err != nil || len(images) != 0 {
		t.Fatalf("循环复合对象不应生成图片，实际 %+v, %v", images, err)
	}
}

// GB/T 33190—2016 §8.5 表 34 规定 Visible 缺省为 true；
// 显式隐藏的复合图元不应将内部图片带入导出结果。
func TestExtractImagesSkipsInvisibleCompositeObjects(t *testing.T) {
	doc, _ := extractionDocument(t, nil)
	for _, test := range []struct {
		visibility string
		want       int
	}{{"", 1}, {`Visible="true"`, 1}, {`Visible="false"`, 0}} {
		t.Run(test.visibility, func(t *testing.T) {
			var content models.PageContent
			source := `<Page><Content><Layer ID="1"><CompositeObject ID="2" ResourceID="40" ` + test.visibility + `/></Layer></Content></Page>`
			if err := xml.Unmarshal([]byte(source), &content); err != nil {
				t.Fatal(err)
			}
			images, err := ExtractImages(t.Context(), doc, parser.NewPage(content), 0)
			if err != nil || len(images) != test.want {
				t.Fatalf("复合对象可见性应控制其图片导出，实际 %d 张, %v，期望 %d 张", len(images), err, test.want)
			}
		})
	}
}

func TestCollectUsesGlobalNonNilPageIndexesAndPreservesOwners(t *testing.T) {
	first, second, third := parser.NewPage(models.PageContent{}), parser.NewPage(models.PageContent{}), parser.NewPage(models.PageContent{})
	doc1 := &parser.Document{Pages: []*parser.Page{nil, first, second}}
	doc2 := &parser.Document{Pages: []*parser.Page{third, nil}}
	documents := []*parser.Document{nil, doc1, doc2}
	if got := Count(documents); got != 3 {
		t.Fatalf("非空页面总数=%d，期望 3", got)
	}
	for _, end := range []int{3, 99} {
		pages, err := Collect(t.Context(), documents, 1, end)
		if err != nil || len(pages) != 2 {
			t.Fatalf("跨文档页范围错误: %+v, %v", pages, err)
		}
		if pages[0].Source != second || pages[0].Owner != doc1 || pages[1].Source != third || pages[1].Owner != doc2 {
			t.Fatal("全局页序或来源文档不正确")
		}
		if pages[0].Width != 210 || pages[0].Height != 297 {
			t.Fatal("缺省页面应使用 A4 尺寸")
		}
	}
	if pages, err := Collect(t.Context(), documents, 1, 1); err != nil || len(pages) != 0 {
		t.Fatalf("空范围应无页面: %+v, %v", pages, err)
	}
}

func TestExtractionPreservesCancellationAndPageLoadErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Collect(ctx, nil, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ExtractPage(ctx, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ExtractImages(ctx, nil, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if page, err := ExtractPage(t.Context(), nil, nil); err != nil || page.Width != 210 || page.Height != 297 {
		t.Fatalf("空页面应返回默认尺寸: %+v, %v", page, err)
	}
	if images, err := ExtractImages(t.Context(), nil, parser.NewPage(models.PageContent{}), 0); err != nil || len(images) != 0 {
		t.Fatalf("空文档不应提取图片: %+v, %v", images, err)
	}
	doc, _ := extractionDocument(t, map[string]string{"Doc_0/Page.xml": `<Page><Content>`})
	for _, extract := range []func() error{
		func() error { _, err := ExtractPage(t.Context(), doc, doc.Pages[0]); return err },
		func() error { _, err := ExtractImages(t.Context(), doc, doc.Pages[0], 0); return err },
		func() error { _, err := Collect(t.Context(), []*parser.Document{doc}, 0, 1); return err },
	} {
		if err := extract(); !errors.Is(err, io.EOF) {
			t.Fatalf("应保留截断页面的读取错误，实际 %v", err)
		}
	}
}

func TestPageWithoutSourcePreservesInputAndMetadata(t *testing.T) {
	source, owner := &parser.Page{}, &parser.Document{}
	page := Page{Width: 100, Height: 200, Source: source, Owner: owner, Entries: []Entry{{Text: "body"}, {Text: "watermark", Source: EntrySourceAnnotation}, {Text: "header", Source: EntrySourceTemplate}}}
	before := slices.Clone(page.Entries)
	filtered := page.WithoutSource(EntrySourceAnnotation)
	if len(filtered.Entries) != 2 || filtered.Entries[0].Text != "body" || filtered.Entries[1].Text != "header" {
		t.Fatalf("来源过滤错误: %+v", filtered)
	}
	if filtered.Width != 100 || filtered.Height != 200 || filtered.Source != source || filtered.Owner != owner {
		t.Fatal("来源过滤不应丢失页面元数据")
	}
	filtered.Entries[0].Text = "changed"
	if !reflect.DeepEqual(page.Entries, before) {
		t.Fatal("过滤后的页面不应共享可修改的条目切片")
	}
	if got := (Page{}).WithoutSource(EntrySourceAnnotation); len(got.Entries) != 0 {
		t.Fatal("空页面过滤错误")
	}
	if got := page.WithoutSource(EntrySource(99)); !reflect.DeepEqual(got, page) {
		t.Fatal("不存在的来源不应改变页面")
	}
	if EntrySourceTemplate.String() != "template" || EntrySourceAnnotation.String() != "annotation" || EntrySourcePage.String() != "page" {
		t.Fatal("来源名称不正确")
	}
}

func TestTextWidthFallbackAndRasterFormatFiltering(t *testing.T) {
	var object models.TextObject
	object.Size = math.NaN()
	if got := textEntryWidth(object, "中A"); got != 9 {
		t.Fatalf("无效字号应按默认字号估算宽度，实际 %g", got)
	}
	object.Font = 20
	if got := textEntryFontName(nil, object); got != "" {
		t.Fatal("无文档时不应解析出字体")
	}
	if got := textEntryFontName(&parser.Document{}, object); got != "" {
		t.Fatal("未声明的字体应为空")
	}
	for _, test := range []struct{ format, path, want string }{
		{"PNG", "Res/image.PNG", "png"}, {"JPEG", "Res/photo.JPEG", "jpeg"}, {"", "Res/photo.jpg", "jpg"},
		{"SVG", "Res/image.png", ""}, {"PNG", "Res/image.svg", ""}, {"", "Res/no-extension", ""},
	} {
		got, ok := rasterExtension(test.format, models.StLoc(test.path))
		if got != test.want || ok != (test.want != "") {
			t.Errorf("图片格式过滤 %q/%q = %q, %t", test.format, test.path, got, ok)
		}
	}
	if !strings.Contains(EnsureUTF8("\xff"), "�") {
		t.Fatal("非法编码应替换为可输出字符")
	}
}
