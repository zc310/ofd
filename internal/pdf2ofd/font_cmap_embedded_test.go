package pdf2ofd

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	fontparser "github.com/zc310/font"
	"golang.org/x/image/font/gofont/goregular"
)

func embeddedCMapInterpreter(t *testing.T, options Options) *pdfInterpreter {
	t.Helper()
	options, err := options.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	return &pdfInterpreter{ctx: &model.Context{XRefTable: &model.XRefTable{Table: map[int]*model.XRefTableEntry{}}}, control: &conversionControl{ctx: t.Context(), options: options}}
}

func encodingTestStream(program string, dict types.Dict) *types.StreamDict {
	if dict == nil {
		dict = types.Dict{}
	}
	return &types.StreamDict{Dict: dict, Content: []byte(program)}
}

func TestEmbeddedCMapPreservesCodeLengthUnicodeCIDAndWordSpacing(t *testing.T) {
	p := embeddedCMapInterpreter(t, Options{Strict: true})
	program := "4 begincodespacerange <20><7f> <0020><007f> <800000><80ffff> <ff000000><ffffffff> endcodespacerange 5 begincidchar <20> 32 <0020> 33 <0041> 2 <800041> 3 <ff000041> 4 endcidchar 1 begincidrange <41><43> 10 endcidrange"
	f := pdfFontInfo{codeBytes: 2, defaultW: 1000, widths: map[int]float64{10: 600, 2: 700, 3: 800, 4: 900}}
	p.loadEncodingCMap(&f, encodingTestStream(program, nil))
	f.encodedUnicode = map[uint64]string{}
	parsePDFToUnicodeMapping([]byte("5 beginbfchar <20><0020> <0020><0020> <00 41><0042> <800041><0043> <ff000041><0044> endbfchar 1 beginbfrange <41><43>[<0041><0058><0059>] endbfrange"), p.control, f.encodedUnicode)
	text, codes := decodePDFText([]byte{0x41, 0, 0x41, 0x80, 0, 0x41, 0xff, 0, 0, 0x41, 0x20, 0, 0x20}, f)
	if err := p.control.check(); err != nil {
		t.Fatal(err)
	}
	wantCodes := []uint64{pdfEncodingCode(0x41, 1), pdfEncodingCode(0x41, 2), pdfEncodingCode(0x800041, 3), pdfEncodingCode(0xff000041, 4), pdfEncodingCode(32, 1), pdfEncodingCode(32, 2)}
	if text != "ABCD  " || !reflect.DeepEqual(codes, wantCodes) {
		t.Fatalf("decode=%q %x, want %x", text, codes, wantCodes)
	}
	for i, cid := range []uint16{10, 2, 3, 4, 32, 33} {
		if got := pdfCIDCode(codes[i], f); got != cid {
			t.Fatalf("code %x CID=%d want %d", codes[i], got, cid)
		}
	}
	if !pdfCodeWordSpacing(codes[4], f) || pdfCodeWordSpacing(codes[5], f) {
		t.Fatal("Tw must apply only to a single-byte 32")
	}
	if width := pdfTextWidth(codes[:4], f, 10, 0, 0, 100); width != 30 {
		t.Fatalf("CID widths=%g want 30", width)
	}
	// Four-byte bfrange reaching FFFFFFFF must terminate without wrapping.
	parsePDFToUnicodeMapping([]byte("1 beginbfrange <fffffffe><ffffffff><0041> endbfrange"), p.control, f.encodedUnicode)
	if f.encodedUnicode[pdfEncodingCode(0xffffffff, 4)] != "B" {
		t.Fatal("four-byte ToUnicode endpoint lost")
	}
}

func TestEmbeddedCMapInheritsNamedAndStreamBasesWithLocalOverrides(t *testing.T) {
	for _, base := range []types.Object{types.Name("GBK-EUC-V"), types.Name("Identity-V")} {
		t.Run(base.String(), func(t *testing.T) {
			p := embeddedCMapInterpreter(t, Options{Strict: true})
			f := pdfFontInfo{}
			p.loadEncodingCMap(&f, encodingTestStream("/WMode 0 def 1 begincidchar <a3a8> 7 endcidchar", types.Dict{"UseCMap": base}))
			if p.control.check() != nil || f.vertical || pdfCIDCode(pdfEncodingCode(0xa3a8, 2), f) != 7 {
				t.Fatalf("inherit/override failed: %v %+v", p.control.check(), f)
			}
			if base == types.Name("GBK-EUC-V") {
				if pdfCIDCode(pdfEncodingCode(0xd6d0, 2), f) != 4559 || pdfCodeUnicode(pdfEncodingCode(0xd6d0, 2), f) != "中" {
					t.Fatal("named non-Identity base mapping lost")
				}
				if pdfCodeUnicode(pdfEncodingCode(0xa3a8, 2), f) == "（" {
					t.Fatal("overridden CID inherited the base character's Unicode")
				}
			}
		})
	}
	p := embeddedCMapInterpreter(t, Options{Strict: true})
	base := encodingTestStream("/WMode 1 def 1 begincodespacerange <8100><81ff> endcodespacerange 1 begincidrange <8100><8103> 1 endcidrange 1 beginnotdefrange <8104><81ff> 9 endnotdefrange", nil)
	p.ctx.Table[1] = &model.XRefTableEntry{Object: *base}
	f := pdfFontInfo{}
	p.loadEncodingCMap(&f, encodingTestStream("/Unknown usecmap /WMode 0 def 1 begincidchar <8101> 20 endcidchar", types.Dict{"UseCMap": *types.NewIndirectRef(1, 0), "WMode": types.Integer(1)}))
	if p.control.check() != nil || !f.vertical {
		t.Fatalf("stream dictionary precedence failed: %v", p.control.check())
	}
	for code, cid := range map[uint32]uint16{0x8100: 1, 0x8101: 20, 0x8102: 3, 0x8110: 9} {
		if got := pdfCIDCode(pdfEncodingCode(code, 2), f); got != cid {
			t.Fatalf("inherited %x=%d want %d", code, got, cid)
		}
	}
	// The program operator also resolves non-Identity names and escaped names.
	f = pdfFontInfo{}
	p.loadEncodingCMap(&f, encodingTestStream("/GBK#2dEUC#2dV usecmap", nil))
	if p.control.check() != nil || !f.vertical || pdfCodeUnicode(pdfEncodingCode(0xa3a8, 2), f) != "（" {
		t.Fatalf("program inheritance failed: %v", p.control.check())
	}
	// A one-byte code with the same value as an Identity code must not alias it.
	f = pdfFontInfo{}
	p.loadEncodingCMap(&f, encodingTestStream("/Identity-H usecmap 1 begincodespacerange <20><7f> endcodespacerange", nil))
	if p.control.check() == nil {
		t.Fatal("ambiguous inherited code-space prefixes accepted")
	}
}

func TestEmbeddedCMapRangesKeepLastDeclarationAndBoundNotdef(t *testing.T) {
	p := embeddedCMapInterpreter(t, Options{Strict: true, Limits: Limits{MaxOperations: 3000}})
	f := pdfFontInfo{}
	p.loadEncodingCMap(&f, encodingTestStream("1 begincodespacerange <00000000><ffffffff> endcodespacerange 2 begincidrange <fffffffa><ffffffff> 1 <fffffffb><fffffffc> 20 endcidrange 1 begincidchar <fffffffc> 30 endcidchar 2 beginnotdefrange <00000000><ffffffff> 9 <00000002><00000003> 8 endnotdefrange", nil))
	if err := p.control.check(); err != nil {
		t.Fatal(err)
	}
	for code, cid := range map[uint32]uint16{0: 9, 2: 8, 4: 9, 0xfffffffa: 1, 0xfffffffb: 20, 0xfffffffc: 30, 0xfffffffd: 4, 0xffffffff: 6} {
		if got := pdfCIDCode(pdfEncodingCode(code, 4), f); got != cid {
			t.Fatalf("range %x CID=%d want %d", code, got, cid)
		}
	}
	if len(f.encodingCMap.notdef) > 3 || len(f.encodingCMap.ranges) > 5 {
		t.Fatal("ranges expanded instead of retaining intervals")
	}
}

func TestEmbeddedCMapAllPredefinedBasesAcceptInheritedCodeSpaces(t *testing.T) {
	files, err := pdfCMapArchive()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for name := range files {
		if strings.HasSuffix(name, "-UCS2") {
			continue
		}
		p := embeddedCMapInterpreter(t, Options{Strict: true})
		f := pdfFontInfo{}
		p.loadEncodingCMap(&f, encodingTestStream("/"+name+" usecmap", nil))
		if err := p.control.check(); err != nil {
			t.Fatalf("predefined base %s: %v", name, err)
		}
		count++
	}
	if count != 150 {
		t.Fatalf("inherited %d predefined bases, want 150", count)
	}
	p := embeddedCMapInterpreter(t, Options{Strict: true})
	f := pdfFontInfo{}
	p.loadEncodingCMap(&f, encodingTestStream("/UniGB-UTF32-H usecmap 1 begincodespacerange <41><41> endcodespacerange", nil))
	if p.control.check() != nil || pdfCIDCode(pdfEncodingCode(0x41, 1), f) != 0 || pdfCIDCode(pdfEncodingCode(0x41, 4), f) == 0 {
		t.Fatalf("base aliased differently sized code: %v", p.control.check())
	}
}

func TestEmbeddedCMapInvalidCodesDoNotConsumeValidSuffix(t *testing.T) {
	p := embeddedCMapInterpreter(t, Options{Strict: true})
	f := pdfFontInfo{encodedUnicode: map[uint64]string{pdfEncodingCode(0x41, 1): "A", pdfEncodingCode(0x8140, 2): "B"}}
	p.loadEncodingCMap(&f, encodingTestStream("2 begincodespacerange <41><41> <8140><82fe> endcodespacerange 2 begincidchar <41> 1 <8140> 2 endcidchar", nil))
	for _, raw := range [][]byte{{0x81, 0xff, 0x41}, {0x83, 0x41}, {0x81}} {
		text, codes := decodePDFText(raw, f)
		want := strings.Repeat("\uFFFD", len(raw))
		if raw[len(raw)-1] == 0x41 {
			want = strings.TrimSuffix(want, "\uFFFD") + "A"
		}
		if text != want || len(codes) != len(raw) {
			t.Fatalf("invalid input %x decoded=%q %x want %q", raw, text, codes, want)
		}
	}
}

func TestEmbeddedCMapReportsMalformedUnknownAndCyclicBases(t *testing.T) {
	for _, program := range []string{
		"/Unknown-H usecmap", "1 begincodespacerange <0000000000><ffffffffff> endcodespacerange",
		"2 begincodespacerange <00><ff> <0000><ffff> endcodespacerange",
		"1 begincodespacerange <8140><8230> endcodespacerange",
		"1 begincodespacerange <8100><81ff> endcodespacerange 1 begincidchar <8200> 1 endcidchar",
		"1 begincodespacerange <00000000><ffffffff> endcodespacerange 1 begincidrange <00000000><ffffffff> 1 endcidrange",
		"/Identity-H usecmap /WMode 2 def",
	} {
		t.Run(program, func(t *testing.T) {
			var diagnostics []Diagnostic
			p := embeddedCMapInterpreter(t, Options{Strict: true, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }})
			f := pdfFontInfo{}
			p.loadEncodingCMap(&f, encodingTestStream(program, nil))
			if !errors.Is(p.control.check(), ErrUnsupported) || f.encodingCMap != nil || len(diagnostics) != 1 || diagnostics[0].Code != "font.cmap" {
				t.Fatalf("invalid CMap accepted: %v %+v", p.control.check(), diagnostics)
			}
		})
	}
	for _, strict := range []bool{false, true} {
		p := embeddedCMapInterpreter(t, Options{Strict: strict})
		base := encodingTestStream("/Identity-H usecmap", types.Dict{"UseCMap": *types.NewIndirectRef(1, 0)})
		p.ctx.Table[1] = &model.XRefTableEntry{Object: *base}
		f := pdfFontInfo{}
		p.loadEncodingCMap(&f, base)
		if f.encodingCMap != nil || (strict && !errors.Is(p.control.check(), ErrUnsupported)) || (!strict && p.control.check() != nil) {
			t.Fatalf("cycle policy strict=%v err=%v", strict, p.control.check())
		}
	}
}

func TestEmbeddedCMapInheritanceAndParsingHonorLimitsAndCancellation(t *testing.T) {
	for _, limits := range []Limits{{MaxDepth: 2}, {MaxOperations: 10}, {MaxTotalDecodedBytes: 30}, {MaxDecodedStreamBytes: 10}} {
		p := embeddedCMapInterpreter(t, Options{Limits: limits})
		for i := 1; i <= 5; i++ {
			dict := types.Dict{}
			if i < 5 {
				dict["UseCMap"] = *types.NewIndirectRef(i+1, 0)
			}
			p.ctx.Table[i] = &model.XRefTableEntry{Object: *encodingTestStream("/Identity-H usecmap", dict)}
		}
		f := pdfFontInfo{}
		p.loadEncodingCMap(&f, encodingTestStream("/Identity-H usecmap", types.Dict{"UseCMap": *types.NewIndirectRef(1, 0)}))
		if !errors.Is(p.control.check(), ErrLimitExceeded) {
			t.Fatalf("limit %+v ignored: %v", limits, p.control.check())
		}
	}
	p := embeddedCMapInterpreter(t, Options{})
	ctx, cancel := context.WithCancel(t.Context())
	p.control.ctx = ctx
	cancel()
	f := pdfFontInfo{}
	p.loadEncodingCMap(&f, encodingTestStream("/Identity-H usecmap", nil))
	if p.control.check() != context.Canceled {
		t.Fatalf("cancellation wrapped/lost: %v", p.control.check())
	}
	ctx, cancel = context.WithCancel(t.Context())
	p = embeddedCMapInterpreter(t, Options{Strict: true, OnDiagnostic: func(Diagnostic) { cancel() }})
	p.control.ctx = ctx
	p.loadEncodingCMap(&f, encodingTestStream("/Unknown usecmap", nil))
	if p.control.check() != context.Canceled {
		t.Fatalf("diagnostic cancellation lost: %v", p.control.check())
	}
}

// The existing Go font is used as a CIDFont; no CJK font fixture is required.
func embeddedVariableCMapPDF(t *testing.T, vertical bool, renderMode int, streamBase bool) []byte {
	t.Helper()
	sfnt, err := fontparser.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	mapData := make([]byte, 66)
	for cid, r := range map[int]rune{1: 'A', 2: 'B', 3: 'C', 4: 'D', 32: ' '} {
		binary.BigEndian.PutUint16(mapData[2*cid:], sfnt.GlyphIndex(r))
	}
	program := "begincmap /CMapName /Mixed def /CMapType 1 def /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> def 4 begincodespacerange <20><7f> <8100><81ff> <820000><82ffff> <83000000><83ffffff> endcodespacerange 5 begincidchar <41> 1 <8142> 2 <820043> 3 <83000044> 4 <20> 32 endcidchar endcmap"
	unicode := "begincmap 4 begincodespacerange <20><7f> <8100><81ff> <820000><82ffff> <83000000><83ffffff> endcodespacerange 5 beginbfchar <41><0058> <8142><0059> <820043><005a> <83000044><0051> <20><0020> endbfchar endcmap"
	mode := 0
	if vertical {
		mode = 1
	}
	content := fmt.Sprintf("q BT /F1 25 Tf %d Tr 60 Tz 2 Tc 5 Tw 80 160 Td [<41208142> 120 <82004383000044>] TJ ET", renderMode)
	if renderMode == 7 {
		content += " 0 0 1 rg 0 0 200 200 re f"
	}
	content += " Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type0 /BaseFont /GoRegular /Encoding 11 0 R /DescendantFonts [6 0 R] /ToUnicode 9 0 R >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GoRegular /DW 1000 /W [1 [600 700 800 900] 32 [500]] /W2 [1 [-1000 300 800 -1100 400 900 -1200 500 700 -900 250 850]] /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R /CIDToGIDMap 10 0 R >>",
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /Ascent 1000 /Descent -200 /FontBBox [0 -200 1100 1000] /CapHeight 800 /ItalicAngle 0 /StemV 80 /FontFile2 8 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(goregular.TTF), len(goregular.TTF), goregular.TTF),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(unicode), unicode),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(mapData), mapData),
	}
	base := ""
	if streamBase {
		base = " /UseCMap 12 0 R"
		program = "begincmap /CMapName /MixedChild def /CMapType 1 def 1 begincidchar <8142> 2 endcidchar endcmap"
	}
	program = strings.Replace(program, "begincmap", fmt.Sprintf("begincmap /WMode %d def", mode), 1)
	objects = append(objects, fmt.Sprintf("<< /Type /CMap /CMapName /Mixed /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /WMode %d%s /Length %d >>\nstream\n%s\nendstream", mode, base, len(program), program))
	if streamBase {
		program = "begincmap /CMapName /MixedBase def /CMapType 1 def 4 begincodespacerange <20><7f> <8100><81ff> <820000><82ffff> <83000000><83ffffff> endcodespacerange 5 begincidchar <41> 1 <8142> 3 <820043> 3 <83000044> 4 <20> 32 endcidchar endcmap"
		objects = append(objects, fmt.Sprintf("<< /Type /CMap /CMapName /MixedBase /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /WMode 0 /Length %d >>\nstream\n%s\nendstream", len(program), program))
	}
	return assemblePDF(objects)
}

func TestEmbeddedCMapMixedLengthsPreserveMetricsGlyphClipsAndText(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		for _, mode := range []int{0, 7} {
			for _, streamBase := range []bool{false, true} {
				t.Run(fmt.Sprintf("vertical=%v/mode=%d/base=%v", vertical, mode, streamBase), func(t *testing.T) {
					pdf := embeddedVariableCMapPDF(t, vertical, mode, streamBase)
					doc, page, _ := textFidelityConvert(t, pdf)
					texts := layerTexts(page.Content().Layer[0])
					var value strings.Builder
					var glyphs []int
					for _, text := range texts {
						for _, code := range text.TextCode {
							value.WriteString(code.Value)
						}
						for _, transform := range text.CGTransform {
							glyphs = append(glyphs, transform.Glyphs...)
						}
					}
					if strings.ReplaceAll(value.String(), " ", "") != "XYZQ" {
						t.Fatalf("ToUnicode was indexed by CID: %q", value.String())
					}
					sfnt, _ := fontparser.ParseSFNT(goregular.TTF, 0)
					var wantGlyphs []int
					for _, r := range "ABCD" {
						wantGlyphs = append(wantGlyphs, int(sfnt.GlyphIndex(r)))
					}
					var nonspace []int
					for _, glyph := range glyphs {
						if glyph != int(sfnt.GlyphIndex(' ')) {
							nonspace = append(nonspace, glyph)
						}
					}
					if mode == 0 && !reflect.DeepEqual(nonspace, wantGlyphs) {
						t.Fatalf("CIDToGIDMap=%v want %v", nonspace, wantGlyphs)
					}
					if vertical {
						for i, want := range [][2]float64{{72.5, 60}, {70, 103.5}, {67.5, 127}, {73.75, 158.75}} {
							x, y := textModelOrigin(texts[i])
							if math.Abs(x-want[0]) > .002 || math.Abs(y-want[1]) > .002 {
								t.Fatalf("W2/Tw/TJ origin %d=(%g,%g) want %v", i, x, y, want)
							}
						}
					}
					if mode == 7 {
						paths := layerPaths(page.Content().Layer[0])
						if len(paths) != 1 || paths[0].Clips == nil {
							t.Fatal("mixed-code glyph clipping missing")
						}
					}
					image := textFidelityRaster(t, doc, page)
					if dir := os.Getenv("OFD_CMAP_EVIDENCE_DIR"); dir != "" {
						name := fmt.Sprintf("mixed-v%v-tr%d-base%v", vertical, mode, streamBase)
						if err := os.MkdirAll(dir, 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, name+".pdf"), pdf, 0600); err != nil {
							t.Fatal(err)
						}
						f, err := os.Create(filepath.Join(dir, name+".png"))
						if err != nil {
							t.Fatal(err)
						}
						err = png.Encode(f, image)
						closeErr := f.Close()
						if err != nil {
							t.Fatal(err)
						}
						if closeErr != nil {
							t.Fatal(closeErr)
						}
					}
					textFidelityCompareSource(t, pdf, image)
					textFidelityCompareSourceReader(t, pdf, image, "pdftoppm")
					roundtrip := textFidelityPDFOutput(t, doc, page)
					textFidelityCompareSource(t, roundtrip, image)
					text := textFidelityExtractPDF(t, roundtrip)
					if strings.Join(strings.Fields(text), "") != "XYZQ" {
						t.Fatalf("roundtrip text lost: %q", text)
					}
				})
			}
		}
	}
}

func TestEmbeddedCMapInvalidBaseStrictProducesNoOutput(t *testing.T) {
	pdf := verticalTextPDF(t, "BT /F1 25 Tf 80 160 Td <0001> Tj ET", "", "/Unknown-H usecmap")
	var output bytes.Buffer
	var ds []Diagnostic
	err := ConvertWithOptions(t.Context(), pdf, &output, "", Options{Strict: true, OnDiagnostic: func(d Diagnostic) { ds = append(ds, d) }})
	if !errors.Is(err, ErrUnsupported) || output.Len() != 0 || len(ds) != 1 || ds[0].Code != "font.cmap" {
		t.Fatalf("strict invalid base: err=%v bytes=%d diagnostics=%v", err, output.Len(), ds)
	}
}
