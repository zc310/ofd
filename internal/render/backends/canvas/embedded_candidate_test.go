package canvas

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/parser"
	"golang.org/x/image/font/gofont/goregular"
)

func TestEmbeddedCandidateDoesNotApplyRequestedResourceGlyphMappings(t *testing.T) {
	base := canvas.NewFontFamily("candidate-reference")
	if err := base.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	baseFace := base.Face(12, canvas.Black)
	wantA, differentGlyph := baseFace.Font.GlyphIndex('A'), baseFace.Font.GlyphIndex('B')
	if wantA == 0 || differentGlyph == 0 || wantA == differentGlyph {
		t.Fatal("测试字体的 A/B 必须指向不同的非空字形")
	}
	for _, tc := range []struct {
		name, candidateAttrs string
		primary              []byte
		want                 uint16
	}{
		{"declared_font_uses_own_mapping", `FontName="SameName"`, goregular.TTF, differentGlyph},
		{"missing_font_borrows_same_name", `FontName="SameName"`, nil, wantA},
		{"broken_font_borrows_same_name", `FontName="SameName"`, []byte("broken font"), wantA},
		{"missing_font_borrows_same_family", `FontName="OtherSubset" FamilyName="SameFamily"`, nil, wantA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{
				"OFD.xml":            []byte(`<OFD><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`),
				"Doc_0/Document.xml": []byte(`<Document><CommonData><DocumentRes>Res.xml</DocumentRes></CommonData><Pages/></Document>`),
				"Doc_0/Res.xml": []byte(`<Res><Fonts>
<Font ID="1" FontName="SameName" FamilyName="SameFamily"><FontFile>primary.ttf</FontFile></Font>
<Font ID="2" ` + tc.candidateAttrs + `><FontFile>candidate.ttf</FontFile></Font>
</Fonts></Res>`),
				"Doc_0/candidate.ttf": goregular.TTF,
			}
			if tc.primary != nil {
				files["Doc_0/primary.ttf"] = tc.primary
			}
			var buffer bytes.Buffer
			archive := zip.NewWriter(&buffer)
			for name, data := range files {
				entry, err := archive.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(buffer.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			fonts := NewFonts(ofd.Documents[0])
			defer fonts.Close()
			// 故意让资源 A 的 'A' 指向另一字形，验证这份映射不跨资源套用。
			fonts.RegisterGlyphs(1, map[rune]uint16{'A': differentGlyph})
			family, err := fonts.LoadFont(1)
			if err != nil {
				t.Fatal(err)
			}
			if !fonts.HasLoadedEmbeddedFont(1) {
				t.Fatal("未通过声明字体或同名内嵌候选加载")
			}
			face := family.(*canvas.FontFamily).Face(12, canvas.Black)
			if got := face.Font.GlyphIndex('A'); got != tc.want {
				t.Fatalf("A 对应字形 = %d，期望 %d", got, tc.want)
			}
			if path, _ := face.ToPath("A"); path == nil || path.Empty() {
				t.Fatal("内嵌字体缺少可绘制的 A 轮廓")
			}
		})
	}
}
