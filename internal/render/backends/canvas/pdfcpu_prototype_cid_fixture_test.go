package canvas

import (
	"bytes"
	"encoding/binary"
	"os/exec"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// Adobe 5176 sections 5, 13, 18 and 19: three glyphs, two Font DICTs,
// nonidentity CIDs 580/22031, distinct default widths and distinct outlines.
func prototypeCIDFontData() []byte {
	names := prototypeCFFIndex([]byte("PrototypeCID"))
	strings := prototypeCFFIndex([]byte("Adobe"), []byte("Identity"))
	top := prototypeCFFDict(0x0c1e, 391, 392, 0)
	// FontMatrix = [0.0005 0 0 0.0005 0 0], hence 2000 design units per em.
	top = append(top, 30, 0x0a, 0x00, 0x05, 0xff, 139, 139, 30, 0x0a, 0x00, 0x05, 0xff, 139, 139, 12, 7)
	offsets := make([]int, 0, 4)
	for _, op := range []int{15, 17, 0x0c24, 0x0c25} {
		offsets = append(offsets, len(top)+1)
		top = append(top, prototypeCFFDict(op, 0)...)
	}
	base := 4 + len(names) + len(prototypeCFFIndex(top)) + len(strings) + 2
	charset := []byte{0, 2, 68, 86, 15}
	rectangle := prototypeType2Numbers(100, 100)
	rectangle = append(rectangle, 21)
	rectangle = append(rectangle, prototypeType2Numbers(300, 0, 0, 400, -300, 0, 0, -400)...)
	rectangle = append(rectangle, 5, 14)
	triangle := prototypeType2Numbers(100, 100)
	triangle = append(triangle, 21)
	triangle = append(triangle, prototypeType2Numbers(400, 0, -200, 400, -200, -400)...)
	triangle = append(triangle, 5, 14)
	chars := prototypeCFFIndex([]byte{14}, rectangle, triangle)
	private0, private1 := prototypeCFFDict(20, 500), prototypeCFFDict(20, 900)
	fd0, fd1 := prototypeCFFDict(18, len(private0), 0), prototypeCFFDict(18, len(private1), 0)
	fdPos := base + len(charset) + len(chars)
	selectPos := fdPos + len(prototypeCFFIndex(fd0, fd1))
	binary.BigEndian.PutUint32(fd0[6:], uint32(selectPos+4))
	binary.BigEndian.PutUint32(fd1[6:], uint32(selectPos+4+len(private0)))
	for i, value := range []int{base, base + len(charset), fdPos, selectPos} {
		binary.BigEndian.PutUint32(top[offsets[i]:], uint32(value))
	}
	data := []byte{1, 0, 4, 4}
	for _, part := range [][]byte{names, prototypeCFFIndex(top), strings, {0, 0}, charset, chars,
		prototypeCFFIndex(fd0, fd1), {0, 0, 0, 1}, private0, private1} {
		data = append(data, part...)
	}
	return data
}

func prototypeCFFIndex(objects ...[]byte) []byte {
	data := binary.BigEndian.AppendUint16(nil, uint16(len(objects)))
	data = append(data, 4)
	offset := uint32(1)
	for _, object := range objects {
		data = binary.BigEndian.AppendUint32(data, offset)
		offset += uint32(len(object))
	}
	data = binary.BigEndian.AppendUint32(data, offset)
	for _, object := range objects {
		data = append(data, object...)
	}
	return data
}

func prototypeCFFDict(op int, values ...int) []byte {
	var data []byte
	for _, value := range values {
		data = append(data, 29)
		data = binary.BigEndian.AppendUint32(data, uint32(value))
	}
	if op > 255 {
		return append(data, 12, byte(op))
	}
	return append(data, byte(op))
}

func prototypeType2Numbers(values ...int16) []byte {
	var data []byte
	for _, value := range values {
		data = append(data, 28)
		data = binary.BigEndian.AppendUint16(data, uint16(value))
	}
	return data
}

func TestPDFCPUPrototypeCIDCFFKeepsOriginalCIDCharsetAndFontDicts(t *testing.T) {
	raw := prototypeCIDFontData()
	data, err := fontrepair.RepairWithGlyphs(raw, []fontrepair.GlyphMapping{
		{Rune: 'A', Glyph: 1}, {Rune: 'B', Glyph: 2}, {Rune: '😀', Glyph: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	family := canvas.NewFontFamily("nonidentity-CID")
	if err := family.LoadFont(data, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	defer family.Destroy()
	face := family.Face(24, canvas.Black)
	if face.Font.UnitsPerEm() != 2000 {
		t.Fatalf("非默认FontMatrix对应的UPM=%d，期望2000", face.Font.UnitsPerEm())
	}
	if face.Font.CFF.NumFontDicts() != 2 {
		t.Fatal("测试必须保留两个Font DICT")
	}
	for glyph, want := range map[uint16]uint16{1: 580, 2: 22031} {
		if got, ok := face.Font.CFF.GlyphCID(glyph); !ok || got != want {
			t.Fatalf("GID%d的CID=%d，期望%d", glyph, got, want)
		}
	}
	text := canvas.NewTextLine(face, "AB😀", canvas.Left)
	matrix := canvas.Identity.Translate(5, 10)
	r, err := pdfcpuwriter.New(t.Context(), 70, 30)
	if err != nil {
		t.Fatal(err)
	}
	r.RenderText(text, matrix)
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(face.Font.Tables["CFF "], raw) {
		t.Fatal("嵌入时修改了CFF字形、FDSelect或子程序")
	}
	if err := api.Validate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	ctx := prototypePDFContext(t, output.Bytes())
	page, _, attrs, err := ctx.PageDict(t.Context(), 1, false)
	if err != nil || page == nil {
		t.Fatalf("读取页面失败：%v", err)
	}
	fonts, err := ctx.DereferenceDict(attrs.Resources["Font"])
	if err != nil {
		t.Fatal(err)
	}
	font, err := ctx.DereferenceDict(fonts["F0"])
	if err != nil {
		t.Fatal(err)
	}
	encoding, _, err := ctx.DereferenceStreamDict(font["Encoding"])
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.DecodeStream(encoding); err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{[]byte("<0001> 580"), []byte("<0002> 22031"), []byte("<0003> 580")} {
		if !bytes.Contains(encoding.Content, want) {
			t.Fatalf("CFF编码把原CID误当GID：%s", encoding.Content)
		}
	}
	if _, ok := font["Encoding"].(types.IndirectRef); !ok {
		t.Fatal("CID键控CFF必须保留自定义Encoding CMap")
	}
	if got := prototypeExtractText(t, output.Bytes()); !bytes.Contains(got, []byte("AB😀")) {
		t.Fatalf("非恒等CID映射丢失Unicode语义：%q", got)
	}
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少pdftoppm")
	}
	reference := renderPDFText(t, 70, 30, func(pdf pdfTextRenderer) { text.RenderTo(pdf, matrix, 0) })
	prototypeSavePage(t, 704, output.Bytes(), reference)
	first, err := testutil.RasterizePDFPages(t.Context(), tool, output.Bytes(), 144)
	if err != nil {
		t.Fatal(err)
	}
	second, err := testutil.RasterizePDFPages(t.Context(), tool, reference, 144)
	if err != nil {
		t.Fatal(err)
	}
	prototypeCheckFontInk(t, first[0], second[0])
}
