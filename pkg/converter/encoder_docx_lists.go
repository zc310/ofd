package converter

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/docx"
	"github.com/zc310/ofd/internal/textdoc"
)

// 本文件从 OFD 的绝对定位文字里识别列表条目。OFD 没有列表语义，条目只是
// 「带编号前缀的行」，因此判断依据只能是行首文本形态，再加一段跨行的序号
// 一致性校验。
//
// 与既有零件的分工：encoder_markdown.go 的 numberedHeaderRegex 把「1.」
// 「1.2.」判成**标题**，markdownParagraphMarkerRegex 把「第X条」「（一）」
// 「一、」判成**段落起点**，两者都不产出列表语义。DOCX 侧另立一套规则，
// 带编号的行一律优先按条目处理——文档里「1.」开头的行通常是条目而不是章节。

// 列表标记的正则。分隔符之后必须跟空白或非 ASCII 字符，用来排除「1.0mm」
// 「4.0mm」这类单位紧跟的尺寸标注；RE2 不支持前瞻，后继字符在代码里判。
var (
	// arabicListRegex 匹配行首的阿拉伯数字序号，如「1.」「2)」。
	arabicListRegex = regexp.MustCompile(`^(\d+)\s*[.)、．]`)

	// chineseOrdinalRegex 匹配「一、」「二.」「（三）」「(四)」等中文序号。
	chineseOrdinalRegex = regexp.MustCompile(
		`^(?:([一二三四五六七八九十百]+)\s*[、.．]|[（(]([一二三四五六七八九十百]+)[)）])`)

	// bulletListRegex 匹配「·」「•」「▪」「-」「*」等项目符号。
	bulletListRegex = regexp.MustCompile(`^[·•▪●○◆■–—\-\*+]\s+`)

	// 多段序号（「1.2.」「1.2.3.」）刻意不识别。实测它们几乎都是章节号或目录
	// 条目：保密宣传册的目录是「1.1 …」「1.2 …」加点前导符与页码，项目文档里
	// 是「2016.07 2018.09」这样的日期行。把它们做成自动编号列表会丢掉点前导符
	// 与页码，还会把日期重新编号。
	//
	// chineseNumeral 是中文数字到阿拉伯数字的映射。
	chineseNumeral = map[rune]int{
		'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7,
		'八': 8, '九': 9, '十': 10, '百': 100,
	}
)

// listItem 是识别出的一个列表条目。
type listItem struct {
	// level 是层级，0 起算，映射到 w:ilvl。中文序号与阿拉伯序号都是一级：
	// 多级序号已经按上面的理由排除。
	level int
	// order 是本行的序号。
	order int
	// marker 标明判定依据（"arabic"/"chinese"/"bullet"），空串表示不是条目。
	marker string
}

// detectListItem 判断一行文字是否是列表条目，是则返回条目信息。
//
// 只看行首标记：条目序号一定出现在行首，而行内的顿号、括号、引号都不该被
// 当成序号。序号从 0 起的（「0.1mm」）在行级即可排除；「必须从 1 开始」是段级
// 要求，由 FilterCredibleListItems 校验。
func detectListItem(text string) (listItem, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return listItem{}, false
	}
	// 纯数字行是页码，不是条目。
	if pageNumberRegex.MatchString(trimmed) || dashPageNumberRegex.MatchString(trimmed) {
		return listItem{}, false
	}

	if loc := arabicListRegex.FindStringSubmatchIndex(trimmed); loc != nil {
		if !separatedFollowsMarker(trimmed, loc[1]) {
			return listItem{}, false
		}
		value, err := strconv.Atoi(capturedGroup(trimmed, loc, 2))
		if err != nil || value < 1 {
			return listItem{}, false
		}
		return listItem{level: 0, order: value, marker: "arabic"}, true
	}
	if loc := chineseOrdinalRegex.FindStringSubmatchIndex(trimmed); loc != nil {
		if !separatedFollowsMarker(trimmed, loc[1]) {
			return listItem{}, false
		}
		// 两个捕获组只有一个参与匹配，另一个的起止下标是 -1。
		digit := capturedGroup(trimmed, loc, 2)
		if digit == "" {
			digit = capturedGroup(trimmed, loc, 4)
		}
		order := parseChineseNumeral(digit)
		if order <= 0 {
			return listItem{}, false
		}
		return listItem{level: 0, order: order, marker: "chinese"}, true
	}
	if bulletListRegex.MatchString(trimmed) {
		// 项目符号没有序号，连续性无从验证；行级即认定，段级要求由
		// runCredible 放宽。
		return listItem{level: 0, order: 1, marker: "bullet"}, true
	}
	return listItem{}, false
}

// capturedGroup 取出第 group 个捕获组的文本。未参与匹配的组起止下标都是 -1，
// 返回空串。
func capturedGroup(text string, loc []int, group int) string {
	if loc[group] < 0 || loc[group+1] < 0 {
		return ""
	}
	return text[loc[group]:loc[group+1]]
}

// separatedFollowsMarker 判断序号分隔符之后是否跟空白或非 ASCII 字符。
//
// 「1.0mm」「4.0mm」的序号后紧跟单位数字，属于尺寸标注；「1. 建成」「1.建设」
// 才是条目。中文序号的「一、第一条」分隔符后是「第」，也必须放行。
func separatedFollowsMarker(text string, end int) bool {
	if end >= len(text) {
		// 行尾只有序号，正文为空，交给上层按空白处理。
		return true
	}
	next := rune(text[end])
	return next == ' ' || next == '\t' || next > 0x7F
}

// parseChineseNumeral 把「一」「十二」「二十」这类中文数字转成阿拉伯数字。
// 只覆盖列表序号的常见范围，解析失败返回 0。
func parseChineseNumeral(text string) int {
	runes := []rune(text)
	if len(runes) == 0 {
		return 0
	}
	if len(runes) == 1 {
		return chineseNumeral[runes[0]]
	}
	// 「十X」表示 10+X，「X十」表示 X*10，「X十Y」表示 X*10+Y。
	total, section := 0, 0
	for _, r := range runes {
		value, ok := chineseNumeral[r]
		if !ok {
			return 0
		}
		switch r {
		case '十':
			if section == 0 {
				section = 1
			}
			total += section * 10
			section = 0
		case '百':
			if section == 0 {
				section = 1
			}
			total += section * 100
			section = 0
		default:
			section = value
		}
	}
	return total + section
}

// FilterCredibleListItems 只保留可信连续段里的条目，其余清空。
//
// 按段筛选而不是整页一刀切：一页里可能只有部分行构成列表，其余是巧合。实测
// 项目立项报告首页的「一、」「二、」与紧随其后的「1. 2. 3.」就是两个独立列表。
func FilterCredibleListItems(items []listItem) []listItem {
	filtered := make([]listItem, len(items))
	for _, run := range listRuns(items) {
		if !run.credible() {
			continue
		}
		for _, index := range run.indexes {
			filtered[index] = items[index]
		}
	}
	return filtered
}

// listRun 是一段连续的条目行。
type listRun struct {
	// indexes 是段内各行在输入切片中的下标。
	indexes []int
	// items 是段内各行的条目，顺序与 indexes 一致。
	items []listItem
	// hasBullet 表示段内含项目符号行。
	hasBullet bool
}

// listRuns 把条目行切成连续段。序号回退到 1 也算断段：Word 的自动编号总是
// 从 1 渲染，「二、」紧接「1.」是两个列表，并成一段会把「二、」重编号成「1.」，
// 等于改写内容。
func listRuns(items []listItem) []listRun {
	var runs []listRun
	current := listRun{}
	previous := 0

	flush := func() {
		if len(current.indexes) > 0 {
			runs = append(runs, current)
		}
		current = listRun{}
		previous = 0
	}

	for i, item := range items {
		if item.marker == "" {
			flush()
			continue
		}
		if len(current.indexes) > 0 && item.marker != "bullet" && item.order != previous+1 {
			flush()
		}
		current.indexes = append(current.indexes, i)
		current.items = append(current.items, item)
		if item.marker == "bullet" {
			current.hasBullet = true
		} else {
			previous = item.order
		}
	}
	flush()
	return runs
}

// credible 判断一段是否构成可信列表。
//
// 必须从序号 1 开始：Word 的自动编号总是从 1 渲染，把「3. 4.」交给它会变成
// 「1. 2.」。跨页延续的列表因此漏判——单条序号的误报率太高（带单位的数值标注、
// 章节号），宁可漏判也不产出错误的自动编号。
//
// 编号行至少两条；项目符号没有序号，出现即认定。
func (r listRun) credible() bool {
	if len(r.indexes) == 0 || r.items[0].order != 1 {
		return false
	}
	return r.hasBullet || len(r.indexes) >= 2
}

// docxListProperties 返回把条目挂到 numbering.xml 上的段落属性。
func docxListProperties(item listItem) *docx.NumberingProps {
	return &docx.NumberingProps{Level: docx.IntVal(item.level), NumID: docx.IntVal(1)}
}

// stripListMarker 去掉条目行首的序号或项目符号，只保留正文。真实列表里序号由
// Word 自动渲染，留在正文里会显示两遍。
func stripListMarker(text string) string {
	// 只剥离被判定为条目的行首序号。多段序号（「1.2.」）不是条目，但会被
	// arabicListRegex 的单段分支匹配掉前半截，直接替换会留下「2. 二级条目」
	// 这样的残缺文本。
	if _, ok := detectListItem(text); !ok {
		return strings.TrimSpace(text)
	}
	for _, pattern := range []*regexp.Regexp{arabicListRegex, chineseOrdinalRegex, bulletListRegex} {
		if pattern.MatchString(text) {
			return strings.TrimSpace(pattern.ReplaceAllString(text, ""))
		}
	}
	return strings.TrimSpace(text)
}

// rowListMarker 返回行内第一个非空文字对象的文本。OFD 常把序号与正文拆成同一行
// 的多个文字对象，条目文字此时在第二个对象上，所以判定要看整行而不是单个对象。
func rowListMarker(row []textdoc.Entry) (string, bool) {
	for _, entry := range row {
		if text := strings.TrimSpace(entry.Text); text != "" {
			return text, true
		}
	}
	return "", false
}
