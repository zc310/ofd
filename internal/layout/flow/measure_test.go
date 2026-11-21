package flow

import (
	"encoding/binary"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/sfnt"
)

// 系统等宽候选可为集合；逻辑字体必须仍有可用度量，不能因此被误报为未注册。
func TestMetricFaceAcceptsTrueTypeCollection(t *testing.T) {
	data := make([]byte, 16+len(gomono.TTF))
	copy(data, "ttcf")
	binary.BigEndian.PutUint32(data[4:], 0x00010000)
	binary.BigEndian.PutUint32(data[8:], 1)
	binary.BigEndian.PutUint32(data[12:], 16)
	face := append([]byte(nil), gomono.TTF...)
	for i := range int(binary.BigEndian.Uint16(face[4:])) {
		start := 12 + i*16 + 8
		binary.BigEndian.PutUint32(face[start:], binary.BigEndian.Uint32(face[start:])+16)
	}
	copy(data[16:], face)
	parsed, up, down := metricFace(data)
	if parsed == nil || up <= 0 || down <= 0 {
		t.Fatalf("TTC 度量无效: %v / %g / %g", parsed, up, down)
	}
	var buffer sfnt.Buffer
	index, err := parsed.GlyphIndex(&buffer, 'M')
	if err != nil || index == 0 {
		t.Fatalf("集合中的 Go Mono 字形不可用: %d / %v", index, err)
	}
}

func TestLogicalFontWidthUsesSelectedFaceRatherThanGlobalPrintMetrics(t *testing.T) {
	face := firstFont(gomono.TTF)
	a := layoutState{ctx: t.Context(), opts: Options{AllowLogicalFonts: true},
		faces:         map[string]*sfnt.Font{"logical": face},
		fontResources: map[string]creator.Font{"logical": {Name: "logical", FamilyName: "sans-serif"}}}
	if m, i := a.width("M", 4, "logical"), a.width("i", 4, "logical"); m != i || m == 0 {
		t.Fatalf("没有使用已选择的 Go Mono 字宽: M=%g i=%g", m, i)
	}
	if width := a.width("中", 4, "logical"); width != 4 {
		t.Fatalf("逻辑字体缺字时应按全角估算: %g", width)
	}
}
