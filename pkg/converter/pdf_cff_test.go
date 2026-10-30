package converter_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	fontparser "github.com/zc310/font"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	ofdreader "github.com/zc310/ofd/pkg/reader"
	"github.com/zc310/ofd/pkg/validator"
	"golang.org/x/image/font/gofont/goregular"
)

func externalCFFTestFont(t *testing.T, variable string) ([]byte, *fontparser.SFNT) {
	t.Helper()
	paths := []string{os.Getenv(variable)}
	switch variable {
	case "OFD_CFF_TEST_FONT":
		paths = append(paths, "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJKjp-Regular.otf", "/usr/share/fonts/opentype/noto/NotoSansJP-Regular.otf")
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("读取 CFF 测试字体 %q: %v", path, err)
		}
		sfnt, err := fontparser.ParseSFNT(data, 0)
		if err != nil || sfnt.CFF == nil || (variable != "OFD_CFF_SINGLE_TEST_FONT" && sfnt.CFF.NumFontDicts() < 2) {
			continue
		}
		t.Logf("external CFF (%d Font DICTs): %s", sfnt.CFF.NumFontDicts(), path)
		return sfnt.Write(), sfnt // TTC: embed only the selected first face as OTF.
	}
	t.Skipf("未找到适用的 CFF 测试字体（候选路径 %q）；可设置 %s 指向外部 OTF/TTC", paths, variable)
	return nil, nil
}

func TestExternalCFFTestFontSkipsMissingFile(t *testing.T) {
	const variable = "OFD_CFF_MISSING_TEST_FONT"
	t.Setenv(variable, filepath.Join(t.TempDir(), "missing.otf"))
	var skipped bool
	t.Run("missing", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		externalCFFTestFont(t, variable)
		t.Fatal("缺失字体没有跳过测试")
	})
	if !skipped {
		t.Fatal("缺失字体应调用 t.Skip")
	}
}

func cffOFDTestDocument(t *testing.T, data []byte, value string, glyphs []int) []byte {
	t.Helper()
	text := creator.Text{X: 8, Y: 12, Width: 90, Height: 15, Font: "CFF", Size: 6, Value: value}
	if len(glyphs) > 0 {
		for i, glyph := range glyphs {
			text.CGTransforms = append(text.CGTransforms, creator.CGTransform{CodePosition: i, CodeCount: 1, GlyphCount: 1, Glyphs: []int{glyph}})
		}
	}
	format := "otf"
	if len(data) >= 4 && string(data[:4]) == "ttcf" {
		format = "ttc"
	}
	input, err := creator.Marshal(creator.Document{
		ID: "pdf-cff", PageSize: creator.PageSize{Width: 110, Height: 50},
		Fonts: []creator.Font{{Name: "CFF", Format: format, Data: data}, {Name: "Latin", Format: "ttf", Data: goregular.TTF}},
		Pages: []creator.Page{
			{Items: []creator.Item{creator.Text{X: 8, Y: 8, Width: 90, Height: 15, Font: "Latin", Size: 6, Value: "Latin 123"}}},
			{Items: []creator.Item{text}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	check, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	if report := check.ValidateReader(t.Context(), bytes.NewReader(input), "CFF test"); report.Status != validator.StatusValid {
		t.Fatalf("CFF OFD fixture invalid: %+v", report)
	}
	return input
}

func checkCFFPDFPrograms(t *testing.T, pdf, originalCFF []byte) {
	t.Helper()
	ctx, err := api.ReadAndValidate(t.Context(), bytes.NewReader(pdf), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if ctx.PageCount != 2 {
		t.Fatalf("PDF page count=%d want 2", ctx.PageCount)
	}
	fullCFF, subsetTT := false, false
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		stream, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err != nil || stream == nil || stream.Decode() != nil {
			continue
		}
		if name := stream.NameEntry("Subtype"); name != nil && *name == "CIDFontType0C" {
			if !bytes.Equal(stream.Content, originalCFF) {
				t.Fatal("PDF changed the complete CID CFF program")
			}
			fullCFF = true
			continue
		}
		fontData := stream.Content
		if fixed, err := fontrepair.Repair(fontData); err == nil {
			fontData = fixed
		}
		sfnt, err := fontparser.ParseSFNT(fontData, 0)
		if err != nil {
			continue
		}
		if sfnt.CFF != nil && sfnt.CFF.NumFontDicts() > 1 {
			if !bytes.Equal(sfnt.Tables["CFF "], originalCFF) {
				t.Fatal("PDF changed the complete CFF program")
			}
			fullCFF = true
		}
		if sfnt.IsTrueType && sfnt.NumGlyphs() < 100 {
			subsetTT = true
		}
	}
	if !fullCFF || !subsetTT {
		t.Fatalf("mixed font embedding: full CFF=%v subset TrueType=%v", fullCFF, subsetTT)
	}
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		dict, err := ctx.DereferenceDict(entry.Object)
		if err == nil && dict != nil {
			if name := dict.NameEntry("Subtype"); name != nil && *name == "CIDFontType0" && dict["CIDToGIDMap"] != nil {
				t.Fatal("CIDFontType0 incorrectly uses CIDToGIDMap")
			}
		}
	}
}

func embeddedCFFProgram(t *testing.T, ofd []byte) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(ofd), int64(len(ofd)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if !strings.HasPrefix(entry.Name, "Doc_0/Res/Fonts/") {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		font, err := fontparser.ParseSFNT(data, 0)
		if err != nil {
			t.Fatal(err)
		}
		if font.CFF != nil {
			return font.Tables["CFF "]
		}
	}
	t.Fatal("OFD has no embedded CFF font")
	return nil
}

func compareCFFPDFToOFD(t *testing.T, input, pdf []byte, expectedText string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	if tool, err := exec.LookPath("pdftotext"); err == nil {
		out, err := exec.CommandContext(t.Context(), tool, "-enc", "UTF-8", path, "-").CombinedOutput()
		if err != nil || !strings.Contains(string(out), expectedText) || !strings.Contains(string(out), "Latin 123") {
			t.Fatalf("PDF text lost: %v %q", err, out)
		}
	} else {
		t.Log("pdftotext unavailable: extraction check omitted")
	}
	ofd, err := parser.NewOFD(input)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := render.NewDocument(color.White, ofd.Documents[0])
	defer document.Close()
	page := document.Pages[1]
	surface, err := document.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	raster := surface.Rasterize(geom.DPI(144), nil)
	if dir := os.Getenv("OFD_CFF_EVIDENCE_DIR"); dir != "" {
		file, err := os.Create(filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "-")+"-ofd.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, raster)
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	for _, reader := range []string{"mutool", "pdftoppm"} {
		tool, err := exec.LookPath(reader)
		if err != nil {
			t.Logf("%s unavailable: source comparison omitted", reader)
			continue
		}
		base := filepath.Join(dir, reader)
		args := []string{"draw", "-q", "-r", "144", "-o", base + ".png", path, "2"}
		if reader == "pdftoppm" {
			args = []string{"-f", "2", "-l", "2", "-singlefile", "-r", "144", "-png", path, base}
		}
		if out, err := exec.CommandContext(t.Context(), tool, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", reader, err, out)
		}
		file, err := os.Open(base + ".png")
		if err != nil {
			t.Fatal(err)
		}
		image, err := png.Decode(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if dir := os.Getenv("OFD_CFF_EVIDENCE_DIR"); dir != "" {
			data, err := os.ReadFile(base + ".png")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "-")+"-"+reader+".png"), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		compareCFFGlyphInk(t, image, raster)
	}
}

func compareCFFGlyphInk(t *testing.T, source image.Image, got *image.RGBA) {
	t.Helper()
	bad, ink := 0, 0
	dark := func(img image.Image, x, y int) bool {
		return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y < 100
	}
	for y := 1; y < min(source.Bounds().Dy(), got.Bounds().Dy())-1; y++ {
		for x := 1; x < min(source.Bounds().Dx(), got.Bounds().Dx())-1; x++ {
			s, g := dark(source, x, y), dark(got, x, y)
			if !s && !g {
				continue
			}
			ink++
			match := s == g
			for yy := y - 1; yy <= y+1; yy++ {
				for xx := x - 1; xx <= x+1; xx++ {
					if (s && dark(got, xx, yy)) || (g && dark(source, xx, yy)) {
						match = true
					}
				}
			}
			if !match {
				bad++
			}
		}
	}
	if ink < 100 || bad > ink/50 {
		t.Fatalf("PDF/OFD glyph ink differs: %d/%d bounds=%v/%v", bad, ink, source.Bounds(), got.Bounds())
	}
}

func TestPDFMultiFDCFFCompleteEmbeddingPreservesChineseAndOtherFontSubset(t *testing.T) {
	data, sfnt := externalCFFTestFont(t, "OFD_CFF_TEST_FONT")
	value := "中文⽂（），。ABC123"
	for _, r := range value {
		if sfnt.GlyphIndex(r) == 0 {
			t.Skipf("external CFF font lacks %c", r)
		}
	}
	runCFFPDFExport(t, data, sfnt, value, nil)
}

func TestPDFMultiFDCFFTTCEmbedsOnlySelectedFace(t *testing.T) {
	data, sfnt := externalCFFTestFont(t, "OFD_CFF_TEST_FONT")
	const value = "中文ABC123"
	for _, r := range value {
		if sfnt.GlyphIndex(r) == 0 {
			t.Skipf("external CFF font lacks %c", r)
		}
	}
	// Construct a two-face TTC from external CFF and the existing Go TrueType
	// font, without retaining any new test font file in the repository.
	offset := (20 + len(data) + 3) &^ 3
	ttc := make([]byte, offset+len(goregular.TTF))
	copy(ttc, "ttcf")
	binary.BigEndian.PutUint32(ttc[4:], 0x00010000)
	binary.BigEndian.PutUint32(ttc[8:], 2)
	binary.BigEndian.PutUint32(ttc[12:], 20)
	binary.BigEndian.PutUint32(ttc[16:], uint32(offset))
	for _, face := range []struct {
		data   []byte
		offset int
	}{{data, 20}, {goregular.TTF, offset}} {
		copy(ttc[face.offset:], face.data)
		count := int(binary.BigEndian.Uint16(face.data[4:]))
		for i := 0; i < count; i++ {
			entry := face.offset + 12 + i*16
			binary.BigEndian.PutUint32(ttc[entry+8:], binary.BigEndian.Uint32(ttc[entry+8:])+uint32(face.offset))
		}
	}
	runCFFPDFExport(t, ttc, sfnt, value, nil)
}

func TestPDFMultiFDCFFWebFallbackKeepsOtherFontSubset(t *testing.T) {
	data, sfnt := externalCFFTestFont(t, "OFD_CFF_TEST_FONT")
	const value = "中文ABC123"
	for _, r := range value {
		if sfnt.GlyphIndex(r) == 0 {
			t.Skipf("external CFF font lacks %c", r)
		}
	}
	const family = "PDF-CFF-Test-Fallback"
	if err := ofdreader.RegisterFallbackFont(ofdreader.FontSource{Family: family, Data: data}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofdreader.UnregisterFallbackFont(family) })
	reader, err := ofdreader.Open(cffOFDTestDocument(t, nil, value, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := reader.UseFallbackFont(family); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := reader.RenderPDFTo(t.Context(), &output, []int{0, 1}, ofdreader.RenderOptions{DPI: 72, Background: color.White}); err != nil {
		t.Fatal(err)
	}
	checkCFFPDFPrograms(t, output.Bytes(), sfnt.Tables["CFF "])
	compareCFFPDFToOFD(t, cffOFDTestDocument(t, data, value, nil), output.Bytes(), value)
}

func TestPDFMultiFDCFFNonidentityCIDKeepsExplicitGlyphs(t *testing.T) {
	data, sfnt := externalCFFTestFont(t, "OFD_CFF_CID_TEST_FONT")
	var glyphs []int
	var value strings.Builder
	for glyph := uint16(1); glyph < sfnt.NumGlyphs() && len(glyphs) < 6; glyph++ {
		cid, ok := sfnt.CFF.GlyphCID(glyph)
		if !ok || cid == glyph {
			continue
		}
		// The semantic characters intentionally differ from the glyph's cmap;
		// CGTransform is authoritative for appearance, ToUnicode for extraction.
		value.WriteRune(rune('A' + len(glyphs)))
		glyphs = append(glyphs, int(glyph))
	}
	if len(glyphs) < 2 {
		t.Skip("external CFF needs mapped glyphs with nonidentity CIDs")
	}
	runCFFPDFExport(t, data, sfnt, value.String(), glyphs)
}

func TestPDFSingleFDCFFRetainsSubsettingAndOriginalText(t *testing.T) {
	data, sfnt := externalCFFTestFont(t, "OFD_CFF_SINGLE_TEST_FONT")
	if sfnt.CFF.NumFontDicts() != 1 {
		t.Skip("test needs a single Font DICT CFF")
	}
	const value = "ABC123"
	input := cffOFDTestDocument(t, data, value, nil)
	var output bytes.Buffer
	if err := converter.PDF(t.Context(), input, &output); err != nil {
		t.Fatal(err)
	}
	compareCFFPDFToOFD(t, input, output.Bytes(), value)
}

func runCFFPDFExport(t *testing.T, data []byte, sfnt *fontparser.SFNT, value string, glyphs []int) {
	t.Helper()
	input := cffOFDTestDocument(t, data, value, glyphs)
	wantCFF := embeddedCFFProgram(t, input)
	for _, entrypoint := range []string{"converter", "reader"} {
		t.Run(entrypoint, func(t *testing.T) {
			var output bytes.Buffer
			if entrypoint == "converter" {
				if err := converter.PDF(t.Context(), input, &output); err != nil {
					t.Fatal(err)
				}
			} else {
				reader, err := ofdreader.Open(input)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				if err := reader.RenderPDFTo(t.Context(), &output, []int{0, 1}, ofdreader.RenderOptions{DPI: 72, Background: color.White}); err != nil {
					t.Fatal(err)
				}
			}
			if dir := os.Getenv("OFD_CFF_EVIDENCE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				name := strings.ReplaceAll(t.Name(), "/", "-")
				for suffix, data := range map[string][]byte{".ofd": input, ".pdf": output.Bytes()} {
					if err := os.WriteFile(filepath.Join(dir, name+suffix), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			checkCFFPDFPrograms(t, output.Bytes(), wantCFF)
			compareCFFPDFToOFD(t, input, output.Bytes(), value)
		})
	}
}
