package canvas

import (
	"bytes"
	"encoding/binary"
	"testing"

	fontfile "github.com/zc310/ofd/internal/thirdparty/font"
	"golang.org/x/image/font/gofont/goregular"
)

// A and B have opposite outer windings within the same TTC face.
// Put holes first to prevent guessing the fill direction from the first
// contour or the font format.
func mixedWindingFontCollection(t *testing.T) []byte {
	t.Helper()
	f, err := fontfile.ParseSFNT(bytes.Clone(goregular.TTF), 0)
	if err != nil {
		t.Fatal(err)
	}
	be := binary.BigEndian
	glyf := []byte{}
	loca := make([]byte, 4*(int(f.Maxp.NumGlyphs)+1))
	for id := range f.Maxp.NumGlyphs {
		be.PutUint32(loca[4*int(id):], uint32(len(glyf)))
		if id == f.GlyphIndex(' ') {
			// SimSun uses a single off-curve point for its empty space glyph.
			// Keep it away from the origin to catch accidental baseline segments.
			glyph := make([]byte, 20)
			for offset, value := range map[int]uint16{0: 1, 2: 80, 4: 100, 6: 80, 8: 100, 15: 80, 17: 100} {
				be.PutUint16(glyph[offset:], value)
			}
			glyf = append(glyf, glyph...)
			continue
		}
		if id != f.GlyphIndex('A') && id != f.GlyphIndex('B') &&
			id != f.GlyphIndex('C') && id != f.GlyphIndex('D') {
			continue
		}
		points := [][2]int16{
			{400, 400}, {600, 400}, {600, 600}, {400, 600},
			{100, 100}, {100, 900}, {900, 900}, {900, 100},
		}
		if id == f.GlyphIndex('B') {
			for start := 0; start < len(points); start += 4 {
				points[start+1], points[start+3] = points[start+3], points[start+1]
			}
		}
		xMax := uint16(900)
		if id == f.GlyphIndex('C') || id == f.GlyphIndex('D') {
			// 同一字形内放置相反方向的两个外轮廓，各自携带反向孔洞；孔洞仍排在外轮廓之前。
			points = [][2]int16{
				{200, 400}, {300, 400}, {300, 600}, {200, 600},
				{100, 100}, {100, 900}, {400, 900}, {400, 100},
				{700, 400}, {700, 600}, {800, 600}, {800, 400},
				{600, 100}, {900, 100}, {900, 900}, {600, 900},
			}
			if id == f.GlyphIndex('D') {
				// C 的面积恰好抵消，D 的右外轮廓更大，覆盖非零面积和的情形。
				points[13][0], points[14][0] = 1000, 1000
				xMax = 1000
			}
		}
		contours := len(points) / 4
		headerSize := 12 + 2*contours
		glyph := make([]byte, headerSize+len(points)*5)
		be.PutUint16(glyph, uint16(contours))
		for i, value := range []uint16{100, 100, xMax, 900} {
			be.PutUint16(glyph[2+i*2:], value)
		}
		for i := range contours {
			be.PutUint16(glyph[10+2*i:], uint16(4*i+3))
		}
		x, y := int16(0), int16(0)
		for i, point := range points {
			glyph[headerSize+i] = 1 // on-curve, with signed 16-bit coordinate deltas
			be.PutUint16(glyph[headerSize+len(points)+2*i:], uint16(point[0]-x))
			be.PutUint16(glyph[headerSize+3*len(points)+2*i:], uint16(point[1]-y))
			x, y = point[0], point[1]
		}
		glyf = append(glyf, glyph...)
	}
	be.PutUint32(loca[4*int(f.Maxp.NumGlyphs):], uint32(len(glyf)))
	f.Tables["glyf"], f.Tables["loca"] = glyf, loca
	be.PutUint16(f.Tables["head"][50:], 1)
	data := f.Write()
	collection := make([]byte, 20+len(data))
	copy(collection, "ttcf")
	be.PutUint32(collection[4:], 0x00010000)
	be.PutUint32(collection[8:], 2)
	be.PutUint32(collection[12:], 20)
	be.PutUint32(collection[16:], 20)
	copy(collection[20:], data)
	for i := range int(be.Uint16(data[4:])) {
		record := collection[20+12+16*i:]
		be.PutUint32(record[8:], be.Uint32(record[8:])+20)
	}
	return collection
}

func TestSingleOffCurveSpaceDoesNotAddBaselineWhenTranslatedOrEmboldened(t *testing.T) {
	family := NewFontFamily(t.Name())
	if err := family.LoadFont(mixedWindingFontCollection(t), 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	for _, style := range []FontStyle{FontRegular, FontBold, FontBold | FontItalic} {
		t.Run(style.String(), func(t *testing.T) {
			face := family.Face(12*ptPerMm, Black, style)
			reference, width := face.ToPath("AB")
			for _, suffix := range []string{" ", "   "} {
				got, advance := face.ToPath("AB" + suffix)
				if got.String() != reference.String() {
					t.Fatalf("尾部空格产生了可见轮廓：样式 %v，后缀 %q", style, suffix)
				}
				if advance <= width {
					t.Fatal("空格不可见，但仍应保留步进宽度")
				}
			}
			path := &Path{}
			gid := face.Font.GlyphIndex(' ')
			if err := face.Font.GlyphPath(path, gid, 0, 48, 6, face.MmPerEm, fontfile.NoHinting); err != nil {
				t.Fatal(err)
			}
			if !path.Empty() {
				t.Fatalf("平移后的单点空格错误连接到原点：%s", path)
			}
		})
	}
}

func TestFauxBoldExpandsBothGlyphWindingsInSameTTCFace(t *testing.T) {
	family := NewFontFamily(t.Name())
	if err := family.LoadFont(mixedWindingFontCollection(t), 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	regular := family.Face(12*ptPerMm, Black, FontRegular)
	bold := family.Face(12*ptPerMm, Black, FontBold)
	for _, value := range []string{"A", "B", "AB", "BA", " A B "} {
		t.Run(value, func(t *testing.T) {
			before, width := regular.ToPath(value)
			got, boldWidth := bold.ToPath(value)
			if width != boldWidth {
				t.Fatal("合成粗体改变了文字步进宽度")
			}
			wantBounds, gotBounds := before.Bounds(), got.Bounds()
			if gotBounds.X0 >= wantBounds.X0 || gotBounds.Y0 >= wantBounds.Y0 ||
				gotBounds.X1 <= wantBounds.X1 || gotBounds.Y1 <= wantBounds.Y1 {
				t.Fatalf("合成粗体未向外扩张：常规 %v，粗体 %v", wantBounds, gotBounds)
			}
			// Preserve the outer fill and hole center, but shrink the hole.
			x := wantBounds.X0 + 400*regular.MmPerEm
			y := 500 * regular.MmPerEm
			if got.ContainsPoint(x, y, NonZero) {
				t.Fatal("合成粗体填满了孔洞")
			}
			x = wantBounds.X0 + 310*regular.MmPerEm
			if !got.ContainsPoint(x, y, NonZero) {
				t.Fatal("合成粗体未收缩孔洞")
			}
			after, _ := regular.ToPath(value)
			if before.String() != after.String() {
				t.Fatal("合成粗体修改了共享的原始轮廓")
			}
		})
	}
}

func TestFauxBoldExpandsOppositeOuterContoursAndShrinksTheirHoles(t *testing.T) {
	family := NewFontFamily(t.Name())
	if err := family.LoadFont(mixedWindingFontCollection(t), 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	defer family.Destroy()
	regular := family.Face(12*ptPerMm, Black, FontRegular)
	bold := family.Face(12*ptPerMm, Black, FontBold)
	for _, value := range []string{"C", "D"} {
		t.Run(value, func(t *testing.T) {
			before, width := regular.ToPath(value)
			after, boldWidth := bold.ToPath(value)
			if width != boldWidth {
				t.Fatal("加粗改变了字形推进量")
			}
			right := 905.0
			if value == "D" {
				right = 1005
			}
			for _, x := range []float64{95, right} {
				x, y := x*regular.MmPerEm, 500*regular.MmPerEm
				if before.ContainsPoint(x, y, NonZero) || !after.ContainsPoint(x, y, NonZero) {
					t.Fatal("方向相反的外轮廓未同时向外扩张")
				}
			}
			for _, x := range []float64{205, 705} {
				x, y := x*regular.MmPerEm, 500*regular.MmPerEm
				if before.ContainsPoint(x, y, NonZero) || !after.ContainsPoint(x, y, NonZero) {
					t.Fatal("孔洞没有随加粗收缩")
				}
			}
			for _, x := range []float64{250, 750} {
				if after.ContainsPoint(x*regular.MmPerEm, 500*regular.MmPerEm, NonZero) {
					t.Fatal("加粗填满了孔洞中心")
				}
			}
			again, _ := regular.ToPath(value)
			if before.String() != again.String() {
				t.Fatal("加粗改写了原始轮廓")
			}
		})
	}
}

func TestEmboldenGlyphPathPreservesNestedIslandsAndHoles(t *testing.T) {
	// 从内向外乱序排列孔洞、外部孔洞、岛和外轮廓，不能靠轮廓顺序猜测填充关系。
	path := MustParseSVGPath("M9 9L11 9L11 11L9 11z M3 3L17 3L17 17L3 17z M6 6L6 14L14 14L14 6z M0 0L0 20L20 20L20 0z")
	for _, original := range []*Path{path, path.Reverse()} {
		before := original.String()
		after := emboldenGlyphPath(original, 0.25)
		for _, x := range []float64{-0.1, 3.1, 5.9, 9.1} {
			if original.ContainsPoint(x, 10, NonZero) || !after.ContainsPoint(x, 10, NonZero) {
				t.Fatalf("加粗未沿正确填充侧扩张，x=%g", x)
			}
		}
		for _, x := range []float64{4, 10} {
			if after.ContainsPoint(x, 10, NonZero) {
				t.Fatalf("加粗破坏了孔洞，x=%g", x)
			}
		}
		if original.String() != before {
			t.Fatal("加粗修改了输入路径")
		}
	}
}

// 分别测量冷轮廓构建和缓存命中，不把整形及字体解析时间计入粗体偏移。
func BenchmarkFauxBoldCJKGlyphPath(b *testing.B) {
	family := NewFontFamily("CJK-benchmark")
	found := false
	for _, name := range []string{"SimSun", "Noto Sans CJK SC", "Noto Serif CJK SC", "Microsoft YaHei"} {
		if _, ok := FindSystemFontFace(name, FontRegular); !ok {
			continue
		}
		if err := family.LoadSystemFont(name, FontRegular); err != nil {
			b.Fatal(err)
		}
		found = true
		break
	}
	if !found {
		b.Skip("未安装可用于基准测试的中文系统字体")
	}
	defer family.Destroy()
	face := family.Face(12, Black, FontRegular)
	face.FauxBold = FontBold.FauxWeight()
	glyphs := face.Glyphs("中华人民共和国字体渲染测试中文排版粗体")
	ppem := face.PPEM(DefaultResolution)
	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			face.buildPath(glyphs, ppem)
		}
	})
	b.Run("cached", func(b *testing.B) {
		face.toPath(glyphs, ppem)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			face.toPath(glyphs, ppem)
		}
	})
}
