package creator

import (
	"math"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"
	"github.com/zc310/ofd/internal/utils"
)

const defaultTextSize = 4.2333333333

// completeTextCodes 补充 TextCode 的默认起点，并按选项补充字符位置增量。
func completeTextCodes(codes []TextCode, value string, width, height, size, hScale float64, direction int, fontName string, fonts []Font, weight int, italic, completeDeltas bool) []TextCode {
	baseline := height
	if baseline <= 0 {
		baseline = size
		if baseline == 0 {
			baseline = defaultTextSize
		}
	}
	if len(codes) == 0 {
		if runeCountOf(value) == 0 {
			return codes
		}
		// 单段整体文本：X/Y 必为默认值，直接构造，零多余拷贝。
		x := 0.0
		y := baseline
		return []TextCode{{Value: value, X: &x, Y: &y}}
	}
	// 已提供 codes：先扫描是否真的需要改写，不需要则原样返回（零分配）。
	changed := false
	for _, code := range codes {
		if code.X == nil || code.Y == nil {
			changed = true
			break
		}
	}
	if !changed && completeDeltas {
		for _, code := range codes {
			if runeCountOf(code.Value) > 1 && len(code.DeltaX) == 0 && len(code.DeltaY) == 0 {
				changed = true
				break
			}
		}
	}
	if !changed {
		return codes
	}
	result := append([]TextCode(nil), codes...)
	total := 0
	for _, code := range result {
		total += runeCountOf(code.Value)
	}
	for index := range result {
		code := &result[index]
		if code.X == nil {
			x := 0.0
			code.X = &x
		}
		if code.Y == nil {
			y := baseline
			code.Y = &y
		}
		if completeDeltas && total > 1 && runeCountOf(code.Value) > 1 && len(code.DeltaX) == 0 && len(code.DeltaY) == 0 {
			face := findTextFace(fontName, fonts, size, weight, italic)
			deltaX, deltaY := makeTextCodeDeltas([]rune(code.Value), width, size, hScale, direction, total, face)
			code.DeltaX = deltaX
			code.DeltaY = deltaY
		}
	}
	return result
}

// runeCountOf 统计字符串的字符数；对 ASCII 快路径直接取 len，否则交给
// utf8.RuneCountInString，全程不分配。
func runeCountOf(value string) int {
	for i := 0; i < len(value); i++ {
		if value[i] >= utf8.RuneSelf {
			return utf8.RuneCountInString(value[i:])
		}
	}
	return len(value)
}

func makeTextCodeDeltas(runes []rune, width, size, hScale float64, direction, total int, face *canvas.FontFace) ([]float64, []float64) {
	count := len(runes) - 1
	deltaX := make([]float64, count)
	deltaY := make([]float64, count)
	if count == 0 {
		return deltaX, deltaY
	}
	if size == 0 {
		size = defaultTextSize
	}
	if hScale == 0 {
		hScale = 1
	}
	fallback := size * hScale
	if face == nil && width > 0 && total > 0 {
		fallback = width / float64(total)
	}
	for index, r := range runes[:count] {
		advance := fallback
		if face != nil {
			advance = face.TextWidth(string(r)) * hScale
		}
		if !finiteTextCodeNumber(advance) || advance == 0 {
			advance = fallback
		}
		switch normalizeTextCodeDirection(direction) {
		case 90:
			deltaY[index] = advance
		case 180:
			deltaX[index] = -advance
		case 270:
			deltaY[index] = -advance
		default:
			deltaX[index] = advance
		}
	}
	return deltaX, deltaY
}

func findTextFace(name string, fonts []Font, size float64, weight int, italic bool) *canvas.FontFace {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "SimSun"
	}
	if size == 0 {
		size = defaultTextSize
	}
	style := canvas.FontRegular
	if weight >= 650 {
		style = canvas.FontBold
	}
	if italic {
		style |= canvas.FontItalic
	}
	for _, value := range fonts {
		if strings.TrimSpace(value.Name) != name || len(value.Data) == 0 {
			continue
		}
		family := canvas.NewFontFamily(name)
		if err := family.LoadFont(value.Data, 0, style); err == nil {
			return family.Face(size*2.83465, canvas.Black, style, canvas.FontNormal)
		}
		return nil
	}
	// 未提供字体文件时，按渲染器相同的顺序查找系统字体。
	if runtime.GOOS == "js" {
		return nil
	}
	if family := loadSystemFontFamily(name, style); family != nil {
		return family.Face(size*2.83465, canvas.Black, style, canvas.FontNormal)
	}
	return nil
}

func loadSystemFontFamily(name string, style canvas.FontStyle) (family *canvas.FontFamily) {
	defer func() {
		if recover() != nil {
			family = nil
		}
	}()

	family = canvas.NewFontFamily(name)
	if family.LoadSystemFont(name, style) == nil {
		return family
	}

	if strings.EqualFold(name, "宋体") || strings.EqualFold(name, "simsun") {
		if path, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simsun.ttc"); err == nil && family.LoadFontFile(path, style) == nil {
			return family
		}
	}
	if strings.EqualFold(name, "黑体") || strings.EqualFold(name, "simhei") {
		if path, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf"); err == nil && family.LoadFontFile(path, style) == nil {
			return family
		}
	}

	for _, candidate := range []string{
		"仿宋", "FangSong", "NSimSum", "楷体", "KaiTi", "黑体", "SimHei",
		"Noto Sans CJK SC", "WenQuanYi Micro Hei", "Cantarell", "Noto Sans",
		"Noto Serif", "DejaVu Sans", "DejaVu Serif", "Times",
	} {
		fallback := canvas.NewFontFamily(candidate)
		if fallback.LoadSystemFont(candidate, style) == nil {
			return fallback
		}
	}
	return nil
}

func normalizeTextCodeDirection(direction int) int {
	direction %= 360
	if direction < 0 {
		direction += 360
	}
	switch {
	case direction >= 45 && direction < 135:
		return 90
	case direction >= 135 && direction < 225:
		return 180
	case direction >= 225 && direction < 315:
		return 270
	default:
		return 0
	}
}

func finiteTextCodeNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func completeClipsTextCodes(clips *Clips, fonts []Font, completeDeltas bool) *Clips {
	if clips == nil {
		return nil
	}
	result := &Clips{Items: append([]Clip(nil), clips.Items...)}
	for clipIndex := range result.Items {
		result.Items[clipIndex].Areas = append([]ClipArea(nil), result.Items[clipIndex].Areas...)
		for areaIndex := range result.Items[clipIndex].Areas {
			text := result.Items[clipIndex].Areas[areaIndex].Text
			if text == nil {
				continue
			}
			copyText := *text
			copyText.TextCodes = completeTextCodes(copyText.TextCodes, copyText.Value, copyText.Boundary.Width, copyText.Boundary.Height, copyText.Size, copyText.HScale, copyText.ReadDirection, copyText.Font, fonts, copyText.Weight, copyText.Italic, completeDeltas)
			result.Items[clipIndex].Areas[areaIndex].Text = &copyText
		}
	}
	return result
}
