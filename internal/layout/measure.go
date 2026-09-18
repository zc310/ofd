package layout

import (
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

// metricKey 唯一标识用于度量的一段字符样式。
type metricKey struct {
	bold   bool
	italic bool
	mono   bool
}

var (
	metricsOnce sync.Once
	metricFonts map[metricKey]*sfnt.Font
	metricAsc   map[metricKey]float64
	metricDesc  map[metricKey]float64
)

// initMetrics 惰性加载内置 Go 字体，只用于排版度量。生成 OFD 时使用逻辑字体，
// 不嵌入这些字体数据，以保证中文字符仍能回退到阅读器字体。
func initMetrics() {
	sources := map[metricKey][]byte{
		{bold: false, italic: false, mono: false}: goregular.TTF,
		{bold: true, italic: false, mono: false}:  gobold.TTF,
		{bold: false, italic: true, mono: false}:  goitalic.TTF,
		{bold: true, italic: true, mono: false}:   gobolditalic.TTF,
		{bold: false, italic: false, mono: true}:  gomono.TTF,
		{bold: true, italic: false, mono: true}:   gomono.TTF,
		{bold: false, italic: true, mono: true}:   gomono.TTF,
		{bold: true, italic: true, mono: true}:    gomono.TTF,
	}
	metricFonts = make(map[metricKey]*sfnt.Font, len(sources))
	metricAsc = make(map[metricKey]float64, len(sources))
	metricDesc = make(map[metricKey]float64, len(sources))
	const refPPEM = 1000 * 64
	for key, data := range sources {
		face, err := sfnt.Parse(data)
		if err != nil {
			continue
		}
		metricFonts[key] = face
		var buf sfnt.Buffer
		metrics, err := face.Metrics(&buf, refPPEM, font.HintingNone)
		if err != nil {
			metricAsc[key] = 0.8
			metricDesc[key] = 0.2
			continue
		}
		metricAsc[key] = float64(metrics.Ascent) / 64 / 1000
		metricDesc[key] = float64(metrics.Descent) / 64 / 1000
	}
}

// measureWidth 返回 text 在给定字号（毫米）下占用的宽度。
func measureWidth(text string, sizeMM float64, key metricKey) float64 {
	if text == "" || sizeMM <= 0 {
		return 0
	}
	metricsOnce.Do(initMetrics)
	face := metricFonts[key]
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
