package canvas

import (
	"archive/zip"
	"bytes"
	"math"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/font"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/font/gofont/goregular"
)

func TestNativeGlyphReferencesRequireMatchingFontAndCmapAnchors(t *testing.T) {
	// 族的缓存别名故意与字体文件真实名称不同，不能拿别名确认 GID 身份。
	family := canvas.NewFontFamily("unrelated cache alias")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	reference := family.Face(12, canvas.Black)
	a, b := reference.Font.GlyphIndex('A'), reference.Font.GlyphIndex('B')
	for _, tc := range []struct {
		name, attrs string
		anchors     map[rune]uint16
		want        bool
	}{
		{"matching", `FontName="Go"`, map[rune]uint16{'A': a}, true},
		{"matching family", `FontName="F0" FamilyName="Go"`, map[rune]uint16{'A': a}, true},
		{"wrong font", `FontName="Tahoma"`, map[rune]uint16{'A': a}, false},
		{"cache alias is not font identity", `FontName="unrelated cache alias"`, map[rune]uint16{'A': a}, false},
		{"no anchors", `FontName="Go"`, nil, false},
		{"wrong anchor", `FontName="Go"`, map[rune]uint16{'A': b}, false},
		{"one wrong anchor", `FontName="Go"`, map[rune]uint16{'A': a, 'B': a}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			zw := zip.NewWriter(&buffer)
			for name, data := range map[string]string{
				"OFD.xml":          `<OFD><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
				"Doc/Document.xml": `<Document><CommonData><DocumentRes>Res.xml</DocumentRes></CommonData><Pages/></Document>`,
				"Doc/Res.xml":      `<Res><Fonts><Font ID="1" ` + tc.attrs + `/></Fonts></Res>`,
			} {
				w, err := zw.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte(data)); err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(buffer.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			fonts := NewFonts(ofd.Documents[0])
			defer fonts.Close()
			fonts.RegisterGlyphs(1, tc.anchors)
			object := models.TextObject{CtText: models.CtText{Font: 1, Size: 12 / 2.83465, TextCode: []models.TextCode{{Value: "A"}}, CGTransform: []models.CTCGTransform{
				{CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{int(b), int(a)}},
				{CodeCount: 1, Glyphs: models.StArrayI{int(reference.Font.NumGlyphs()), 0, -1, 65536}},
			}}}
			face := fonts.FaceObject(family, object, nil)
			address := string(fontrepair.GlyphRune(b))
			if got := face.GlyphIndex([]rune(address)[0]); (got == b) != tc.want {
				t.Fatalf("native reference = %d, want available=%v", got, tc.want)
			}
			if !tc.want {
				return
			}
			if got := face.GlyphIndex(fontrepair.GlyphRune(a)); got != 0 {
				t.Fatal("GlyphCount did not exclude the extra glyph")
			}
			if got := face.GlyphIndex(fontrepair.GlyphRune(reference.Font.NumGlyphs())); got != 0 {
				t.Fatal("accepted out-of-range glyph")
			}
			path := &canvas.Path{}
			if err := reference.Font.GlyphPath(path, b, 0, 0, 0, reference.MmPerEm, font.NoHinting); err != nil {
				t.Fatal(err)
			}
			if got := face.DirectPath(address); got == nil || got.Empty() {
				t.Fatal("native reference has no outline")
			} else if math.Abs(got.Bounds().W()-path.Bounds().W()) > 1e-9 {
				t.Fatal("native reference uses the wrong glyph outline")
			}
			wantWidth := float64(reference.Font.GlyphAdvance(b)) * reference.MmPerEm
			if math.Abs(face.TextWidth(address)-wantWidth) > 1e-9 {
				t.Fatalf("native advance = %g, want %g", face.TextWidth(address), wantWidth)
			}
			if reference.Font.GlyphIndex([]rune(address)[0]) != 0 {
				t.Fatal("native reference changed the shared system font cmap")
			}
		})
	}
}

func TestNativeSpaceGlyphKeepsEmptyOutlineAndAdvance(t *testing.T) {
	family := canvas.NewFontFamily("native space")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	base := family.Face(12, canvas.Black)
	gid := base.Font.GlyphIndex(' ')
	address := fontrepair.GlyphRune(gid)
	face := canvasFontFace{face: base, nativeGlyphs: map[rune]uint16{address: gid}}
	for _, path := range []*geom.Path{face.DirectPath(string(address)), face.ToPath(string(address))} {
		if path == nil || !path.Empty() {
			t.Fatal("native space must not become a .notdef box")
		}
	}
	if got := face.TextWidth(string(address)); got <= 0 {
		t.Fatalf("native space lost advance: %g", got)
	}
}
