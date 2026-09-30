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
func completeTextCodes(codes []TextCode, value string, height, size, hScale float64, direction int, fontName string, fonts []Font, weight int, italic, completeDeltas bool) []TextCode {
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
		code := TextCode{Value: value, X: &x, Y: &y}
		if completeDeltas && runeCountOf(value) > 1 {
			face := findTextFace(fontName, fonts, size, weight, italic)
			code.DeltaX, code.DeltaY = makeTextCodeDeltas([]rune(value), size, hScale, direction, face)
		}
		return []TextCode{code}
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
			deltaX, deltaY := makeTextCodeDeltas([]rune(code.Value), size, hScale, direction, face)
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

// makeTextCodeDeltas 按字体度量计算字符间增量；字体缺失或该字形缺失时，退回
// 一 em 宽（size*hScale）作为默认字距。规格里字符的默认推进量就是一 em，用随
// 意一个替代字体度量反而会让字距与阅读端不一致。
func makeTextCodeDeltas(runes []rune, size, hScale float64, direction int, face *canvas.FontFace) ([]float64, []float64) {
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
	for index, r := range runes[:count] {
		advance := fallback
		// 只有字体真正包含该字形时才用字体度量：缺字时 TextWidth 返回的是
		// .notdef 的推进量（通常非 0），直接当字符宽度会让补出的 Delta 偏小，
		// 文字挤在一起（例如用西文字体排中文）。缺字时退回 em 宽的近似值。
		if faceHasGlyph(face, r) {
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
		if strings.TrimSpace(value.Name) != name && strings.TrimSpace(value.FamilyName) != name {
			continue
		}
		if len(value.Data) == 0 {
			// 只声明名称的系统字体：用它的族名（如 楷体）解析系统字体。
			if familyName := strings.TrimSpace(value.FamilyName); familyName != "" {
				name = familyName
			}
			break
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

// chineseFontGroups 把同一中文字体的中英文族名与常见文件名归为一组。系统字体
// 索引可能只认其中一种写法（例如 fontconfig 认「宋体」却不认「SimSun」），按组
// 尝试才能命中用户指定的字体，而不是误用别的中文字体。
var chineseFontGroups = []struct {
	names []string
	files []string
}{
	{[]string{"宋体", "宋体GB2312", "SimSun", "NSimSun"}, []string{"simsun.ttc", "simsun.ttf", "SimSun.ttf", "Songti.ttc"}},
	{[]string{"黑体", "黑体GB2312", "SimHei"}, []string{"simhei.ttf", "simhei.ttc", "SimHei.ttf"}},
	{[]string{"楷体", "楷体GB2312", "KaiTi", "SimKai"}, []string{"simkai.ttf", "SimKai.ttf"}},
	{[]string{"仿宋", "仿宋GB2312", "FangSong", "SimFang"}, []string{"simfang.ttf", "SimFang.ttf"}},
}

// chineseFontGroup 返回指定字体所属的中文字体组的中英文族名与候选文件名。
func chineseFontGroup(name string) (names, files []string, ok bool) {
	name = strings.TrimSpace(name)
	for _, group := range chineseFontGroups {
		for _, candidate := range group.names {
			if strings.EqualFold(candidate, name) {
				return group.names, group.files, true
			}
		}
	}
	return nil, nil, false
}

func loadSystemFontFamily(name string, style canvas.FontStyle) (family *canvas.FontFamily) {
	defer func() {
		if recover() != nil {
			family = nil
		}
	}()

	// 指定的中文字体：按同组的中英文族名和常见文件名依次尝试。
	if names, files, ok := chineseFontGroup(name); ok {
		for _, candidateStyle := range relaxFontStyles(style) {
			for _, candidate := range names {
				family := canvas.NewFontFamily(candidate)
				if family.LoadSystemFont(candidate, candidateStyle) == nil {
					return family
				}
			}
		}
		if path, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), files...); err == nil {
			family := canvas.NewFontFamily(name)
			if family.LoadFontFile(path, style) == nil {
				return family
			}
		}
		// 本机确实没有该中文字体：返回 nil，由调用方使用默认字距，而不是退到
		// 别的中文字体度量。
		return nil
	}

	for _, candidateStyle := range relaxFontStyles(style) {
		family := canvas.NewFontFamily(name)
		if family.LoadSystemFont(name, candidateStyle) == nil {
			return family
		}
	}
	for _, candidate := range []string{
		"Cantarell", "Noto Sans", "Noto Serif", "DejaVu Sans", "DejaVu Serif", "Times",
	} {
		for _, candidateStyle := range relaxFontStyles(style) {
			fallback := canvas.NewFontFamily(candidate)
			if fallback.LoadSystemFont(candidate, candidateStyle) == nil {
				return fallback
			}
		}
	}
	return nil
}

// relaxFontStyles 返回按优先级放宽的样式序列：某族没有请求的粗体/斜体字面时，
// 先用同族的常规字面度量（渲染端会对常规字形合成粗体/斜体），避免因为缺字面而
// 误用别的字体族。
func relaxFontStyles(style canvas.FontStyle) []canvas.FontStyle {
	styles := []canvas.FontStyle{style}
	if style&canvas.FontItalic != 0 {
		styles = append(styles, style&^canvas.FontItalic)
	}
	if style&canvas.FontBold != 0 {
		styles = append(styles, style&^canvas.FontBold, canvas.FontRegular)
	}
	return styles
}

// faceHasGlyph 判断字体是否真的包含该码位的字形。缺字时 TextWidth 会返回
// .notdef 的推进量（通常非 0），不能当作字符宽度。
func faceHasGlyph(face *canvas.FontFace, r rune) bool {
	return face != nil && face.Font != nil && face.Font.GlyphIndex(r) != 0
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
			copyText.TextCodes = completeTextCodes(copyText.TextCodes, copyText.Value, copyText.Boundary.Height, copyText.Size, copyText.HScale, copyText.ReadDirection, copyText.Font, fonts, copyText.Weight, copyText.Italic, completeDeltas)
			result.Items[clipIndex].Areas[areaIndex].Text = &copyText
		}
	}
	return result
}
