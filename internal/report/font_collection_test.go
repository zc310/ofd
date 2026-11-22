package report

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	fontfile "github.com/zc310/ofd/internal/thirdparty/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// 测试字体只借用 Go 字形轮廓，把报告所需中文绑定到现有 GID，不依赖本机中文字体。
func reportTestChineseFont(t *testing.T, data []byte, weight uint16) []byte {
	t.Helper()
	face, err := fontfile.ParseSFNT(bytes.Clone(data), 0)
	if err != nil {
		t.Fatal(err)
	}
	be := binary.BigEndian
	be.PutUint16(face.Tables["OS/2"][4:], weight)
	if weight == 600 {
		// 避免元数据扫描按 BOLD 位把 600 面提升为 700。
		be.PutUint16(face.Tables["OS/2"][62:], be.Uint16(face.Tables["OS/2"][62:])&^0x20)
	}
	mappings := make([]fontrepair.GlyphMapping, 0, 6)
	for _, r := range "中文校验报告" {
		mappings = append(mappings, fontrepair.GlyphMapping{Rune: r, Glyph: face.GlyphIndex('A')})
	}
	data, err = fontrepair.RepairWithGlyphs(face.Write(), mappings)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func reportTestCollection(faces ...[]byte) []byte {
	be := binary.BigEndian
	data := make([]byte, 0, 12+4*len(faces))
	data = append(data, make([]byte, 12+4*len(faces))...)
	copy(data, "ttcf")
	be.PutUint32(data[4:], 0x00010000)
	be.PutUint32(data[8:], uint32(len(faces)))
	for i, face := range faces {
		base := len(data)
		be.PutUint32(data[12+4*i:], uint32(base))
		data = append(data, face...)
		for j := range int(be.Uint16(face[4:])) {
			record := data[base+12+16*j:]
			be.PutUint32(record[8:], be.Uint32(record[8:])+uint32(base))
		}
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return data
}

func TestLoadBoldFontPreservesMatchedCollectionIndex(t *testing.T) {
	for _, indexed := range []bool{true, false} {
		name := "explicit-unindexed"
		if indexed {
			name = "system-indexed"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "multi.ttc")
			data := reportTestCollection(reportTestChineseFont(t, gobold.TTF, 600),
				reportTestChineseFont(t, goregular.TTF, 400), reportTestChineseFont(t, gobold.TTF, 700))
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			scanDir := dir
			if !indexed {
				scanDir = t.TempDir()
			}
			if err := canvas.CacheSystemFonts(filepath.Join(t.TempDir(), "index.gob"), []string{scanDir}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := canvas.CacheSystemFonts(filepath.Join(t.TempDir(), "restore.gob"), nil); err != nil {
					t.Error(err)
				}
			})
			regular, err := loadFontFamilyFaceIndex(path, "Go", false, 1)
			if err != nil {
				t.Fatal(err)
			}
			wantWeight := uint16(600)
			if indexed {
				selected, ok := canvas.FindSystemFontFace("Go", canvas.FontBold)
				if !ok || selected.Index != 2 {
					t.Fatalf("系统匹配未选中 700 面: %+v", selected)
				}
				wantWeight = 700
			}
			bold, ok := loadBoldFont(regular)
			if !ok {
				t.Fatal("未加载到同族实际粗体")
			}
			parsed, err := fontfile.ParseSFNT(bold.Data, 0)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.OS2.UsWeightClass != wantWeight {
				t.Fatalf("粗体字重=%d，期望=%d", parsed.OS2.UsWeightClass, wantWeight)
			}
		})
	}
}
