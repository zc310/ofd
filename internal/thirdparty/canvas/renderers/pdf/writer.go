package pdf

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/unicode/norm"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
	"github.com/zc310/ofd/internal/thirdparty/canvas/text"
	ctext "github.com/zc310/ofd/internal/thirdparty/canvas/text"
	cfont "github.com/zc310/ofd/internal/thirdparty/font"
)

// TODO: Invalid graphics transparency, Group has a transparency S entry or the S entry is null
// TODO: Invalid Color space, The operator "g" can't be used without Color Profile

// DestKind is a named destination type, matching the PDF destination styles.
type DestKind int

const (
	// DestFit fits the whole page in the window.
	DestFit DestKind = iota
	// DestFitH fits the page height and scrolls to Y.
	DestFitH
	// DestFitV fits the page width and scrolls to X.
	DestFitV
	// DestXYZ scrolls to X, Y and applies Zoom. A Zoom of 0 keeps the current one.
	DestXYZ
	// DestFitR fits the X0, Y0 to X1, Y1 rectangle in the window.
	DestFitR
)

// Dest describes a named destination. Coordinates are in millimetres with the
// origin at the top left of the target page, matching the source formats this is
// fed from; they are converted to PDF's bottom-left origin when written.
//
// Which fields matter depends on Kind: Fit uses none, FitH uses Y, FitV uses X,
// XYZ uses X, Y and Zoom, and FitR uses X0, Y0, X1 and Y1.
type Dest struct {
	Kind           DestKind
	X, Y, Zoom     float64
	X0, Y0, X1, Y1 float64
}

// destArray renders the destination as a PDF array for the given page reference
// and page height. height is needed because the source origin is the top left
// while PDF's is the bottom left.
func (d Dest) destArray(page pdfRef, height float64) pdfArray {
	up := func(v float64) float64 { return (height - v) * ptPerMm }
	switch d.Kind {
	case DestFit:
		return pdfArray{page, pdfName("Fit")}
	case DestFitH:
		return pdfArray{page, pdfName("FitH"), up(d.Y)}
	case DestFitV:
		return pdfArray{page, pdfName("FitV"), d.X * ptPerMm}
	case DestXYZ:
		// ISO 32000-1 section 12.3.2, Table 151: Zoom is dimensionless.
		return pdfArray{page, pdfName("XYZ"), d.X * ptPerMm, up(d.Y), d.Zoom}
	case DestFitR:
		return pdfArray{page, pdfName("FitR"), d.X0 * ptPerMm, up(d.Y1), d.X1 * ptPerMm, up(d.Y0)}
	}
	return pdfArray{page, pdfName("Fit")}
}

type pdfAnchor struct {
	page int
	name string
	rect canvas.Rect
	// dest is set by AddDestToPage and takes precedence over rect. rect stays
	// for the rectangle-based AddAnchor, whose destination type is inferred from
	// the rectangle's shape.
	dest     *Dest
	pageSize float64
}

type pdfOutline struct {
	page  int
	name  string
	level int
	y     float64
	uri   string

	// dest is set by AddOutlineToPage and takes precedence over y. y stays for
	// the AddOutline form, which infers Fit or FitH from whether y is zero.
	dest     *Dest
	pageSize float64

	parent, prev, next, first, last, count int
}

type pdfWriter struct {
	w   io.Writer
	err error

	pos        int
	objOffsets []int
	pages      []pdfRef

	page        *pdfPageWriter
	fontSubset  map[*canvas.Font]*pdfFontSubsetter
	fontsH      map[*canvas.Font]pdfRef
	fontsV      map[*canvas.Font]pdfRef
	fontsStd    map[*canvas.Font]pdfRef
	images      map[image.Image]pdfRef
	forms       map[*canvas.Canvas]pdfRef
	formBytes   int64
	flateWriter *zlib.Writer
	flateLevel  int
	anchors     []pdfAnchor
	outlines    []pdfOutline
	compress    bool
	subset      bool
	title       string
	subject     string
	keywords    string
	author      string
	creator     string
	producer    string
	lang        string

	// Gradient functions depend only on the function dictionary, not geometry or
	// transforms, and can be shared across pages and Forms in this document.
	gradientFunctions     map[string]pdfRef
	gradientFunctionBytes int
}

func newPDFWriter(writer io.Writer) *pdfWriter {
	w := &pdfWriter{
		w:          writer,
		objOffsets: []int{0, 0, 0}, // catalog, metadata, page tree
		fontSubset: map[*canvas.Font]*pdfFontSubsetter{},
		fontsH:     map[*canvas.Font]pdfRef{},
		fontsV:     map[*canvas.Font]pdfRef{},
		fontsStd:   map[*canvas.Font]pdfRef{},
		images:     map[image.Image]pdfRef{},
		compress:   true,
		subset:     true,
	}

	w.write("%%PDF-1.7\n%%Ŧǟċơ\n")
	return w
}

// SetCompression enable the compression of the streams.
func (w *pdfWriter) SetCompression(compress bool) {
	w.compress = compress
}

// SeFontSubsetting enables the subsetting of embedded fonts.
func (w *pdfWriter) SetFontSubsetting(subset bool) {
	w.subset = subset
}

// SetTitle sets the document's title.
func (w *pdfWriter) SetTitle(title string) {
	w.title = title
}

// SetSubject sets the document's subject.
func (w *pdfWriter) SetSubject(subject string) {
	w.subject = subject
}

// SetKeywords sets the document's keywords.
func (w *pdfWriter) SetKeywords(keywords string) {
	w.keywords = keywords
}

// SetAuthor sets the document's author.
func (w *pdfWriter) SetAuthor(author string) {
	w.author = author
}

// SetCreator sets the document's creator.
func (w *pdfWriter) SetCreator(creator string) {
	w.creator = creator
}

// SetProducer 设置 PDF 信息字典中的生产者；空值保留默认生产者。
func (w *pdfWriter) SetProducer(producer string) {
	if producer != "" {
		w.producer = producer
	}
}

// SetLang sets the document's language.
func (w *pdfWriter) SetLang(lang string) {
	w.lang = lang
}

func (w *pdfWriter) writeBytes(b []byte) {
	if w.err != nil {
		return
	}
	n, err := w.w.Write(b)
	w.pos += n
	w.err = err
}

func (w *pdfWriter) write(s string, v ...interface{}) {
	if w.err != nil {
		return
	}
	n, err := fmt.Fprintf(w.w, s, v...)
	w.pos += n
	w.err = err
}

type pdfRef int
type pdfName string
type pdfArray []interface{}
type pdfDict map[pdfName]interface{}
type pdfFilter string
type pdfStream struct {
	dict   pdfDict
	stream []byte
}

const (
	pdfFilterASCII85 pdfFilter = "ASCII85Decode"
	pdfFilterFlate   pdfFilter = "FlateDecode"
	pdfFilterDCT     pdfFilter = "DCTDecode"
	// pdfFilterStoredFlate marks a stream that already holds zlib data: it is written
	// verbatim while the dict still declares FlateDecode.
	pdfFilterStoredFlate pdfFilter = "StoredFlate"
)

func pdfValContinuesName(val any) bool {
	switch val.(type) {
	case string, pdfName, pdfFilter, pdfArray, pdfDict, pdfStream:
		return false
	}
	return true
}

func (w *pdfWriter) writeVal(i interface{}) {
	switch v := i.(type) {
	case bool:
		if v {
			w.write("true")
		} else {
			w.write("false")
		}
	case int:
		w.write("%d", v)
	case float64:
		w.write("%v", dec(v))
	case string:
		v = strings.Replace(v, `\`, `\\`, -1)
		v = strings.Replace(v, `(`, `\(`, -1)
		v = strings.Replace(v, `)`, `\)`, -1)
		w.write("(%v)", v)
	case pdfRef:
		w.write("%v 0 R", v)
	case pdfName, pdfFilter:
		w.write("/%v", v)
	case pdfArray:
		w.write("[")
		for j, val := range v {
			if j != 0 {
				w.write(" ")
			}
			w.writeVal(val)
		}
		w.write("]")
	case pdfDict:
		w.write("<<")
		if val, ok := v["Type"]; ok {
			w.write("/Type")
			if pdfValContinuesName(val) {
				w.write(" ")
			}
			w.writeVal(val)
		}
		if val, ok := v["Subtype"]; ok {
			w.write("/Subtype")
			if pdfValContinuesName(val) {
				w.write(" ")
			}
			w.writeVal(val)
		}
		keys := []string{}
		for key := range v {
			if key != "Type" && key != "Subtype" {
				keys = append(keys, string(key))
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			w.writeVal(pdfName(key))
			if pdfValContinuesName(v[pdfName(key)]) {
				w.write(" ")
			}
			w.writeVal(v[pdfName(key)])
		}
		w.write(">>")
	case pdfStream:
		if v.dict == nil {
			v.dict = pdfDict{}
		}

		filters := []pdfFilter{}
		if filter, ok := v.dict["Filter"].(pdfFilter); ok {
			filters = append(filters, filter)
		} else if filterArray, ok := v.dict["Filter"].(pdfArray); ok {
			for _, f := range slices.Backward(filterArray) {
				if filter, ok := f.(pdfFilter); ok {
					filters = append(filters, filter)
				}
			}
		}
		// Rewrite StoredFlate to FlateDecode in the dict (the data is already zlib), but
		// keep StoredFlate in the encoder filter list so the switch below passes the data
		// through untouched. Turning it into Flate here would compress the passthrough
		// data a second time and the decoder would only recover the inner zlib stream,
		// leaving the image blank.
		if named, ok := v.dict["Filter"].(pdfFilter); ok && named == pdfFilterStoredFlate {
			v.dict["Filter"] = pdfFilterFlate
		} else if filterArray, ok := v.dict["Filter"].(pdfArray); ok {
			for i, f := range filterArray {
				if f == pdfFilterStoredFlate {
					filterArray[i] = pdfFilterFlate
				}
			}
		}

		// DCT 图像流本身已是压缩数据，flate 只能勉强再压 ~1.7%，用低级别
		// 即可拿到几乎相同的压缩率，同时省下大量 CPU。
		flateLevel := zlib.DefaultCompression
		if slices.Contains(filters, pdfFilterDCT) {
			flateLevel = 1
		}

		b := v.stream
		for _, filter := range filters {
			var b2 bytes.Buffer
			switch filter {
			case pdfFilterASCII85:
				w := ascii85.NewEncoder(&b2)
				w.Write(b)
				w.Close()
				fmt.Fprintf(&b2, "~>")
				b = b2.Bytes()
			case pdfFilterFlate:
				if w.flateWriter == nil || w.flateLevel != flateLevel {
					w.flateWriter, _ = zlib.NewWriterLevel(&b2, flateLevel)
					w.flateLevel = flateLevel
				} else {
					w.flateWriter.Reset(&b2)
				}
				w.flateWriter.Write(b)
				w.flateWriter.Close()
				b = b2.Bytes()
			case pdfFilterStoredFlate:
				b2.Write(b)
				b = b2.Bytes()
			default:
				// assume already in the right format
			}
		}

		v.dict["Length"] = len(b)
		w.writeVal(v.dict)
		w.write("stream\n")
		w.writeBytes(b)
		w.write("\nendstream")
	default:
		panic(fmt.Sprintf("unknown PDF type %T", i))
	}
}

func (w *pdfWriter) writeObject(val interface{}) pdfRef {
	// newlines before and after obj and endobj are required by PDF/A
	w.objOffsets = append(w.objOffsets, w.pos)
	w.write("%v 0 obj\n", len(w.objOffsets))
	w.writeVal(val)
	w.write("\nendobj\n")
	return pdfRef(len(w.objOffsets))
}

func standardFontName(font *canvas.Font) string {
	switch strings.ToLower(font.Name()) {
	case "courier":
		if font.Style() == canvas.FontRegular {
			return "Courier"
		} else if font.Style() == canvas.FontBold {
			return "Courier-Bold"
		} else if font.Style() == canvas.FontItalic {
			return "Courier-Oblique"
		} else if font.Style() == canvas.FontBold|canvas.FontItalic {
			return "Courier-BoldOblique"
		}
	case "dingbats":
		if font.Style() == canvas.FontRegular {
			return "ZapfDingbats"
		}
	case "helvetica":
		if font.Style() == canvas.FontRegular {
			return "Helvetica"
		} else if font.Style() == canvas.FontBold {
			return "Helvetica-Bold"
		} else if font.Style() == canvas.FontItalic {
			return "Helvetica-Oblique"
		} else if font.Style() == canvas.FontBold|canvas.FontItalic {
			return "Helvetica-BoldOblique"
		}
	case "symbol":
		if font.Style() == canvas.FontRegular {
			return "Symbol"
		}
	case "times":
		if font.Style() == canvas.FontRegular {
			return "Times-Roman"
		} else if font.Style() == canvas.FontBold {
			return "Times-Bold"
		} else if font.Style() == canvas.FontItalic {
			return "Times-Italic"
		} else if font.Style() == canvas.FontBold|canvas.FontItalic {
			return "Times-BoldItalic"
		}
	}
	return ""
}

func (w *pdfWriter) getFont(font *canvas.Font, vertical bool) pdfRef {
	if standardFont := standardFontName(font); standardFont != "" {
		// handle 14 embedded standard fonts in PDF if name and style match
		if ref, ok := w.fontsStd[font]; ok {
			return ref
		}

		dict := pdfDict{
			"Type":     pdfName("Font"),
			"Subtype":  pdfName("Type1"),
			"BaseFont": pdfName(standardFont),
			"Encoding": pdfName("WinAnsiEncoding"),
		}
		ref := w.writeObject(dict)
		w.fontsStd[font] = ref
		// don't set fontSubset
		return ref
	}

	fonts := w.fontsH
	if vertical {
		fonts = w.fontsV
	}
	if ref, ok := fonts[font]; ok {
		return ref
	}
	w.objOffsets = append(w.objOffsets, 0)
	ref := pdfRef(len(w.objOffsets))
	fonts[font] = ref
	if w.fontSubset[font] == nil {
		w.fontSubset[font] = newPDFFontSubsetter()
	}
	return ref
}

// subsetTag returns a deterministic six-uppercase-letter subset tag derived
// from the embedded font program so that identical programs yield identical tags
// Some PostScript printers cache embedded fonts by name.
func subsetTag(fontProgram []byte) string {
	var tag [6]byte
	checksum := crc32.ChecksumIEEE(fontProgram)
	for i := 0; i < 6; i++ {
		tag[i] = 'A' + byte(checksum%26)
		checksum >>= 5 // divide by 32, 6 times comes to 2^30, inside the checksum range
	}
	return string(tag[:])
}

func (w *pdfWriter) writeFont(ref pdfRef, font *canvas.Font, vertical bool) {
	// CFF 子集保留数值 CID 并按新 GID 重建 FDSelect（Adobe 5176 §13、§18）。
	// 页内字符码经 Encoding 映射到 CID，不能把子集 GID 当成原始 CID。
	sfnt := font.SFNT
	glyphIDs := w.fontSubset[font].List() // also when not subsetting, to minimize cmap table
	if err := w.fontSubset[font].err; err != nil {
		w.err = err
		return
	}
	subset := w.subset
	if subset {
		sfntSubset, err := sfnt.Subset(glyphIDs, cfont.SubsetOptions{Tables: cfont.KeepPDFTables})
		if err == nil {
			sfnt = sfntSubset
		} else {
			w.err = fmt.Errorf("font subsetting failed: %w", err)
			return
		}
	}
	fontProgram := sfnt.Write()
	var cffCIDs []uint16
	cffCIDKeyed := false
	cidSystemInfo := pdfDict{"Registry": "Adobe", "Ordering": "Identity", "Supplement": 0}
	if sfnt.IsCFF && sfnt.CFF != nil {
		registry, ordering, supplement, cidKeyed := sfnt.CFF.CIDSystemInfo()
		cffCIDKeyed = cidKeyed
		if cidKeyed {
			cidSystemInfo = pdfDict{"Registry": registry, "Ordering": ordering, "Supplement": supplement}
			// Embed the original CFF directly as CIDFontType0C. OpenType streams
			// make some readers derive a GID map from ToUnicode instead of the
			// CFF charset when the Encoding CMap is nonidentity.
			fontProgram = sfnt.Tables["CFF "]
		}
		cffCIDs = make([]uint16, len(glyphIDs))
		for i, glyphID := range glyphIDs {
			if subset {
				glyphID = uint16(i)
			}
			cid := glyphID // name-keyed OpenType CFF uses glyph IDs
			if cidKeyed {
				var ok bool
				if cid, ok = sfnt.CFF.GlyphCID(glyphID); !ok {
					w.err = fmt.Errorf("CFF charset has no CID for glyph %d", glyphID)
					return
				}
			}
			cffCIDs[i] = cid
		}
	}

	// calculate the character widths for the W array and shorten it
	f := 1000.0 / float64(font.SFNT.Head.UnitsPerEm)
	widths := make([]int, len(glyphIDs)+1)
	for subsetGlyphID, glyphID := range glyphIDs {
		widths[subsetGlyphID] = int(f*float64(font.SFNT.GlyphAdvance(glyphID)) + 0.5)
	}
	DW := widths[0]
	W := pdfArray{}
	i, j := 1, 1
	for k, width := range widths {
		if k != 0 && width != widths[j] {
			if 4 < k-j { // at about 5 equal widths, it would be shorter using the other notation format
				if i < j {
					arr := pdfArray{}
					for _, w := range widths[i:j] {
						arr = append(arr, w)
					}
					W = append(W, i, arr)
				}
				if widths[j] != DW {
					W = append(W, j, k-1, widths[j])
				}
				i = k
			}
			j = k
		}
	}
	if i < len(widths) {
		arr := pdfArray{}
		for _, w := range widths[i:] {
			arr = append(arr, w)
		}
		W = append(W, i, arr)
	}
	if cffCIDs != nil {
		// W is indexed by CID, while ToUnicode and page strings use the compact
		// character codes. CIDToGIDMap is defined only for CIDFontType2.
		byCID := make(map[uint16]int, len(cffCIDs))
		for i, cid := range cffCIDs {
			byCID[cid] = widths[i]
		}
		ids := make([]int, 0, len(byCID))
		for cid := range byCID {
			ids = append(ids, int(cid))
		}
		sort.Ints(ids)
		W = pdfArray{}
		for _, cid := range ids {
			if width := byCID[uint16(cid)]; width != DW {
				W = append(W, cid, pdfArray{width})
			}
		}
	}

	// create ToUnicode CMap
	toUnicode := bytes.Buffer{}
	fmt.Fprintf(&toUnicode, `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo <</Registry(Adobe)/Ordering(UCS)/Supplement 0>> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF> endcodespacerange`)
	for start := 1; start < len(glyphIDs); start += 100 {
		end := min(start+100, len(glyphIDs))
		fmt.Fprintf(&toUnicode, "\n%d beginbfchar", end-start)
		for code := start; code < end; code++ {
			value, known := w.fontSubset[font].Text(code)
			if !known {
				value = "\uFFFD"
				if runes := font.SFNT.GlyphToUnicode(glyphIDs[code]); len(runes) > 0 {
					value = string(runes[0])
				}
			}
			fmt.Fprintf(&toUnicode, "\n<%04X> <", code)
			for _, unit := range utf16.Encode([]rune(value)) {
				fmt.Fprintf(&toUnicode, "%04X", unit)
			}
			toUnicode.WriteByte('>')
		}
		toUnicode.WriteString("\nendbfchar")
	}
	fmt.Fprintf(&toUnicode, `
endcmap
CMapName currentdict /CMap defineresource pop
end
end`)
	toUnicodeStream := pdfStream{
		dict:   pdfDict{},
		stream: toUnicode.Bytes(),
	}
	if w.compress {
		toUnicodeStream.dict["Filter"] = pdfFilterFlate
	}
	toUnicodeRef := w.writeObject(toUnicodeStream)

	fontProgramStream := pdfStream{
		dict:   pdfDict{},
		stream: fontProgram,
	}
	if w.compress {
		fontProgramStream.dict["Filter"] = pdfFilterFlate
	}

	// write font program
	var cidSubtype string
	var fontfileKey pdfName
	var fontfileRef pdfRef
	if font.SFNT.IsTrueType {
		cidSubtype = "CIDFontType2"
		fontfileKey = "FontFile2"
		fontfileRef = w.writeObject(fontProgramStream)
	} else if font.SFNT.IsCFF {
		cidSubtype = "CIDFontType0"
		fontfileKey = "FontFile3"
		fontProgramStream.dict["Subtype"] = pdfName("OpenType")
		if cffCIDKeyed {
			fontProgramStream.dict["Subtype"] = pdfName("CIDFontType0C")
		}
		fontfileRef = w.writeObject(fontProgramStream)
	}

	// get name and CID subtype
	name := font.Name()
	if records := font.SFNT.Name.Get(cfont.NamePostScript); 0 < len(records) {
		name = records[0].String()
	}
	baseFont := strings.ReplaceAll(name, " ", "")
	if subset {
		baseFont = subsetTag(fontProgram) + "+" + baseFont
	}

	var encoding interface{} = pdfName("Identity-H")
	if vertical {
		encoding = pdfName("Identity-V")
	}
	if cffCIDs != nil {
		mode := 0
		if vertical {
			mode = 1
		}
		var cmap strings.Builder
		fmt.Fprintf(&cmap, "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CMapName /CanvasGlyphs def\n/CMapType 1 def\n/WMode %d def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n", mode)
		for start := 0; start < len(cffCIDs); start += 100 {
			end := min(start+100, len(cffCIDs))
			fmt.Fprintf(&cmap, "%d begincidchar\n", end-start)
			for code := start; code < end; code++ {
				fmt.Fprintf(&cmap, "<%04X> %d\n", code, cffCIDs[code])
			}
			cmap.WriteString("endcidchar\n")
		}
		cmap.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
		stream := pdfStream{dict: pdfDict{"Type": pdfName("CMap"), "CMapName": pdfName("CanvasGlyphs"), "CIDSystemInfo": cidSystemInfo, "WMode": mode}, stream: []byte(cmap.String())}
		if w.compress {
			stream.dict["Filter"] = pdfFilterFlate
		}
		encoding = w.writeObject(stream)
	}

	// Type0 fonts support two-byte page codes. Their descendant metrics and
	// CFF charset are indexed by CID; ToUnicode remains indexed by page code.
	dict := pdfDict{
		"Type":      pdfName("Font"),
		"Subtype":   pdfName("Type0"),
		"BaseFont":  pdfName(baseFont),
		"Encoding":  encoding,
		"ToUnicode": toUnicodeRef,
		"DescendantFonts": pdfArray{pdfDict{
			"Type":     pdfName("Font"),
			"Subtype":  pdfName(cidSubtype),
			"BaseFont": pdfName(baseFont),
			"DW":       DW,
			"W":        W,
			//"CIDToGIDMap": pdfName("Identity"),
			"CIDSystemInfo": cidSystemInfo,
			"FontDescriptor": pdfDict{
				"Type":     pdfName("FontDescriptor"),
				"FontName": pdfName(baseFont),
				"Flags":    4, // Symbolic
				"FontBBox": pdfArray{
					int(f * float64(font.SFNT.Head.XMin)),
					int(f * float64(font.SFNT.Head.YMin)),
					int(f * float64(font.SFNT.Head.XMax)),
					int(f * float64(font.SFNT.Head.YMax)),
				},
				"ItalicAngle": float64(font.SFNT.Post.ItalicAngle),
				"Ascent":      int(f * float64(font.SFNT.Hhea.Ascender)),
				"Descent":     -int(f * float64(font.SFNT.Hhea.Descender)),
				"CapHeight":   int(f * float64(font.SFNT.OS2.SCapHeight)),
				"StemV":       80, // taken from Inkscape, should be calculated somehow, maybe use: 10+220*(usWeightClass-50)/900
				fontfileKey:   fontfileRef,
			},
		}},
	}

	if !subset && font.SFNT.IsTrueType {
		cidToGIDMap := make([]byte, 2*len(glyphIDs))
		for subsetGlyphID, glyphID := range glyphIDs {
			j := int(subsetGlyphID) * 2
			cidToGIDMap[j+0] = byte((glyphID & 0xFF00) >> 8)
			cidToGIDMap[j+1] = byte(glyphID & 0x00FF)
		}
		cidToGIDMapStream := pdfStream{
			dict:   pdfDict{},
			stream: cidToGIDMap,
		}
		if w.compress {
			cidToGIDMapStream.dict["Filter"] = pdfFilterFlate
		}
		cidToGIDMapRef := w.writeObject(cidToGIDMapStream)
		dict["DescendantFonts"].(pdfArray)[0].(pdfDict)["CIDToGIDMap"] = cidToGIDMapRef
	} else if font.SFNT.IsTrueType {
		// CIDs equal the new (subset) glyph IDs; Identity is the spec default
		// when CIDToGIDMap is absent, but strict RIPs are happier with it
		// spelled out (matches Acrobat/Affinity output).
		dict["DescendantFonts"].(pdfArray)[0].(pdfDict)["CIDToGIDMap"] = pdfName("Identity")
	}

	w.objOffsets[ref-1] = w.pos
	w.write("%v 0 obj\n", ref)
	w.writeVal(dict)
	w.write("\nendobj\n")
}

func (w *pdfWriter) writeFonts(fontMap map[*canvas.Font]pdfRef, vertical bool) {
	// sort fonts by ref to make PDF deterministic
	refs := make([]pdfRef, 0, len(fontMap))
	refMap := make(map[pdfRef]*canvas.Font, len(fontMap))
	for font, ref := range fontMap {
		refs = append(refs, ref)
		refMap[ref] = font
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i] < refs[j]
	})
	for _, ref := range refs {
		w.writeFont(ref, refMap[ref], vertical)
	}
}

// validOutlines drops entries whose target page was never written, together with
// their descendants.
//
// AddOutlineToPage accepts a page index that does not exist yet, and the index is
// only checkable once every page exists, so a caller that stopped early can leave
// entries behind. Filtering before the tree is built keeps the positional object
// references contiguous; dropping a parent would otherwise leave its children
// pointing at a level the tree builder treats as disconnected and skips.
func (w *pdfWriter) validOutlines() []pdfOutline {
	kept := make([]pdfOutline, 0, len(w.outlines))
	skipDepth := -1
	for _, entry := range w.outlines {
		if skipDepth >= 0 {
			if entry.level > skipDepth {
				continue
			}
			skipDepth = -1
		}
		if entry.uri == "" && (entry.page < 0 || len(w.pages) <= entry.page) {
			skipDepth = entry.level
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// pdfTextString encodes s as a PDF text string.
//
// PDF text strings are byte strings in PDFDocEncoding unless they start with a
// UTF-16BE byte order mark, so non-ASCII text has to be encoded explicitly.
// Writing the UTF-8 bytes directly renders as mojibake in any viewer.
func pdfTextString(s string) string {
	ascii := true
	for _, r := range s {
		if 0x80 <= r {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}

	rs := utf16.Encode([]rune(s))
	b := make([]byte, 2+2*len(rs))
	b[0] = 254
	b[1] = 255
	for i, r := range rs {
		b[2+2*i+0] = byte(r >> 8)
		b[2+2*i+1] = byte(r & 0x00FF)
	}
	return string(b)
}

func (w *pdfWriter) writeOutlines() (pdfRef, bool) {
	if len(w.outlines) == 0 {
		return 0, false
	}
	w.outlines = w.validOutlines()
	if len(w.outlines) == 0 {
		return 0, false
	}
	last := -1       // last top-level
	stack := []int{} // index into outlines and refs
	firstRef := pdfRef(len(w.objOffsets) + 1)
	for i := range w.outlines {
		if w.outlines[i].level == 0 {
			w.outlines[i].prev = last
			if last != -1 {
				w.outlines[last].next = i
			}
			stack = append(stack[:0], i)
			last = i
		} else if len(stack) == 0 || w.outlines[stack[len(stack)-1]].level+1 < w.outlines[i].level {
			continue // ignore disconnected level
		} else {
			for w.outlines[i].level <= w.outlines[stack[len(stack)-1]].level {
				w.outlines[stack[len(stack)-2]].count += w.outlines[stack[len(stack)-1]].count
				stack = stack[:len(stack)-1]
			}
			parent := stack[len(stack)-1]
			w.outlines[i].parent = parent
			if w.outlines[parent].first == -1 {
				w.outlines[parent].first = i
			} else if prev := w.outlines[parent].last; prev != -1 {
				w.outlines[i].prev = prev
				w.outlines[prev].next = i
			}
			w.outlines[parent].last = i
			w.outlines[parent].count++
			stack = append(stack, i)
		}
	}
	for 1 < len(stack) {
		w.outlines[stack[len(stack)-2]].count += w.outlines[stack[len(stack)-1]].count
		stack = stack[:len(stack)-1]
	}
	for i := range w.outlines {
		outline := pdfDict{
			"Title": pdfTextString(w.outlines[i].name),
		}
		entry := &w.outlines[i]
		if entry.uri != "" {
			outline["A"] = pdfDict{"S": pdfName("URI"), "URI": entry.uri}
		} else if entry.dest != nil {
			outline["Dest"] = entry.dest.destArray(w.pages[entry.page], entry.pageSize)
		} else if entry.y == 0.0 {
			outline["Dest"] = pdfArray{w.pages[entry.page], pdfName("Fit")}
		} else {
			outline["Dest"] = pdfArray{w.pages[entry.page], pdfName("FitH"), entry.y * ptPerMm}
		}
		if w.outlines[i].parent != -1 {
			outline["Parent"] = firstRef + pdfRef(w.outlines[i].parent)
		}
		if w.outlines[i].prev != -1 {
			outline["Prev"] = firstRef + pdfRef(w.outlines[i].prev)
		}
		if w.outlines[i].next != -1 {
			outline["Next"] = firstRef + pdfRef(w.outlines[i].next)
		}
		if w.outlines[i].first != -1 {
			outline["First"] = firstRef + pdfRef(w.outlines[i].first)
		}
		if w.outlines[i].last != -1 {
			outline["Last"] = firstRef + pdfRef(w.outlines[i].last)
		}
		if w.outlines[i].count != 0 {
			outline["Count"] = w.outlines[i].count
		}
		w.writeObject(outline)
	}
	if last == -1 {
		return 0, false
	}
	return w.writeObject(pdfDict{
		"Type":  pdfName("Outlines"),
		"First": firstRef,
		"Last":  firstRef + pdfRef(last),
		"Count": len(w.outlines),
	}), true
}

// Close finished the document.
func (w *pdfWriter) Close() error {
	defer func() {
		w.forms = nil
		w.formBytes = 0
		w.gradientFunctions = nil
		w.gradientFunctionBytes = 0
		w.flateWriter = nil
	}()
	// TODO: support cross reference table streams and compressed objects for all dicts
	if w.page != nil {
		w.pages = append(w.pages, w.page.writePage(pdfRef(3)))
	}

	kids := pdfArray{}
	for _, page := range w.pages {
		kids = append(kids, page)
	}

	// write fonts
	w.writeFonts(w.fontsH, false)
	w.writeFonts(w.fontsV, false)

	// document catalog
	catalog := pdfDict{
		"Type":  pdfName("Catalog"),
		"Pages": pdfRef(3),
		// TODO: add metadata?
	}

	if 0 < len(w.anchors) {
		names := pdfArray{}
		slices.SortFunc(w.anchors, func(a, b pdfAnchor) int {
			return strings.Compare(a.name, b.name) // sort lexically
		})
		for _, anchor := range w.anchors {
			// An anchor registered by AddAnchorToPage may name a page that never
			// got written, for instance when the caller stopped early. The page
			// index is only known to be valid once every page exists, so a
			// dangling anchor is dropped here instead of indexing past w.pages.
			if anchor.page < 0 || len(w.pages) <= anchor.page {
				continue
			}
			var dest pdfArray
			if anchor.dest != nil {
				dest = anchor.dest.destArray(w.pages[anchor.page], anchor.pageSize)
			} else if anchor.rect.X0 == 0.0 && anchor.rect.X1 == 0.0 && anchor.rect.Y0 == 0.0 && anchor.rect.Y1 == 0.0 {
				dest = pdfArray{w.pages[anchor.page], pdfName("Fit")}
			} else if anchor.rect.X0 == 0.0 && anchor.rect.X1 == 0.0 && anchor.rect.Y0 == anchor.rect.Y1 {
				dest = pdfArray{w.pages[anchor.page], pdfName("FitH"), anchor.rect.Y0 * ptPerMm}
			} else if anchor.rect.Y0 == 0.0 && anchor.rect.Y1 == 0.0 && anchor.rect.X0 == anchor.rect.X1 {
				dest = pdfArray{w.pages[anchor.page], pdfName("FitV"), anchor.rect.X0 * ptPerMm}
			} else if anchor.rect.X0 == anchor.rect.X1 || anchor.rect.Y0 == anchor.rect.Y1 {
				dest = pdfArray{w.pages[anchor.page], pdfName("XYZ"), anchor.rect.X0 * ptPerMm, anchor.rect.Y0 * ptPerMm, 0}
			} else {
				dest = pdfArray{w.pages[anchor.page], pdfName("FitR"), anchor.rect.X0 * ptPerMm, anchor.rect.Y0 * ptPerMm, anchor.rect.X1 * ptPerMm, anchor.rect.Y1 * ptPerMm}
			}
			names = append(names, anchor.name, w.writeObject(pdfDict{
				"D": dest,
			}))
		}
		catalog["Names"] = pdfDict{
			"Dests": pdfDict{
				"Names": names,
			},
		}
	}

	if ref, ok := w.writeOutlines(); ok {
		catalog["Outlines"] = ref
	}

	// document info
	producer := w.producer
	if producer == "" {
		producer = "tdewolff/canvas"
	}
	info := pdfDict{
		"Producer":     producer,
		"CreationDate": time.Now().Format("D:20060102150405Z0700"),
	}

	if w.title != "" {
		info["Title"] = pdfTextString(w.title)
	}
	if w.subject != "" {
		info["Subject"] = pdfTextString(w.subject)
	}
	if w.keywords != "" {
		info["Keywords"] = pdfTextString(w.keywords)
	}
	if w.author != "" {
		info["Author"] = pdfTextString(w.author)
	}
	if w.creator != "" {
		info["Creator"] = pdfTextString(w.creator)
	}
	if w.lang != "" {
		catalog["Lang"] = pdfTextString(w.creator)
	}

	// document catalog
	w.objOffsets[0] = w.pos
	w.write("%v 0 obj\n", 1)
	w.writeVal(catalog)
	w.write("\nendobj\n")

	// document info
	w.objOffsets[1] = w.pos
	w.write("%v 0 obj\n", 2)
	w.writeVal(info)
	w.write("\nendobj\n")

	// page tree
	w.objOffsets[2] = w.pos
	w.write("%v 0 obj\n", 3)
	w.writeVal(pdfDict{
		"Type":  pdfName("Pages"),
		"Kids":  pdfArray(kids),
		"Count": len(kids),
	})
	w.write("\nendobj\n")

	xrefOffset := w.pos
	w.write("xref\n0 %d\n0000000000 65535 f \n", len(w.objOffsets)+1)
	for _, objOffset := range w.objOffsets {
		w.write("%010d 00000 n \n", objOffset)
	}
	w.write("trailer\n")
	w.writeVal(pdfDict{
		"Root": pdfRef(1),
		"Size": len(w.objOffsets) + 1,
		"Info": pdfRef(2),
		// TODO: write document ID
	})
	w.write("\nstartxref\n%v\n%%%%EOF\n", xrefOffset)
	return w.err
}

type pdfPageWriter struct {
	*bytes.Buffer
	pdfGraphicsState

	pdf           *pdfWriter
	width, height float64
	resources     pdfDict
	patterns      map[pdfPatternKey]pdfName
	annots        pdfArray

	graphicsStates map[float64]pdfName
}

// pdfGraphicsState groups every cached drawing setting restored by q/Q.
// Resources and output belong to the content writer and survive a pop.
type pdfGraphicsState struct {
	alpha          float64
	fill           canvas.Paint
	stroke         canvas.Paint
	lineWidth      float64
	lineCap        int
	lineJoin       int
	miterLimit     float64
	dashes         []float64
	font           *canvas.Font
	fontSize       float64
	fontDirection  ctext.Direction
	inTextObject   bool
	textPosition   canvas.Matrix
	textCharSpace  float64
	textRenderMode int
}

// pushState closes any text object so q/Q scopes never cross a BT/ET pair.
func (w *pdfPageWriter) pushState() pdfGraphicsState {
	if w.inTextObject {
		w.EndTextObject()
	}
	state := w.pdfGraphicsState
	// Keep dash storage independent of any updates made inside the scope.
	state.dashes = append([]float64(nil), state.dashes...)
	_, _ = w.WriteString(" q")
	return state
}

func (w *pdfPageWriter) popState(state pdfGraphicsState) {
	if w.inTextObject {
		w.EndTextObject()
	}
	_, _ = w.WriteString(" Q")
	w.pdfGraphicsState = state
}

// NewPage starts a new page.
func (w *pdfWriter) NewPage(width, height float64) *pdfPageWriter {
	if w.page != nil {
		w.pages = append(w.pages, w.page.writePage(pdfRef(3)))
	}

	// for defaults see https://help.adobe.com/pdfl_sdk/15/PDFL_SDK_HTMLHelp/PDFL_SDK_HTMLHelp/API_References/PDFL_API_Reference/PDFEdit_Layer/General.html#_t_PDEGraphicState
	w.page = w.newContentWriter(width, height)
	m := canvas.Identity.Scale(ptPerMm, ptPerMm)
	fmt.Fprintf(w.page, " %v %v %v %v %v %v cm", dec(m[0][0]), dec(m[1][0]), dec(m[0][1]), dec(m[1][1]), dec(m[0][2]), dec(m[1][2]))
	return w.page
}

// newContentWriter creates an independent graphics state and resource dictionary
// for a page or Form. Form coordinates use the caller's millimetre coordinate
// system, so only NewPage writes the initial millimetre-to-point transform.
func (w *pdfWriter) newContentWriter(width, height float64) *pdfPageWriter {
	return &pdfPageWriter{
		Buffer:         &bytes.Buffer{},
		pdf:            w,
		width:          width,
		height:         height,
		resources:      pdfDict{},
		patterns:       map[pdfPatternKey]pdfName{},
		graphicsStates: map[float64]pdfName{},
		pdfGraphicsState: pdfGraphicsState{
			alpha:          1.0,
			fill:           canvas.Paint{Color: canvas.Black},
			stroke:         canvas.Paint{Color: canvas.Black},
			lineWidth:      1.0,
			lineCap:        0,
			lineJoin:       0,
			miterLimit:     10.0,
			dashes:         []float64{0.0}, // dashArray and dashPhase
			font:           nil,
			fontSize:       0.0,
			fontDirection:  ctext.LeftToRight,
			inTextObject:   false,
			textPosition:   canvas.Identity,
			textCharSpace:  0.0,
			textRenderMode: 0,
		},
	}
}

func (w *pdfPageWriter) writePage(parent pdfRef) pdfRef {
	b := w.Bytes()
	if 0 < len(b) && b[0] == ' ' {
		b = b[1:]
	}
	stream := pdfStream{
		dict:   pdfDict{},
		stream: b,
	}
	if w.pdf.compress {
		stream.dict["Filter"] = pdfFilterFlate
	}
	contents := w.pdf.writeObject(stream)
	page := pdfDict{
		"Type":      pdfName("Page"),
		"Parent":    parent,
		"MediaBox":  pdfArray{0.0, 0.0, w.width * ptPerMm, w.height * ptPerMm},
		"Resources": w.resources,
		"Group": pdfDict{
			"Type": pdfName("Group"),
			"S":    pdfName("Transparency"),
			"I":    true,
			"CS":   pdfName("DeviceRGB"),
		},
		"Contents": contents,
	}
	if 0 < len(w.annots) {
		page["Annots"] = w.annots
	}
	return w.pdf.writeObject(page)
}

// AddAnchor adds an anchor to which a link can point.
func (w *pdfPageWriter) AddAnchor(name string, rect canvas.Rect) {
	w.pdf.anchors = append(w.pdf.anchors, pdfAnchor{page: len(w.pdf.pages), name: name, rect: rect})
}

// AddLink adds a local or external link. Local links are # + anchor name (see AddAnchor).
func (w *pdfPageWriter) AddLink(uri string, rect canvas.Rect) {
	annot := pdfDict{
		"Type":    pdfName("Annot"),
		"Subtype": pdfName("Link"),
		"Border":  pdfArray{0, 0, 0},
		"Rect":    pdfArray{rect.X0 * ptPerMm, rect.Y0 * ptPerMm, rect.X1 * ptPerMm, rect.Y1 * ptPerMm},
	}
	if 0 < len(uri) && uri[0] == '#' {
		// local link; the destination is named, so it must be written as a name
		// object. PDF 32000-2 table 176 only accepts a name here, while 32000-1
		// table 173 also allowed a byte string. The name tree key is a string
		// either way and both forms compare equal by value.
		annot["Dest"] = pdfName(uri[1:])
	} else {
		annot["Contents"] = uri
		annot["A"] = pdfDict{
			"S":   pdfName("URI"),
			"URI": uri,
		}
	}
	w.annots = append(w.annots, annot)
}

// AddOutline adds an outline element.
func (w *pdfPageWriter) AddOutline(name string, level int, y float64) {
	w.pdf.outlines = append(w.pdf.outlines, pdfOutline{
		page:   len(w.pdf.pages),
		name:   name,
		level:  level,
		y:      y,
		parent: -1,
		prev:   -1,
		next:   -1,
		first:  -1,
		last:   -1,
	})
}

// SetAlpha sets the transparency value.
func (w *pdfPageWriter) SetAlpha(alpha float64) {
	if alpha != w.alpha {
		gs := w.getOpacityGS(alpha)
		fmt.Fprintf(w, " /%v gs", gs)
		w.alpha = alpha
	}
}

// SetFill sets the filling paint.
func (w *pdfPageWriter) SetFill(fill canvas.Paint, m canvas.Matrix) {
	if fill.IsPattern() {
		// TODO
	} else if fill.IsGradient() {
		fmt.Fprintf(w, " /Pattern cs /%v scn", w.getPattern(fill.Gradient, m))
	} else {
		a := float64(fill.Color.A) / 255.0
		if fill.Equal(w.fill) {
			// Stroke and image paints may change the shared opacity even when
			// the cached fill color is unchanged.
			w.SetAlpha(a)
			return
		} else if fill.Color.A == 0 {
			fmt.Fprintf(w, " 0 g")
		} else if fill.Color.R == fill.Color.G && fill.Color.R == fill.Color.B {
			fmt.Fprintf(w, " %v g", dec(float64(fill.Color.R)/255.0/a))
		} else {
			fmt.Fprintf(w, " %v %v %v rg", dec(float64(fill.Color.R)/255.0/a), dec(float64(fill.Color.G)/255.0/a), dec(float64(fill.Color.B)/255.0/a))
		}
		w.SetAlpha(a)
	}
	w.fill = fill
}

// SetStroke sets the stroking paint.
func (w *pdfPageWriter) SetStroke(stroke canvas.Paint, m canvas.Matrix) {
	if stroke.IsPattern() {
		// TODO
	} else if stroke.IsGradient() {
		// TODO: should we unset CS?
		fmt.Fprintf(w, " /Pattern CS /%v SCN", w.getPattern(stroke.Gradient, m))
	} else {
		a := float64(stroke.Color.A) / 255.0
		if stroke.Equal(w.stroke) {
			// Restore opacity independently of the cached stroking color.
			w.SetAlpha(a)
			return
		} else if stroke.Color.A == 0 {
			fmt.Fprintf(w, " 0 G")
		} else if stroke.Color.R == stroke.Color.G && stroke.Color.R == stroke.Color.B {
			fmt.Fprintf(w, " %v G", dec(float64(stroke.Color.R)/255.0/a))
		} else {
			fmt.Fprintf(w, " %v %v %v RG", dec(float64(stroke.Color.R)/255.0/a), dec(float64(stroke.Color.G)/255.0/a), dec(float64(stroke.Color.B)/255.0/a))
		}
		w.SetAlpha(a)
	}
	w.stroke = stroke
}

// SetLineWidth sets the stroke width.
func (w *pdfPageWriter) SetLineWidth(lineWidth float64) {
	if lineWidth != w.lineWidth {
		fmt.Fprintf(w, " %v w", dec(lineWidth))
		w.lineWidth = lineWidth
	}
}

// SetLineCap sets the stroke cap type.
func (w *pdfPageWriter) SetLineCap(capper canvas.Capper) {
	var lineCap int
	if _, ok := capper.(canvas.ButtCapper); ok {
		lineCap = 0
	} else if _, ok := capper.(canvas.RoundCapper); ok {
		lineCap = 1
	} else if _, ok := capper.(canvas.SquareCapper); ok {
		lineCap = 2
	} else {
		panic("PDF: line cap not support")
	}
	if lineCap != w.lineCap {
		fmt.Fprintf(w, " %d J", lineCap)
		w.lineCap = lineCap
	}
}

// SetLineJoin sets the stroke join type.
func (w *pdfPageWriter) SetLineJoin(joiner canvas.Joiner) {
	var lineJoin int
	var miterLimit float64
	if _, ok := joiner.(canvas.BevelJoiner); ok {
		lineJoin = 2
	} else if _, ok := joiner.(canvas.RoundJoiner); ok {
		lineJoin = 1
	} else if miter, ok := joiner.(canvas.MiterJoiner); ok {
		lineJoin = 0
		if math.IsNaN(miter.Limit) {
			panic("PDF: line join not support")
		} else {
			miterLimit = miter.Limit
		}
	} else {
		panic("PDF: line join not support")
	}
	if lineJoin != w.lineJoin {
		fmt.Fprintf(w, " %d j", lineJoin)
		w.lineJoin = lineJoin
	}
	if lineJoin == 0 && miterLimit != w.miterLimit {
		fmt.Fprintf(w, " %v M", dec(miterLimit))
		w.miterLimit = miterLimit
	}
}

// SetDashes sets the dash phase and array.
func (w *pdfPageWriter) SetDashes(dashPhase float64, dashArray []float64) {
	if len(dashArray)%2 == 1 {
		dashArray = append(dashArray, dashArray...)
	}

	// PDF can't handle negative dash phases
	if dashPhase < 0.0 {
		totalLength := 0.0
		for _, dash := range dashArray {
			totalLength += dash
		}
		for dashPhase < 0.0 {
			dashPhase += totalLength
		}
	}

	dashes := append(dashArray, dashPhase)
	if !float64sEqual(dashes, w.dashes) {
		if len(dashes) == 1 {
			fmt.Fprintf(w, " [] 0 d")
			dashes[0] = 0.0
		} else {
			fmt.Fprintf(w, " [%v", dec(dashes[0]))
			for _, dash := range dashes[1 : len(dashes)-1] {
				fmt.Fprintf(w, " %v", dec(dash))
			}
			fmt.Fprintf(w, "] %v d", dec(dashes[len(dashes)-1]))
		}
		w.dashes = dashes
	}
}

// SetFont sets the font.
func (w *pdfPageWriter) SetFont(font *canvas.Font, size float64, direction ctext.Direction) {
	if !w.inTextObject {
		panic("must be in text object")
	}
	if font != w.font || w.fontSize != size || w.fontDirection != direction {
		w.font = font
		w.fontSize = size
		w.fontDirection = direction

		vertical := direction == ctext.TopToBottom || direction == ctext.BottomToTop
		ref := w.pdf.getFont(font, vertical)
		if _, ok := w.resources["Font"]; !ok {
			w.resources["Font"] = pdfDict{}
		} else {
			for name, fontRef := range w.resources["Font"].(pdfDict) {
				if ref == fontRef {
					fmt.Fprintf(w, " /%v %v Tf", name, dec(size))
					return
				}
			}
		}

		name := pdfName(fmt.Sprintf("F%d", len(w.resources["Font"].(pdfDict))))
		w.resources["Font"].(pdfDict)[name] = ref
		fmt.Fprintf(w, " /%v %v Tf", name, dec(size))
	}
}

// SetTextPosition sets the text position.
func (w *pdfPageWriter) SetTextPosition(m canvas.Matrix) {
	if !w.inTextObject {
		panic("must be in text object")
	}
	if m.Equals(w.textPosition) {
		return
	}

	if canvas.Equal(m[0][0], w.textPosition[0][0]) && canvas.Equal(m[0][1], w.textPosition[0][1]) && canvas.Equal(m[1][0], w.textPosition[1][0]) && canvas.Equal(m[1][1], w.textPosition[1][1]) {
		d := w.textPosition.Inv().Dot(canvas.Point{m[0][2], m[1][2]})
		fmt.Fprintf(w, " %v %v Td", dec(d.X), dec(d.Y))
	} else {
		fmt.Fprintf(w, " %v %v %v %v %v %v Tm", dec(m[0][0]), dec(m[1][0]), dec(m[0][1]), dec(m[1][1]), dec(m[0][2]), dec(m[1][2]))
	}
	w.textPosition = m
}

// SetTextRenderMode sets the text rendering mode.
func (w *pdfPageWriter) SetTextRenderMode(mode int) {
	if !w.inTextObject {
		panic("must be in text object")
	}
	if w.textRenderMode != mode {
		fmt.Fprintf(w, " %d Tr", mode)
		w.textRenderMode = mode
	}
}

// SetTextCharSpace sets the text character spacing.
func (w *pdfPageWriter) SetTextCharSpace(space float64) {
	if !w.inTextObject {
		panic("must be in text object")
	}
	if !canvas.Equal(w.textCharSpace, space) {
		fmt.Fprintf(w, " %v Tc", dec(space))
		w.textCharSpace = space
	}
}

// StartTextObject starts a text object.
func (w *pdfPageWriter) StartTextObject() {
	if w.inTextObject {
		panic("already in text object")
	}
	fmt.Fprintf(w, " BT")
	w.textPosition = canvas.Identity
	w.inTextObject = true
}

// EndTextObject ends a text object.
func (w *pdfPageWriter) EndTextObject() {
	if !w.inTextObject {
		panic("must be in text object")
	}
	fmt.Fprintf(w, " ET")
	w.inTextObject = false
}

// WriteText writes text using a writing mode and a list of strings and inter-character distance modifiers (ints or float64s).
func (w *pdfPageWriter) WriteText(mode canvas.WritingMode, TJ ...interface{}) {
	if !w.inTextObject {
		panic("must be in text object")
	}
	if len(TJ) == 0 || w.font == nil {
		return
	}

	first := true
	write := func(glyphs []ctext.Glyph) {
		if first {
			fmt.Fprintf(w, "(")
			first = false
		} else {
			fmt.Fprintf(w, " (")
		}
		subset := w.pdf.fontSubset[w.font]
		if subset == nil {
			form := norm.NFC
			for _, glyph := range glyphs {
				s := form.String(glyph.Text) // split ligatures into separate characters
				for _, b := range s {
					c, ok := charmap.Windows1252.EncodeRune(b)
					if !ok && text.IsSpace(glyph.Text) {
						c = ' ' // convert all whitespace characters to a regular space
					}
					if c == '\n' {
						w.WriteByte('\\')
						w.WriteByte('n')
					} else if c == '\r' {
						w.WriteByte('\\')
						w.WriteByte('r')
					} else if c == '\t' {
						w.WriteByte('\\')
						w.WriteByte('t')
					} else if c == '\b' {
						w.WriteByte('\\')
						w.WriteByte('b')
					} else if c == '\f' {
						w.WriteByte('\\')
						w.WriteByte('f')
					} else if c == '\\' || c == '(' || c == ')' {
						w.WriteByte('\\')
						w.WriteByte(c)
					} else {
						w.WriteByte(c)
					}
				}
			}
		} else {
			for _, glyph := range glyphs {
				glyphID := subset.Get(glyph.ID, glyph.Text)
				for _, c := range []uint8{uint8((glyphID & 0xff00) >> 8), uint8(glyphID & 0x00ff)} {
					if c == '\n' {
						w.WriteByte('\\')
						w.WriteByte('n')
					} else if c == '\r' {
						w.WriteByte('\\')
						w.WriteByte('r')
					} else if c == '\t' {
						w.WriteByte('\\')
						w.WriteByte('t')
					} else if c == '\b' {
						w.WriteByte('\\')
						w.WriteByte('b')
					} else if c == '\f' {
						w.WriteByte('\\')
						w.WriteByte('f')
					} else if c == '\\' || c == '(' || c == ')' {
						w.WriteByte('\\')
						w.WriteByte(c)
					} else {
						w.WriteByte(c)
					}
				}
			}
		}
		fmt.Fprintf(w, ")")
	}
	writeString := func(s string) {
		rs := []rune(s)
		glyphs := make([]ctext.Glyph, len(rs))
		for i, r := range rs {
			glyphs[i].ID = w.font.SFNT.GlyphIndex(r)
			glyphs[i].Text = string(r)
		}
		write(glyphs)
	}

	position := w.textPosition
	if glyphs, ok := TJ[0].([]ctext.Glyph); ok && 0 < len(glyphs) && mode != canvas.HorizontalTB && !glyphs[0].Vertical {
		glyphRotation, glyphOffset := glyphs[0].Rotation(), glyphs[0].YOffset-int32(glyphs[0].SFNT.Head.UnitsPerEm/2)
		if glyphRotation != ctext.NoRotation || glyphOffset != 0 {
			w.SetTextPosition(position.Rotate(float64(glyphRotation)).Translate(0.0, glyphs[0].Size/float64(glyphs[0].SFNT.Head.UnitsPerEm)*mmPerPt*float64(glyphOffset)))
		}
	}

	f := 1000.0 / float64(w.font.SFNT.Head.UnitsPerEm)
	fmt.Fprintf(w, "[")
	for _, tj := range TJ {
		switch val := tj.(type) {
		case []ctext.Glyph:
			i := 0
			for j, glyph := range val {
				if mode == canvas.HorizontalTB || !glyph.Vertical {
					origXAdvance := int32(w.font.SFNT.GlyphAdvance(glyph.ID))
					if glyph.XAdvance != origXAdvance {
						write(val[i : j+1])
						fmt.Fprintf(w, " %d", -int(f*float64(glyph.XAdvance-origXAdvance)+0.5))
						i = j + 1
					}
				} else {
					origYAdvance := -int32(w.font.SFNT.GlyphVerticalAdvance(glyph.ID))
					if glyph.YAdvance != origYAdvance {
						write(val[i : j+1])
						fmt.Fprintf(w, " %d", -int(f*float64(glyph.YAdvance-origYAdvance)+0.5))
						i = j + 1
					}
				}
			}
			write(val[i:])
		case string:
			i := 0
			if mode == canvas.HorizontalTB {
				var rPrev rune
				for j, r := range val {
					if i < j {
						kern := w.font.SFNT.Kerning(w.font.SFNT.GlyphIndex(rPrev), w.font.SFNT.GlyphIndex(r))
						if kern != 0 {
							writeString(val[i:j])
							fmt.Fprintf(w, " %d", -int(f*float64(kern)+0.5))
							i = j
						}
					}
					rPrev = r
				}
			}
			writeString(val[i:])
		case float64:
			fmt.Fprintf(w, " %d", -int(val*1000.0/w.fontSize+0.5))
		case int:
			fmt.Fprintf(w, " %d", -int(float64(val)*1000.0/w.fontSize+0.5))
		}
	}
	fmt.Fprintf(w, "]TJ")
}

// DrawImage embeds and draws an image.
func (w *pdfPageWriter) DrawImage(img image.Image, enc cimage.ImageEncoding, m canvas.Matrix) {
	size := img.Bounds().Size()
	state := w.pushState()
	defer w.popState(state)

	// add clipping path around image for smooth edges when rotating
	outerRect := canvas.Rect{0.0, 0.0, float64(size.X), float64(size.Y)}.Transform(m)
	bl := m.Dot(canvas.Point{0, 0})
	br := m.Dot(canvas.Point{float64(size.X), 0})
	tl := m.Dot(canvas.Point{0, float64(size.Y)})
	tr := m.Dot(canvas.Point{float64(size.X), float64(size.Y)})
	_, _ = fmt.Fprintf(w, " %v %v %v %v re W n", dec(outerRect.X0), dec(outerRect.Y0), dec(outerRect.W()), dec(outerRect.H()))
	fmt.Fprintf(w, " %v %v m %v %v l %v %v l %v %v l h W n", dec(bl.X), dec(bl.Y), dec(tl.X), dec(tl.Y), dec(tr.X), dec(tr.Y), dec(br.X), dec(br.Y))

	ref := w.embedImage(img, enc)
	if _, ok := w.resources["XObject"]; !ok {
		w.resources["XObject"] = pdfDict{}
	}
	name := pdfName(fmt.Sprintf("Im%d", len(w.resources["XObject"].(pdfDict))))
	w.resources["XObject"].(pdfDict)[name] = ref

	m = m.Scale(float64(size.X), float64(size.Y))
	w.SetAlpha(1.0)
	_, _ = fmt.Fprintf(w, " %v %v %v %v %v %v cm /%v Do", dec(m[0][0]), dec(m[1][0]), dec(m[0][1]), dec(m[1][1]), dec(m[0][2]), dec(m[1][2]), name)
}

func (w *pdfPageWriter) embedImage(img image.Image, enc cimage.ImageEncoding) pdfRef {
	if ref, ok := w.pdf.images[img]; ok {
		return ref
	}

	var stream []byte
	var streamMask []byte

	size := img.Bounds().Size()
	filters, filtersMask := flateFilters(w.pdf.compress)
	if cimg, ok := img.(*cimage.Image); ok && cimg.Mimetype == "image/png" && cimg.Mask == nil && enc != cimage.Lossy {
		if raw, info, extracted := pngImageStream(cimg.Bytes); extracted {
			stream = raw
			filters = pdfArray{pdfFilterStoredFlate}
			dict := pdfDict{
				"Type":             pdfName("XObject"),
				"Subtype":          pdfName("Image"),
				"Width":            info.width,
				"Height":           info.height,
				"ColorSpace":       pdfName(info.colorSpace),
				"BitsPerComponent": info.bits,
				"Interpolate":      true,
				"Filter":           filters,
				// When Filter is an array, DecodeParms must be an array too (one entry per
				// filter). Otherwise readers ignore Predictor and treat the PNG per-row
				// filter bytes as pixels, shifting every row and shearing the image.
				"DecodeParms": pdfArray{pdfDict{
					"Predictor":        15,
					"Colors":           info.colors,
					"BitsPerComponent": info.bits,
					"Columns":          info.width,
				}},
			}
			ref := w.pdf.writeObject(pdfStream{dict: dict, stream: stream})
			w.pdf.images[img] = ref
			return ref
		}
	}
	if cimg, ok := img.(*cimage.Image); ok && cimg.Mimetype == "image/jpeg" {
		// image is already lossy
		stream = cimg.Bytes
		filters = pdfArray{pdfFilterDCT}
		if cimg.Mask != nil {
			if enc == cimage.Lossy {
				// Keep the source JPEG color program, but never add lossy encoding
				// to the alpha plane. This also decodes existing JPEG masks once.
				mask, hasMask := alphaGray(img, size)
				if hasMask {
					streamMask = mask.Pix
				}
			} else if cimg.Mask.Mimetype == "image/jpeg" {
				// mask as well
				streamMask = cimg.Mask.Bytes
				filtersMask = pdfArray{pdfFilterDCT}
			} else {
				// Lossless encoding was requested, so DCTDecode is not an option.
				// Emit the unpremultiplied RGB and grayscale alpha streams the same
				// way the non-JPEG lossless paths below do. Reusing the JPEG payload
				// here would contradict the requested encoding, and the previous code
				// wrote RGB bytes straight over that payload while the filter still
				// claimed DCTDecode, which corrupted the image and panicked outright
				// when the JPEG was smaller than width*height*3.
				stream, streamMask, _ = rgbStream(img, size)
				filters, filtersMask = flateFilters(w.pdf.compress)
			}
		}
	} else if enc == cimage.Lossy {
		opaque := false
		if opaqueImg, ok := img.(interface{ Opaque() bool }); ok && opaqueImg.Opaque() {
			opaque = true
		}
		if ok {
			var err error
			img, err = cimg.Image() // allow optimisation in jpeg.Encode
			if err != nil {
				w.pdf.err = err
				return 0
			}
		}
		if opaque {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, nil); err != nil {
				w.pdf.err = err
				return 0
			}
			stream = buf.Bytes()
			filters = pdfArray{pdfFilterDCT}
		} else {
			// JPEG cannot carry alpha. Encode straight RGB, not premultiplied
			// samples, and retain the exact alpha plane in the lossless SMask.
			rgb, alpha, _ := rgbStream(img, size)
			straight := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
			for i := 0; i < size.X*size.Y; i++ {
				copy(straight.Pix[4*i:4*i+3], rgb[3*i:3*i+3])
				straight.Pix[4*i+3] = 255
			}
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, straight, nil); err != nil {
				w.pdf.err = err
				return 0
			}
			stream = buf.Bytes()
			filters = pdfArray{pdfFilterDCT}

			streamMask = alpha
		}
	} else if opaqueImg, ok := img.(interface{ Opaque() bool }); ok && opaqueImg.Opaque() {
		stream, _, _ = rgbStream(img, size)
	} else {
		stream, streamMask, _ = rgbStream(img, size)
	}

	dict := pdfDict{
		"Type":             pdfName("XObject"),
		"Subtype":          pdfName("Image"),
		"Width":            size.X,
		"Height":           size.Y,
		"ColorSpace":       pdfName("DeviceRGB"),
		"BitsPerComponent": 8,
		"Interpolate":      true,
		"Filter":           filters,
	}

	if streamMask != nil {
		dict["SMask"] = w.pdf.writeObject(pdfStream{
			dict: pdfDict{
				"Type":             pdfName("XObject"),
				"Subtype":          pdfName("Image"),
				"Width":            size.X,
				"Height":           size.Y,
				"ColorSpace":       pdfName("DeviceGray"),
				"BitsPerComponent": 8,
				"Interpolate":      true,
				"Filter":           filtersMask,
			},
			stream: streamMask,
		})
	}

	ref := w.pdf.writeObject(pdfStream{
		dict:   dict,
		stream: stream,
	})
	w.pdf.images[img] = ref
	return ref
}

// alphaGray extracts the alpha plane of img as an 8-bit grayscale image and
// reports whether any pixel is not fully opaque.
//
// The obvious loop calls img.At(x, y).RGBA(), which costs four interface
// dispatches per pixel and showed up as a fifth of the time spent converting
// image-heavy documents to PDF. Switching on the concrete image type first
// keeps the exact same colour arithmetic by still calling the colour type's own
// RGBA method, so the output is byte for byte identical.
func alphaGray(img image.Image, size image.Point) (*image.Gray, bool) {
	hasAlpha := false
	mask := image.NewGray(image.Rect(0, 0, size.X, size.Y))
	switch src := img.(type) {
	case *image.NRGBA:
		pix, stride := src.Pix, src.Stride
		for y := 0; y < size.Y; y++ {
			row := pix[y*stride : y*stride+size.X*4]
			out := mask.Pix[y*mask.Stride : y*mask.Stride+size.X]
			for x := 0; x < size.X; x++ {
				o := x * 4
				_, _, _, a := color.NRGBA{row[o], row[o+1], row[o+2], row[o+3]}.RGBA()
				out[x] = byte(a >> 8)
				if out[x] != 0xff {
					hasAlpha = true
				}
			}
		}
	case *image.RGBA:
		pix, stride := src.Pix, src.Stride
		for y := 0; y < size.Y; y++ {
			row := pix[y*stride : y*stride+size.X*4]
			out := mask.Pix[y*mask.Stride : y*mask.Stride+size.X]
			for x := 0; x < size.X; x++ {
				o := x * 4
				_, _, _, a := color.RGBA{row[o], row[o+1], row[o+2], row[o+3]}.RGBA()
				out[x] = byte(a >> 8)
				if out[x] != 0xff {
					hasAlpha = true
				}
			}
		}
	default:
		sp := img.Bounds().Min // starting point
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				_, _, _, a := img.At(sp.X+x, sp.Y+y).RGBA()
				if a != 0 {
					mask.Pix[y*mask.Stride+x] = byte(a >> 8)
				}
				if a>>8 != 255 {
					hasAlpha = true
				}
			}
		}
	}
	return mask, hasAlpha
}

// rgbStream flattens img into an unpremultiplied 8-bit RGB stream. It also
// returns the 8-bit alpha plane, or nil when the image is fully opaque.
//
// As in alphaGray, the per-pixel work goes through the concrete colour type's
// RGBA method so the rounding matches the interface-based version exactly.
func rgbStream(img image.Image, size image.Point) (rgb, alpha []byte, hasAlpha bool) {
	rgb = make([]byte, size.X*size.Y*3)
	alpha = make([]byte, size.X*size.Y)
	switch src := img.(type) {
	case *image.NRGBA:
		pix, stride := src.Pix, src.Stride
		for y := 0; y < size.Y; y++ {
			row := pix[y*stride : y*stride+size.X*4]
			o := y * size.X
			for x := 0; x < size.X; x++ {
				p := x * 4
				i := (o + x) * 3
				c := color.NRGBA{row[p], row[p+1], row[p+2], row[p+3]}
				r, g, b, a := c.RGBA()
				if a != 0 {
					rgb[i+0] = byte((r * 65535 / a) >> 8)
					rgb[i+1] = byte((g * 65535 / a) >> 8)
					rgb[i+2] = byte((b * 65535 / a) >> 8)
				}
				alpha[o+x] = byte(a >> 8)
				if alpha[o+x] != 0xff {
					hasAlpha = true
				}
			}
		}
	case *image.RGBA:
		pix, stride := src.Pix, src.Stride
		for y := 0; y < size.Y; y++ {
			row := pix[y*stride : y*stride+size.X*4]
			o := y * size.X
			for x := 0; x < size.X; x++ {
				p := x * 4
				i := (o + x) * 3
				c := color.RGBA{row[p], row[p+1], row[p+2], row[p+3]}
				r, g, b, a := c.RGBA()
				if a != 0 {
					rgb[i+0] = byte((r * 65535 / a) >> 8)
					rgb[i+1] = byte((g * 65535 / a) >> 8)
					rgb[i+2] = byte((b * 65535 / a) >> 8)
				}
				alpha[o+x] = byte(a >> 8)
				if alpha[o+x] != 0xff {
					hasAlpha = true
				}
			}
		}
	default:
		sp := img.Bounds().Min // starting point
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				i := (y*size.X + x) * 3
				r, g, b, a := img.At(sp.X+x, sp.Y+y).RGBA()
				if a != 0 {
					rgb[i+0] = byte((r * 65535 / a) >> 8)
					rgb[i+1] = byte((g * 65535 / a) >> 8)
					rgb[i+2] = byte((b * 65535 / a) >> 8)
				}
				alpha[y*size.X+x] = byte(a >> 8)
				if alpha[y*size.X+x] != 0xff {
					hasAlpha = true
				}
			}
		}
	}
	if !hasAlpha {
		alpha = nil
	}
	return rgb, alpha, hasAlpha
}

// flateFilters returns the filter chains for raw sample streams, honouring the
// writer's compress setting. An empty chain means the samples are stored as-is.
func flateFilters(compress bool) (filters, filtersMask pdfArray) {
	if compress {
		return pdfArray{pdfFilterFlate}, pdfArray{pdfFilterFlate}
	}
	return nil, nil
}

type pngStreamInfo struct {
	width, height int
	bits          int
	colors        int
	colorSpace    string
}

// pngImageStream extracts the compressed pixel stream from raw PNG bytes. It only
// accepts non-transparent, non-interlaced 8-bit grayscale and truecolor images so the
// IDAT chunks can be used verbatim as a FlateDecode image stream.
func pngImageStream(data []byte) ([]byte, pngStreamInfo, bool) {
	var info pngStreamInfo
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return nil, info, false
	}
	var idat bytes.Buffer
	colorType := -1
	offset := 8
	for offset+12 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if length < 0 || offset+12+length > len(data) {
			return nil, info, false
		}
		kind := string(data[offset+4 : offset+8])
		payload := data[offset+8 : offset+8+length]
		switch kind {
		case "IHDR":
			if length < 13 {
				return nil, info, false
			}
			info.width = int(binary.BigEndian.Uint32(payload[0:4]))
			info.height = int(binary.BigEndian.Uint32(payload[4:8]))
			info.bits = int(payload[8])
			colorType = int(payload[9])
			if payload[10] != 0 || payload[11] != 0 || payload[12] != 0 || info.bits != 8 || info.width <= 0 || info.height <= 0 {
				return nil, info, false
			}
			switch colorType {
			case 0:
				info.colors = 1
				info.colorSpace = "DeviceGray"
			case 2:
				info.colors = 3
				info.colorSpace = "DeviceRGB"
			default:
				return nil, info, false
			}
		case "tRNS", "PLTE":
			return nil, info, false
		case "IDAT":
			idat.Write(payload)
		case "IEND":
			if colorType < 0 || idat.Len() == 0 {
				return nil, info, false
			}
			return idat.Bytes(), info, true
		}
		offset += 12 + length
	}
	return nil, info, false
}

func (w *pdfPageWriter) getOpacityGS(a float64) pdfName {
	if name, ok := w.graphicsStates[a]; ok {
		return name
	}
	name := pdfName(fmt.Sprintf("A%d", len(w.graphicsStates)))
	w.graphicsStates[a] = name

	if _, ok := w.resources["ExtGState"]; !ok {
		w.resources["ExtGState"] = pdfDict{}
	}
	w.resources["ExtGState"].(pdfDict)[name] = pdfDict{
		"CA": a,
		"ca": a,
	}
	return name
}

// pdfPatternKey 是渐变 pattern 的去重键：只由渐变类型、几何、色标和变换矩阵
// 决定，全部为可比较值类型，避免每次填充都对嵌套 pdfDict 做 reflect.DeepEqual。
type pdfPatternKey struct {
	kind   int
	geom   [6]float64
	matrix [6]float64
	extend [2]bool
	stops  [8]canvas.Stop
	nStops int
}

// pdfPatternKeyOf 从渐变和矩阵构造去重键；色标过多等无法用定长键表示时返回 false。
func pdfPatternKeyOf(gradient canvas.Gradient, m canvas.Matrix) (pdfPatternKey, bool) {
	key := pdfPatternKey{
		matrix: [6]float64{m[0][0], m[1][0], m[0][1], m[1][1], m[0][2], m[1][2]},
	}
	var grad canvas.Grad
	switch g := gradient.(type) {
	case *canvas.LinearGradient:
		key.kind = 2
		key.geom = [6]float64{g.Start.X, g.Start.Y, g.End.X, g.End.Y, 0, 0}
		key.extend = g.Extend
		grad = g.Grad
	case *canvas.RadialGradient:
		key.kind = 3
		key.geom = [6]float64{g.C0.X, g.C0.Y, g.R0, g.C1.X, g.C1.Y, g.R1}
		key.extend = g.Extend
		grad = g.Grad
	default:
		return pdfPatternKey{}, false
	}
	if len(grad) > len(key.stops) {
		return pdfPatternKey{}, false
	}
	key.nStops = len(grad)
	copy(key.stops[:], grad)
	return key, true
}

func (w *pdfPageWriter) getPattern(gradient canvas.Gradient, m canvas.Matrix) pdfName {
	// TODO: support patterns/gradients with alpha channel
	key, keyed := pdfPatternKeyOf(gradient, m)
	if keyed {
		if name, ok := w.patterns[key]; ok {
			return name
		}
	}

	shading := pdfDict{
		"ColorSpace": pdfName("DeviceRGB"),
	}
	if g, ok := gradient.(*canvas.LinearGradient); ok {
		shading["ShadingType"] = 2
		shading["Coords"] = pdfArray{g.Start.X * ptPerMm, g.Start.Y * ptPerMm, g.End.X * ptPerMm, g.End.Y * ptPerMm}
		shading["Function"] = w.pdf.gradientFunction(g.Grad)
		shading["Extend"] = pdfArray{g.Extend[0], g.Extend[1]}
	} else if g, ok := gradient.(*canvas.RadialGradient); ok {
		shading["ShadingType"] = 3
		shading["Coords"] = pdfArray{g.C0.X * ptPerMm, g.C0.Y * ptPerMm, g.R0 * ptPerMm, g.C1.X * ptPerMm, g.C1.Y * ptPerMm, g.R1 * ptPerMm}
		shading["Function"] = w.pdf.gradientFunction(g.Grad)
		shading["Extend"] = pdfArray{g.Extend[0], g.Extend[1]}
	}
	pattern := pdfDict{
		"PatternType": 2,
		"Shading":     shading,
		"Matrix":      pdfArray{m[0][0], m[1][0], m[0][1], m[1][1], m[0][2] * ptPerMm, m[1][2] * ptPerMm},
	}

	if _, ok := w.resources["Pattern"]; !ok {
		w.resources["Pattern"] = pdfDict{}
	}
	if !keyed {
		// 无法构造定长键时退回内容比较，保证去重语义不变。
		for name, pat := range w.resources["Pattern"].(pdfDict) {
			if reflect.DeepEqual(pat, pattern) {
				return name
			}
		}
	}
	name := pdfName(fmt.Sprintf("P%d", len(w.resources["Pattern"].(pdfDict))))
	w.resources["Pattern"].(pdfDict)[name] = pattern
	if keyed {
		w.patterns[key] = name
	}
	return name
}

const (
	maxGradientFunctions     = 256
	maxGradientFunctionBytes = 1 << 20
)

// gradientFunction deduplicates the exact serialized PDF function dictionary,
// without quantizing coordinates or stops. Each Pattern retains its own Matrix,
// Coords and Extend; object references never escape the current document.
func (w *pdfWriter) gradientFunction(grad canvas.Grad) pdfRef {
	function := patternGradFunction(grad)

	var (
		encoded    bytes.Buffer
		serializer pdfWriter
	)

	serializer.w = &encoded
	serializer.writeVal(function)

	key := encoded.String()
	if ref, ok := w.gradientFunctions[key]; ok {
		return ref
	}

	ref := w.writeObject(function)
	// Bound both entry count and retained key bytes. Eviction does not invalidate
	// serialized objects. Oversized functions are written without retention.
	if len(key) <= maxGradientFunctionBytes {
		if w.gradientFunctions == nil || len(w.gradientFunctions) >= maxGradientFunctions ||
			w.gradientFunctionBytes > maxGradientFunctionBytes-len(key) {
			w.gradientFunctions = make(map[string]pdfRef)
			w.gradientFunctionBytes = 0
		}

		w.gradientFunctions[key] = ref
		w.gradientFunctionBytes += len(key)
	}

	return ref
}

func patternGradFunction(grad canvas.Grad) pdfDict {
	if len(grad) < 2 {
		return pdfDict{}
	}

	fs := pdfArray{}
	bounds := pdfArray{}
	encode := pdfArray{}
	for i := 0; i < len(grad)-1; i++ {
		fs = append(fs, patternStopFunction(grad[i], grad[i+1]))
		if i != 0 {
			bounds = append(bounds, grad[i].Offset)
		}
		encode = append(encode, 0, 1)
	}
	if len(fs) == 1 {
		f := fs[0].(pdfDict)
		f["Domain"] = pdfArray{grad[0].Offset, grad[len(grad)-1].Offset}
		return f
	}
	return pdfDict{
		"FunctionType": 3,
		"Domain":       pdfArray{grad[0].Offset, grad[len(grad)-1].Offset},
		"Bounds":       bounds,
		"Encode":       encode,
		"Functions":    fs,
	}
}

func patternStopFunction(s0, s1 canvas.Stop) pdfDict {
	return pdfDict{
		"FunctionType": 2,
		"Domain":       pdfArray{0, 1},
		"N":            1,
		"C0":           unpremultiplyStop(s0.Color),
		"C1":           unpremultiplyStop(s1.Color),
	}
}

// unpremultiplyStop converts a premultiplied stop color into the straight RGB
// triple a Type 2 function expects.
func unpremultiplyStop(c color.RGBA) pdfArray {
	a := float64(c.A) / 255.0
	if a == 0.0 {
		return pdfArray{0.0, 0.0, 0.0}
	}
	return pdfArray{float64(c.R) / 255.0 / a, float64(c.G) / 255.0 / a, float64(c.B) / 255.0 / a}
}
