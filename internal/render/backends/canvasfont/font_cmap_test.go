package canvasfont

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/font"
	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

// 独立构造旧 Mac cmap 在 Unicode cmap 之前的字体，不依赖用户机器上的楷体。
func fontWithLegacyCmap(t *testing.T) []byte {
	t.Helper()
	f, err := font.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	be := binary.BigEndian
	cmap := f.Tables["cmap"]
	var unicode []byte
	for i := 0; i < int(be.Uint16(cmap[2:])); i++ {
		record := cmap[4+8*i:]
		if be.Uint16(record) == 3 && be.Uint16(record[2:]) == 1 {
			offset := int(be.Uint32(record[4:]))
			unicode = cmap[offset : offset+int(be.Uint16(cmap[offset+2:]))]
			break
		}
	}
	if len(unicode) == 0 {
		t.Fatal("测试字体缺少 Windows Unicode BMP 映射")
	}
	legacy := make([]byte, 262)
	be.PutUint16(legacy[2:], uint16(len(legacy)))
	// H 故意缺失，A 则指向 B：既保护缺字检测，也防止误用非 Unicode 字符码。
	legacy[6+'A'] = byte(f.GlyphIndex('B'))
	out := make([]byte, 20)
	be.PutUint16(out[2:], 2)
	be.PutUint16(out[4:], 1)
	be.PutUint32(out[8:], 20)
	be.PutUint16(out[12:], 3)
	be.PutUint16(out[14:], 1)
	be.PutUint32(out[16:], uint32(20+len(legacy)))
	out = append(out, legacy...)
	out = append(out, unicode...)
	f.Tables["cmap"] = out
	return f.Write()
}

func TestFallbackFontUsesUnicodeInsteadOfLegacyMacCmap(t *testing.T) {
	data := fontWithLegacyCmap(t)
	saved := bytes.Clone(data)
	broken, err := font.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if broken.GlyphIndex('H') != 0 {
		t.Fatal("测试字体没有复现旧子表遮蔽 Unicode 映射")
	}
	original, err := font.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	family := canvas.NewFontFamily(t.Name())
	if err := loadFallbackFace(family, data, FontRegular); err != nil {
		t.Fatal(err)
	}
	f := family.Face(12).Font
	for _, r := range "Hello, World! A" {
		if got, want := f.GlyphIndex(r), original.GlyphIndex(r); got != want {
			t.Fatalf("%c 映射 = %d，期望 Unicode 字形 %d", r, got, want)
		}
	}
	object := models.TextObject{CtText: models.CtText{Size: 18, Weight: 700, TextCode: []models.TextCode{{Value: "Hello, World!"}}}}
	if !familyCoversText(family, object, FontBold) {
		t.Fatal("仍把存在于 Unicode cmap 的英文字母判为缺字")
	}
	if !bytes.Equal(saved, data) || !bytes.Equal(f.Tables["cmap"], broken.Tables["cmap"]) {
		t.Fatal("选择映射修改了共享源数据或原始 cmap 表")
	}
	// 字体查询和整形必须落在同一套 Unicode 字形上。
	native := canvas.NewFontFamily("reference")
	if err := native.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	got, _ := buildTextFace(family, object, nil).ToPath("Hello, World!")
	want, _ := buildTextFace(native, object, nil).ToPath("Hello, World!")
	if got == nil || want == nil || got.String() != want.String() {
		t.Fatal("选择 Unicode cmap 后改变了整形字形")
	}
}
