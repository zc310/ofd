package pdf2ofd

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/canvas"
	fontparser "github.com/zc310/font"
)

func TestPredefinedCMapAdobeResourcesAndAllCIDRanges(t *testing.T) {
	manifest, err := os.ReadFile("cmaps/SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Fields(line)
		data, err := pdfCMapResource(fields[1])
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != fields[0] {
			t.Fatalf("resource %s: %v", fields[1], err)
		}
		if strings.HasSuffix(fields[1], "-UCS2") {
			continue
		}
		cmap, err := loadPDFPredefinedCMap(fields[1])
		if err != nil || cmap == nil {
			t.Fatalf("load %s: %v", fields[1], err)
		}
		// Independently scan Adobe's line-oriented CID records, checking both
		// endpoints and midpoint rather than expanding large four-byte ranges.
		mode := ""
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			f := strings.Fields(scanner.Text())
			if len(f) == 2 && (f[1] == "begincidchar" || f[1] == "begincidrange") {
				mode = f[1]
				continue
			}
			if len(f) == 1 && strings.HasPrefix(f[0], "endcid") {
				mode = ""
				continue
			}
			if mode == "" || len(f) < 2 || !strings.HasPrefix(f[0], "<") {
				continue
			}
			first, err := strconv.ParseUint(strings.Trim(f[0], "<>"), 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			last, target := first, f[1]
			if mode == "begincidrange" {
				last, err = strconv.ParseUint(strings.Trim(f[1], "<>"), 16, 32)
				target = f[2]
			}
			cid, e := strconv.ParseUint(target, 10, 16)
			if err != nil || e != nil {
				t.Fatalf("bad reference line %q", scanner.Text())
			}
			for _, code := range []uint64{first, (first + last) / 2, last} {
				if got := cmap.cid(uint32(code)); uint64(got) != cid+code-first {
					t.Fatalf("%s code %x: CID %d want %d", cmap.name, code, got, cid+code-first)
				}
			}
			count++
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		if len(cmap.spaces) == 0 {
			t.Fatalf("no codespace: %s", cmap.name)
		}
	}
	t.Logf("checked %d Adobe CID records", count)
}

func TestPredefinedCMapUsesCollectionCIDAndVerticalOverrides(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint32
		cid  uint16
		text string
	}{
		{"UniGB-UCS2-H", 0x4e2d, 4559, "中"}, {"UniGB-UCS2-V", 0x4e2d, 4559, "中"},
		{"UniCNS-UCS2-H", 0x4e2d, 661, "中"}, {"UniJIS-UCS2-H", 0x4e2d, 2980, "中"}, {"UniKS-UCS2-H", 0x4e2d, 6922, "中"},
		{"GBK-EUC-V", 0xa3a8, 580, "（"}, {"UniGB-UCS2-V", 0xff08, 580, "（"},
		{"GBK-EUC-V", 0xa1a3, 574, "。"},
	} {
		t.Run(tc.name+fmt.Sprintf("-%x", tc.code), func(t *testing.T) {
			cmap, err := loadPDFPredefinedCMap(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if got := cmap.cid(tc.code); got != tc.cid {
				t.Fatalf("CID=%d want %d", got, tc.cid)
			}
			if got := cmap.unicode(tc.code); got != tc.text {
				t.Fatalf("Unicode=%q want %q", got, tc.text)
			}
			font := pdfFontInfo{predefined: cmap, codeBytes: 2, defaultW: 900, widths: map[int]float64{int(tc.cid): 720, int(tc.code): 100}, verticalMetrics: map[uint16]pdfVerticalMetric{tc.cid: {-1200, 350, 910}}}
			if pdfCodeWidth(uint64(tc.code), font) != 720 || pdfCodeVerticalMetric(uint64(tc.code), font) != (pdfVerticalMetric{-1200, 350, 910}) {
				t.Fatal("metrics indexed by code instead of CID")
			}
			delete(font.widths, int(tc.cid))
			if pdfCodeWidth(uint64(tc.code), font) != 900 {
				t.Fatal("DW overridden by fullwidth/halfwidth approximation")
			}
		})
	}
}

func TestPredefinedCMapFourByteCodesAndToUnicodeStayDistinct(t *testing.T) {
	for _, name := range []string{"UniGB-UTF16-H", "GBK2K-H", "CNS-EUC-H", "UniCNS-UTF16-V", "UniJIS-UTF8-V"} {
		t.Run(name, func(t *testing.T) {
			cmap, err := loadPDFPredefinedCMap(name)
			if err != nil {
				t.Fatal(err)
			}
			base := cmap
			for base.base != nil {
				base = base.base
			}
			var selected pdfCMapRange
			for _, r := range base.ranges {
				if r.first > 65535 && base.unicode(r.first) != "\uFFFD" {
					selected = r
					break
				}
			}
			if selected.first == 0 {
				t.Fatal("no supplementary/multibyte reference")
			}
			var raw [4]byte
			binary.BigEndian.PutUint32(raw[:], selected.first)
			n := 4
			for n > 1 && raw[4-n] == 0 {
				n--
			}
			data := append(append([]byte{}, raw[4-n:]...), raw[4-n:]...)
			font := pdfFontInfo{predefined: cmap, codeBytes: 2, toUnicode: map[uint32]string{selected.first: "𠀀"}}
			text, codes := decodePDFText(data, font)
			if text != "𠀀𠀀" || !reflect.DeepEqual(codes, []uint64{uint64(selected.first), uint64(selected.first)}) {
				t.Fatalf("decode=%q %x", text, codes)
			}
			mapping := parsePDFToUnicode([]byte(fmt.Sprintf("1 beginbfchar <%08x> <d840dc00> endbfchar", selected.first)))
			if mapping[selected.first] != "𠀀" || len(mapping) != 1 {
				t.Fatalf("four-byte ToUnicode truncated: %v", mapping)
			}
		})
	}
	font := pdfFontInfo{encoding: "UniGB-UCS2-H", codeBytes: 2}
	font.predefined, _ = loadPDFPredefinedCMap(font.encoding)
	if pdfCodeWordSpacing(32, font) {
		t.Fatal("double-byte space got Tw")
	}
	font.predefined, _ = loadPDFPredefinedCMap("GBK-EUC-H")
	if !pdfCodeWordSpacing(32, font) {
		t.Fatal("single-byte space lost Tw")
	}
}

func cjkVerticalFont(t *testing.T) []byte {
	t.Helper()
	paths := []string{}
	if path := os.Getenv("OFD_CJK_TEST_FONT"); path != "" {
		paths = append(paths, path)
	} else if runtime.GOOS != "js" && runtime.GOOS != "android" {
		for _, name := range []string{"Noto Sans CJK SC", "Source Han Sans SC", "Microsoft YaHei", "SimSun", "Noto Sans CJK JP"} {
			if path, ok := canvas.FindSystemFont(name, canvas.FontRegular); ok {
				paths = append(paths, path)
			}
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Logf("CJK test font unavailable: %v", err)
			continue
		}
		font, err := fontparser.ParseSFNT(data, 0)
		if err != nil || !font.IsTrueType {
			t.Logf("CJK CIDFontType2 fixture needs a TrueType face: %s", path)
			continue
		}
		covered := true
		for _, r := range "中文（），。：「」《》【】︵︶︐︒︓﹁﹂︽︾︻︼" {
			if font.GlyphIndex(r) == 0 {
				covered = false
				break
			}
		}
		if !covered || len(font.Tables["GSUB"]) == 0 {
			continue
		}
		t.Logf("CJK test font: %s", path)
		return font.Write() // Extract the first TTC face, if needed.
	}
	t.Skip("CJK vertical TrueType font unavailable; set OFD_CJK_TEST_FONT to an external font path")
	return nil
}

func cjkVerticalPDF(t *testing.T, encoding, content string, embedded bool) []byte {
	t.Helper()
	data := cjkVerticalFont(t)
	sfnt, err := fontparser.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmap, err := loadPDFPredefinedCMap(encoding)
	if err != nil {
		t.Fatal(err)
	}
	fontName := "CJKVerticalTest"
	if names := sfnt.Name.Get(6); len(names) > 0 {
		fontName = strings.ReplaceAll(names[0].String(), " ", "#20")
	}
	mapData := make([]byte, 65536*2)
	for _, r := range "中文（），。：「」《》【】" {
		glyph := sfnt.GlyphIndex(r)
		if cmap.vertical {
			glyph = sfnt.GlyphIndex(pdfVerticalPresentationRune(r))
			if glyph == 0 {
				glyph = sfnt.GlyphIndex(r)
			}
		}
		if glyph == 0 {
			t.Fatalf("fixture missing %c", r)
		}
		binary.BigEndian.PutUint16(mapData[int(cmap.cid(uint32(r)))*2:], glyph)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type0 /BaseFont /" + fontName + " /Encoding /" + encoding + " /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /" + fontName + " /DW 1000 /W [4559 [850]] /W2 [580 [-1150 400 820] 581 [-1050 450 900] 4559 [-1250 350 880]] /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 5 >> /FontDescriptor 7 0 R /CIDToGIDMap 9 0 R >>",
		"<< /Type /FontDescriptor /FontName /" + fontName + " /Flags 4 /Ascent 1160 /Descent -288 /FontBBox [-1000 -1000 3000 3000] /CapHeight 733 /ItalicAngle 0 /StemV 80 /FontFile2 8 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(data), len(data), data),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(mapData), mapData),
	}
	if !embedded {
		objects[6] = strings.ReplaceAll(objects[6], " /FontFile2 8 0 R", "")
		objects[5] = strings.ReplaceAll(objects[5], " /CIDToGIDMap 9 0 R", "")
	}
	return assemblePDF(objects)
}

func TestCJKVerticalPredefinedCIDGlyphsOriginsClippingAndExtraction(t *testing.T) {
	for _, mode := range []int{0, 7} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			content := fmt.Sprintf("q BT /F1 23 Tf %d Tr 75 Tz 1 Tc 120 180 Td <ff084e2dff09ff0c3002> Tj ET", mode)
			if mode == 7 {
				content += " 0 0 1 rg 0 0 200 200 re f"
			}
			content += " Q"
			pdf := cjkVerticalPDF(t, "UniGB-UCS2-V", content, true)
			doc, page, ofd := textFidelityConvert(t, pdf)
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != 5 {
				t.Fatalf("texts=%d", len(texts))
			}
			for i, want := range [][2]float64{{110.8, 38.86}, {111.95, 65.69}, {109.65, 93.9}} {
				x, y := textModelOrigin(texts[i])
				if math.Abs(x-want[0]) > .01 || math.Abs(y-want[1]) > .01 {
					t.Fatalf("glyph %d origin=(%g,%g) want %v", i, x, y, want)
				}
			}
			img := textFidelityRaster(t, doc, page)
			textFidelityCompareSource(t, pdf, img)
			textFidelityCompareSourceReader(t, pdf, img, "pdftoppm")
			roundtrip := textFidelityPDFOutput(t, doc, page)
			text := strings.Join(strings.Fields(textFidelityExtractPDF(t, roundtrip)), "")
			if text != "（中），。" {
				t.Fatalf("semantic punctuation changed: %q", text)
			}
			if mode == 0 {
				textFidelityCompareSource(t, roundtrip, img)
			}
			if dir := os.Getenv("OFD_CJK_EVIDENCE_DIR"); dir != "" {
				for suffix, data := range map[string][]byte{"-source.pdf": pdf, ".ofd": ofd, "-roundtrip.pdf": roundtrip} {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("vertical-%d%s", mode, suffix)), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestCJKSubstituteUsesVerticalGSUBWithoutChangingUnicode(t *testing.T) {
	data := cjkVerticalFont(t)
	sfnt, err := fontparser.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	subs, err := pdfVerticalGSUB(sfnt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) == 0 {
		t.Fatal("fixture vertical GSUB missing")
	}
	font := pdfFontInfo{sfnt: sfnt, data: data, vertical: true, substitute: true, codeBytes: 2, verticalSubstitutions: subs}
	for _, r := range "（），。：「」《》【】" {
		g := pdfVerticalSubstituteGlyph(string(r), font)
		if g == 0 || g == sfnt.GlyphIndex(r) {
			t.Fatalf("%c not substituted: %d", r, g)
		}
		transforms := pdfTextGlyphTransforms([]uint64{uint64(r)}, string(r), font)
		if len(transforms) != 1 || transforms[0].Glyphs[0] != int(g) {
			t.Fatalf("%c vertical transform=%+v", r, transforms)
		}
		font.substitute = false
		font.cidToGID = map[uint16]uint16{uint16(r): sfnt.GlyphIndex(r)}
		if pdfCodeGlyph(uint64(r), string(r), font) != sfnt.GlyphIndex(r) {
			t.Fatal("explicit PDF glyph substituted twice")
		}
		font.substitute = true
	}
}
