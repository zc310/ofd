package textdoc

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// 本文件是 OFD 页面到「结构化块」的版面推断：标题层级、正文基准字号、段落边界、
// 页码行识别。这些判断针对的是 OFD 的版面本身，与输出格式无关，因此放在这里而不是
// 某个编码器里——Markdown、DOCX 以及将来的其它结构化输出都复用同一套判定，
// 不会各写一份而互相漂移。

// 标题层级的字号梯度。行字号达到正文字号的几倍即视为对应层级的标题。
const (
	h1Ratio = 2.0
	h2Ratio = 1.75
	h3Ratio = 1.5
	h4Ratio = 1.3
	h5Ratio = 1.15
)

var (
	// NumberedHeaderRegex 匹配「1.」「1.2.」「1.2.3.」等形式的多级编号标题。
	NumberedHeaderRegex = regexp.MustCompile(`^(\d+\.)(?:\d+\.)*`)

	// ParagraphMarkerRegex 匹配常见的中文段落起始标记。
	ParagraphMarkerRegex = regexp.MustCompile(
		`^(第\s*[0-9一二三四五六七八九十百千零两]+\s*[条编]|[（(][一二三四五六七八九十百0-9]+[）)]|[一二三四五六七八九十百]+、)`)

	// PageNumberRegex 匹配纯页码，例如「12」。
	PageNumberRegex = regexp.MustCompile(`^\d{1,3}$`)

	// DashPageNumberRegex 匹配带破折号的页码，例如「- 12 -」「— 12 —」。
	DashPageNumberRegex = regexp.MustCompile(`^[—–\-]\s*\d+\s*[—–\-]$`)
)

// RowInfo 保存页内一行的文本与标题判定结果。
type RowInfo struct {
	Text    string
	MaxSize float64
	// Level 是标题层级，1..6；0 表示不是标题。
	Level int
	// IsTitle 等价于 Level > 0。
	IsTitle bool
	// IsList 由 DOCX 的列表识别写入，其他输出格式不使用，保持 false。
	IsList bool
}

// BodyFontSize 返回一页的正文基准字号：出现次数最多的字号，平票取较小值。
// 找不到有效字号时兜底 6mm。
func BodyFontSize(entries []Entry) float64 {
	fontSizes := make(map[float64]int)
	for _, entry := range entries {
		if entry.Size > 0 {
			fontSizes[entry.Size]++
		}
	}
	maxCount := 0
	bodyFontSize := 6.0
	for size, count := range fontSizes {
		if count > maxCount || (count == maxCount && size < bodyFontSize) {
			maxCount = count
			bodyFontSize = size
		}
	}
	return bodyFontSize
}

// DetectHeadingLevel 判断一行文字的标题层级，不是标题时返回 0。
//
// 编号标题优先于字号：「1.2 引言」无论字号多大都是标题。字号派生的层级还要过
// 两道否决——首字符必须是大写字母、数字或汉字（避免把以标点开头的正文误判），
// 且不能是正文形态（过长或以句末标点结尾）。
func DetectHeadingLevel(text string, fontSize, bodyFontSize float64) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	if IsPageNumber(trimmed) {
		return 0
	}
	if isNumbered, level := isNumberedHeader(trimmed); isNumbered {
		return level
	}

	level := calculateHeaderLevel(fontSize, bodyFontSize)
	if level > 0 {
		firstRune := rune(trimmed[0])
		if !(firstRune >= 'A' && firstRune <= 'Z') &&
			!(firstRune >= '0' && firstRune <= '9') &&
			!(firstRune >= 0x4e00 && firstRune <= 0x9fff) {
			return 0
		}
		if isBodyText(trimmed) {
			return 0
		}
		return level
	}
	if isBoldHeading(trimmed) {
		return 4
	}
	return 0
}

// ParagraphStarts 判断每个正文行是否为段落起点。段落边界按以下信号：
//  1. 段首缩进：行左边缘比正文左边距多出约一个字符宽；
//  2. 段落标记：第 X 条、第 X 编、(一)、一、 等；
//  3. 纵向间距明显大于页内常见行距（部分文档使用段间距而非缩进）。
func ParagraphStarts(rows [][]Entry, inTable []bool, rowInfos []RowInfo) []bool {
	starts := make([]bool, len(rows))

	margins := make(map[float64]int)
	for i, row := range rows {
		if inTable[i] || rowInfos[i].IsTitle {
			continue
		}
		margins[math.Round(RowLeft(row)*2)/2]++
	}
	// 取出现次数最多的左边距作为正文左边距；次数相同时取更小值，保证输出
	// 与 map 迭代顺序无关（否则段落判定会随运行变化）。
	margin, marginCount := 0.0, -1
	for value, count := range margins {
		if count > marginCount || (count == marginCount && value < margin) {
			margin, marginCount = value, count
		}
	}

	pageRight := 0.0
	for i, row := range rows {
		if inTable[i] || rowInfos[i].IsTitle {
			continue
		}
		if right := RowRight(row); right > pageRight {
			pageRight = right
		}
	}

	var gaps []float64
	previousY := math.NaN()
	for i, row := range rows {
		if inTable[i] || rowInfos[i].IsTitle {
			previousY = math.NaN()
			continue
		}
		y := RowTop(row)
		if !math.IsNaN(previousY) && y > previousY {
			gaps = append(gaps, y-previousY)
		}
		previousY = y
	}
	typicalGap := median(gaps)

	previousY = math.NaN()
	for i, row := range rows {
		if inTable[i] || rowInfos[i].IsTitle {
			previousY = math.NaN()
			continue
		}
		size := rowInfos[i].MaxSize
		indent := RowLeft(row) - margin
		// 段首缩进要求该行是通栏行，避免把居中的标题/落款逐行拆成段落。
		fullWidth := pageRight > 0 && RowRight(row) >= pageRight-math.Max(3, size)
		switch {
		case indent > math.Max(2.5, size) && fullWidth:
			starts[i] = true
		case ParagraphMarkerRegex.MatchString(rowInfos[i].Text):
			starts[i] = true
		case typicalGap > 0 && !math.IsNaN(previousY):
			if gap := RowTop(row) - previousY; gap > typicalGap*1.5 {
				starts[i] = true
			}
		}
		previousY = RowTop(row)
	}
	return starts
}

// IsPageNumberRow 判断一行是否为页码行。只有页码形式的文本且位于页面顶部或
// 底部（页眉/页脚区域）时才认定为页码，避免把表格或数据中的纯数字行删掉。
func IsPageNumberRow(row []Entry, pageHeight float64) bool {
	if !IsPageNumber(strings.TrimSpace(JoinText(row))) {
		return false
	}
	if !Finite(pageHeight) || pageHeight <= 0 {
		return true
	}
	y := RowTop(row)
	return y < pageHeight*0.12 || y > pageHeight*0.88
}

// IsPageNumber 判断文本是否为页码形式（纯数字或两侧带破折号的数字）。
func IsPageNumber(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return PageNumberRegex.MatchString(trimmed) || DashPageNumberRegex.MatchString(trimmed)
}

func isNumberedHeader(text string) (bool, int) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, 0
	}
	matches := NumberedHeaderRegex.FindStringSubmatch(trimmed)
	if len(matches) > 0 {
		marker := matches[1]
		dotCount := min(strings.Count(marker, ".")+2, 6)
		if dotCount > 0 {
			return true, dotCount
		}
	}
	return false, 0
}

func calculateHeaderLevel(fontSize, bodyFontSize float64) int {
	if bodyFontSize <= 0 || fontSize <= 0 {
		return 0
	}
	ratio := fontSize / bodyFontSize
	switch {
	case ratio >= h1Ratio:
		return 1
	case ratio >= h2Ratio:
		return 2
	case ratio >= h3Ratio:
		return 3
	case ratio >= h4Ratio:
		return 4
	case ratio >= h5Ratio:
		return 5
	case ratio >= 1.0:
		return 6
	}
	return 0
}

func isBodyText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > 80 {
		return true
	}
	return strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "。") ||
		strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, "？") ||
		strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "！")
}

// isBoldHeading 判断一行是否是 Markdown 风格的加粗小标题（整行被 ** 包裹）。
func isBoldHeading(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 3 || len(trimmed) > 100 {
		return false
	}
	if !strings.HasPrefix(trimmed, "**") || !strings.HasSuffix(trimmed, "**") {
		return false
	}
	inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	if len(inner) < 3 || len(inner) > 100 {
		return false
	}
	if strings.Contains(inner, "\n") {
		return false
	}
	if len(inner) > 0 {
		first := inner[0]
		if first >= '0' && first <= '9' {
			return false
		}
	}
	last := inner[len(inner)-1]
	if last == '.' || last == ',' || last == ';' {
		return false
	}
	return true
}

// median 返回中位数。空输入返回 0。
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
