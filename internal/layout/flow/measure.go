package flow

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
)

// famEnum 标识公文版式引用的字族类别，用于把 OFD 字体资源指向标准字体族。
type famEnum uint8

const (
	// famBody 是正文或缺省字族。
	famBody famEnum = iota
	// famHei 是黑体，用于逻辑字体的度量回退。
	famHei
	// famKai 是楷体，用于逻辑字体的度量回退。
	famKai
	// famTitle 是小标宋，用于逻辑字体的度量回退。
	famTitle
	// famSong 是宋体，用于逻辑字体的度量回退。
	famSong
)

// metricKey 唯一标识用于度量的一段字符样式。
type metricKey struct {
	font   string
	bold   bool
	italic bool
	mono   bool
	fam    famEnum
}

var (
	metricsOnce sync.Once
	metricFonts map[metricKey]*sfnt.Font
	printFonts  map[famEnum]*sfnt.Font
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
	for fam := famBody; fam <= famSong; fam++ {
		for key, data := range latin {
			base := key
			base.fam = fam
			face, _, _ := metricFace(data)
			if face == nil {
				continue
			}
			metricFonts[base] = face
		}
	}
}

// 系统候选可能是 TTC/OTC，度量同样选择第一个字体面；不把集合字节当作单字体解析。
func metricFace(data []byte) (*sfnt.Font, float64, float64) {
	face := firstFont(data)
	if face == nil {
		return nil, 0, 0
	}
	var buffer sfnt.Buffer
	metrics, err := face.Metrics(&buffer, 1000*64, font.HintingNone)
	if err != nil {
		return face, 0.8, 0.2
	}
	return face, float64(metrics.Ascent) / 64000, float64(metrics.Descent) / 64000
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
