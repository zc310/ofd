package layout

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// famEnum 标识公文版式引用的字族类别，用于把 OFD 字体资源指向标准字体族。
type famEnum uint8

const (
	// famBody 是正文仿宋，对应 Options.BodyFamily。
	famBody famEnum = iota
	// famHei 是黑体，对应 Options.HeiFamily（结构层次序号、密级、紧急程度）。
	famHei
	// famKai 是楷体，对应 Options.KaiFamily（二级序号、签发人姓名）。
	famKai
	// famTitle 是小标宋，对应 Options.TitleFamily（文件标题、发文机关标志）。
	famTitle
	// famSong 是宋体，对应 Options.SongFamily（页码等）。
	famSong
)

// metricKey 唯一标识用于度量的一段字符样式。
type metricKey struct {
	bold   bool
	italic bool
	mono   bool
	fam    famEnum
}

var (
	metricsOnce sync.Once
	metricFonts map[metricKey]*sfnt.Font
	printFonts  map[famEnum]*sfnt.Font
	metricAsc   map[metricKey]float64
	metricDesc  map[metricKey]float64
)

// initMetrics 惰性加载度量字体。公文族优先使用系统里的仿宋、黑体、楷体、宋体，
// 找不到时退回内置 Go 字体。生成 OFD 时仍只写逻辑字体名，不嵌入这些字体。
func initMetrics() {
	monoData := monoFontData()
	latin := map[metricKey][]byte{
		{bold: false, italic: false, mono: false}: goregular.TTF,
		{bold: true, italic: false, mono: false}:  gobold.TTF,
		{bold: false, italic: true, mono: false}:  goitalic.TTF,
		{bold: true, italic: true, mono: false}:   gobolditalic.TTF,
		{bold: false, italic: false, mono: true}:  monoData,
		{bold: true, italic: false, mono: true}:   monoData,
		{bold: false, italic: true, mono: true}:   monoData,
		{bold: true, italic: true, mono: true}:    monoData,
	}
	printFonts = map[famEnum]*sfnt.Font{
		famBody:  loadPrintFont("FangSong", "Noto Serif CJK SC"),
		famHei:   loadPrintFont("SimHei", "Noto Sans CJK SC", "Source Han Sans SC"),
		famKai:   loadPrintFont("KaiTi", "Noto Serif CJK SC"),
		famTitle: loadPrintFont("Noto Serif CJK SC", "Source Han Serif SC"),
		famSong:  loadPrintFont("SimSun", "Noto Serif CJK SC"),
	}
	metricFonts = make(map[metricKey]*sfnt.Font, len(latin)*5)
	metricAsc = make(map[metricKey]float64, len(latin)*5)
	metricDesc = make(map[metricKey]float64, len(latin)*5)
	const refPPEM = 1000 * 64
	for fam := famBody; fam <= famSong; fam++ {
		for key, data := range latin {
			base := key
			base.fam = fam
			face, err := sfnt.Parse(data)
			if err != nil {
				continue
			}
			metricFonts[base] = face
			var buf sfnt.Buffer
			metrics, err := face.Metrics(&buf, refPPEM, font.HintingNone)
			if err != nil {
				metricAsc[base] = 0.8
				metricDesc[base] = 0.2
				continue
			}
			metricAsc[base] = float64(metrics.Ascent) / 64 / 1000
			metricDesc[base] = float64(metrics.Descent) / 64 / 1000
		}
	}
}

// monoFontData 读取 Consolas 或系统等宽字体，供行内代码和代码块度量。
// 找不到时退回内置 Go Mono。
func monoFontData() []byte {
	for _, family := range []string{"Consolas", "Noto Sans Mono CJK SC", "Liberation Mono"} {
		path := fontFile(family)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || firstFont(data) == nil {
			continue
		}
		return data
	}
	return gomono.TTF
}

// loadPrintFont 按候选族名查找系统字体并解析第一个可用文件。
func loadPrintFont(families ...string) *sfnt.Font {
	for _, family := range families {
		path := fontFile(family)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if face := firstFont(data); face != nil {
			return face
		}
	}
	return nil
}

func firstFont(data []byte) *sfnt.Font {
	if face, err := sfnt.Parse(data); err == nil {
		return face
	}
	collection, err := sfnt.ParseCollection(data)
	if err != nil || collection.NumFonts() == 0 {
		return nil
	}
	face, err := collection.Font(0)
	if err != nil {
		return nil
	}
	return face
}

func fontFile(family string) string {
	output, err := exec.Command("fc-match", "-f", "%{file}", family).Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(output))
	if path == "" || strings.Contains(path, "DejaVu") {
		return ""
	}
	return path
}

// measureWidth 返回 text 在给定字号（毫米）下占用的宽度。
func measureWidth(text string, sizeMM float64, key metricKey) float64 {
	if text == "" || sizeMM <= 0 {
		return 0
	}
	metricsOnce.Do(initMetrics)
	face := metricFonts[key]
	if !key.mono {
		if printFace := printFonts[key.fam]; printFace != nil {
			face = printFace
		}
	}
	if face == nil {
		return heuristicWidth(text, sizeMM)
	}
	ppem := fixed.Int26_6(sizeMM * 64)
	var buf sfnt.Buffer
	width := 0.0
	for _, r := range text {
		glyph, err := face.GlyphIndex(&buf, r)
		if err != nil || glyph == 0 {
			width += fallbackRuneWidth(r, sizeMM)
			continue
		}
		advance, err := face.GlyphAdvance(&buf, glyph, ppem, font.HintingNone)
		if err != nil {
			width += fallbackRuneWidth(r, sizeMM)
			continue
		}
		width += float64(advance) / 64
	}
	return width
}

// ascent 返回给定字号（毫米）下字体基线以上的高度。
func ascent(sizeMM float64, key metricKey) float64 {
	metricsOnce.Do(initMetrics)
	if value, ok := metricAsc[key]; ok {
		return value * sizeMM
	}
	return 0.8 * sizeMM
}

// descent 返回给定字号（毫米）下基线以下的深度。
func descent(sizeMM float64, key metricKey) float64 {
	metricsOnce.Do(initMetrics)
	if value, ok := metricDesc[key]; ok {
		return value * sizeMM
	}
	return 0.2 * sizeMM
}

func heuristicWidth(text string, sizeMM float64) float64 {
	width := 0.0
	for _, r := range text {
		width += fallbackRuneWidth(r, sizeMM)
	}
	return width
}

func fallbackRuneWidth(r rune, sizeMM float64) float64 {
	if isWideRune(r) {
		return sizeMM
	}
	return sizeMM * 0.5
}

// isWideRune 判断字符是否按全角宽度排版，用于无度量数据时的中文回退。
func isWideRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		(r >= 0x2E80 && r <= 0x2EFF) ||
		(r >= 0x3000 && r <= 0x303F) ||
		(r >= 0x3100 && r <= 0x312F) ||
		(r >= 0x3200 && r <= 0x4DBF) ||
		(r >= 0xA960 && r <= 0xA97F) ||
		(r >= 0xAC00 && r <= 0xD7FF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE1F) ||
		(r >= 0xFE30 && r <= 0xFE4F) ||
		(r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x20000 && r <= 0x3FFFD)
}
