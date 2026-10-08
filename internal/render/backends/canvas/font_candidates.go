package canvas

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/utils"
)

// 本文件实现系统字体候选匹配：PostScript 名别名、通用字体族分类、CJK 字体组
// 到系统文件的映射、等宽字体回退，以及回退字体名归一化。

var systemFontCandidatesCache sync.Map

func systemFontCandidates(ft *models.Font) []string {
	if cached, ok := systemFontCandidatesCache.Load(ft); ok {
		return cached.([]string)
	}
	names := make([]string, 0, 5)
	seen := make(map[string]bool, 5)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(ft.FamilyName)
	add(postscriptFamilyAlias(ft.FamilyName))
	add(postscriptFamilyAlias(ft.FontName))
	if cjkFontGroup(ft) == "" {
		switch classifyFontClass(ft) {
		case "serif":
			add("serif")
		case "sans-serif":
			add("sans-serif")
		case "monospace":
			add("monospace")
		}
	}
	systemFontCandidatesCache.Store(ft, names)
	return names
}

// postscriptFamilyAlias 把常见 PostScript 子集字体名映射到 fontconfig 家族名。
// 例如 NimbusRomNo9L-* 对应系统上的 Nimbus Roman。
func postscriptFamilyAlias(name string) string {
	if name == "" {
		return ""
	}
	if index := strings.IndexByte(name, '+'); index >= 0 {
		name = name[index+1:]
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "nimbusromno9l"), strings.HasPrefix(lower, "nimbusromanno9l"):
		return "Nimbus Roman"
	case strings.HasPrefix(lower, "nimbusrom"):
		return "Nimbus Roman"
	case strings.HasPrefix(lower, "nimbussan"):
		return "Nimbus Sans"
	case strings.HasPrefix(lower, "nimbusmon"):
		return "Nimbus Mono PS"
	default:
		return ""
	}
}

// classifyFontClass 依据 OFD 的 Serif/FixedWidth 标志与族名推断通用字体类别。
func classifyFontClass(ft *models.Font) string {
	if ft.FixedWidth {
		return "monospace"
	}
	if ft.Serif {
		return "serif"
	}
	lower := strings.ToLower(ft.FamilyName + " " + ft.FontName)
	switch {
	case strings.Contains(lower, "nimbusmon"), strings.Contains(lower, "mono"),
		strings.Contains(lower, "courier"), strings.Contains(lower, "typewriter"),
		strings.Contains(lower, "cmtt"):
		return "monospace"
	case strings.Contains(lower, "nimbussan"), strings.Contains(lower, "sans"),
		strings.Contains(lower, "helvetica"), strings.Contains(lower, "arial"),
		strings.Contains(lower, "gothic"):
		return "sans-serif"
	case strings.Contains(lower, "nimbusrom"), strings.Contains(lower, "roman"),
		strings.Contains(lower, "serif"), strings.Contains(lower, "times"),
		strings.Contains(lower, "cmr"), strings.Contains(lower, "cmsy"),
		strings.Contains(lower, "cmmi"), strings.Contains(lower, "bookman"),
		strings.Contains(lower, "schoolbook"), strings.Contains(lower, "georgia"):
		return "serif"
	default:
		return ""
	}
}

func sameFallbackName(left, right string) bool {
	left = normalizeFallbackName(left)
	right = normalizeFallbackName(right)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	return fallbackNameGroup(left) != "" && fallbackNameGroup(left) == fallbackNameGroup(right)
}

func normalizeFallbackName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(name)
	return name
}

func fallbackNameGroup(name string) string {
	switch name {
	case "宋体", "宋体gb2312", "simsun", "nsimsun", "songti", "simsungb2312", "songtigb2312",
		"方正小标宋", "方正小标宋gbk", "方正书宋", "fzxbs":
		return "simsun"
	case "华文宋体", "stsong":
		return "stsong"
	case "黑体", "黑体gb2312", "simhei", "heiti", "hei", "microsoftheiti", "heitisc", "方正黑体", "方正黑体gbk":
		return "simhei"
	case "华文黑体", "stheiti":
		return "stheiti"
	case "楷体", "楷体gb2312", "simkai", "kaiti", "kaishu", "kaitigb2312", "方正楷体", "方正楷体gbk":
		return "simkai"
	case "华文楷体", "stkaiti":
		return "stkaiti"
	case "仿宋", "仿宋gb2312", "simfang", "fangsong", "fangsonggb2312", "方正仿宋", "方正仿宋gbk":
		return "simfang"
	case "华文仿宋", "stfangsong":
		return "stfangsong"
	case "微软雅黑", "microsoftyahei", "microsoftyaheiui", "msyh", "yahei":
		return "yahei"
	case "微软正黑", "microsoftjhenghei", "microsoftjhengheiui", "msjh":
		return "jhenghei"
	case "等线", "dengxian":
		return "dengxian"
	case "思源黑体", "sourcehansanssc", "sourcehansanscn", "notosanssc", "notosanscjksc", "ofdnotosanssc":
		return "sanssc"
	case "思源宋体", "sourcehanserifsc", "sourcehanserifcn", "notoserifsc", "notoserifcjksc":
		return "serifsc"
	case "smileysans", "smileysansoblique", "得意黑":
		return "smileysans"
	default:
		return ""
	}
}

// cjkFontFiles 把中文字体组映射到系统字体目录中可能存在的字体文件名。
// 收录常见宋体/黑体/楷体/仿宋/雅黑，以及「得意黑」（Smiley Sans）；其他字体组
// 没有通用文件名时不映射。
var cjkFontFiles = map[string][]string{
	"simsun":     {"simsun.ttc", "simsun.ttf", "simsunb.ttf", "Songti.ttc"},
	"simhei":     {"simhei.ttf", "simhei.ttc"},
	"simkai":     {"simkai.ttf"},
	"simfang":    {"simfang.ttf"},
	"yahei":      {"msyh.ttc", "msyh.ttf", "msyhbd.ttc", "msyhl.ttc"},
	"smileysans": {"SmileySans-Oblique.ttf", "SmileySans-Oblique.otf", "SmileySans.ttf", "SmileySans.otf"},
}

// cjkFontGroup 从字体族名和字体名中识别中文字体组；只有该组在
// cjkFontFiles 中有候选字体文件时才返回组名。
func cjkFontGroup(ft *models.Font) string {
	for _, name := range []string{ft.FamilyName, ft.FontName} {
		group := fallbackNameGroup(normalizeFallbackName(name))
		if group == "" {
			continue
		}
		if _, ok := cjkFontFiles[group]; ok {
			return group
		}
	}
	return ""
}

// cjkFallbackFamilies 是覆盖中日韩统一表意文字的常见系统族名，按优先级排列。
// fontconfig 会把通用的 sans-serif/serif 解析成 Latin-only 字体（通常是
// DejaVu 系），中文会整片变成豆腐块；这些族名只在已选字体确实缺中文字形时
// 才作为替代被试，不改变原有候选顺序。
var cjkFallbackFamilies = []string{
	"Noto Sans CJK SC",
	"Noto Sans SC",
	"Source Han Sans SC",
	"思源黑体",
	"WenQuanYi Zen Hei",
	"WenQuanYi Micro Hei",
	"Microsoft YaHei",
	"PingFang SC",
	"SimHei",
	"Hiragino Sans GB",
	"Noto Serif CJK SC",
	"Noto Serif SC",
	"SimSun",
}

// cjkFixedWidthFamilies 是既有等宽度量又能覆盖中文的常见系统族名。
// 等宽族里 Latin-only 的候选（DejaVu Sans Mono、Nimbus Mono PS 等）很常见，
// 而 OFD 的等宽字体常常承载中文注释，因此单独列一份带 CJK 的候选。
var cjkFixedWidthFamilies = []string{
	"Noto Sans Mono CJK SC",
	"Source Han Mono",
	"Sarasa Mono SC",
	"Sarasa Mono",
	"Sarasa Gothic",
	"Maple Mono NF CN",
	"Noto Sans Mono CJK TC",
}

// cjkProbeRune 是判断字体是否覆盖中文的探测字符（"中"）。
const cjkProbeRune = '中'

// cjkProbeSize 是覆盖探测用的字号。只需能构造出字体面，字号不影响字形查找。
const cjkProbeSize = 12.0

// familyCoversCJK 判断字体族在给定样式下是否真的有中文字形。
// FontFamily 按 style 存放字体，同一 style 再次载入会覆盖前一个，因此这里
// 只能检查该 style 当前实际生效的那份字体，不能指望同族多字体逐字形回退。
func familyCoversCJK(family *canvas.FontFamily, style drawing.FontStyle) bool {
	if family == nil {
		return false
	}
	face := family.Face(cjkProbeSize, canvas.Black, canvasStyle(style), canvas.FontNormal)
	if face == nil || face.Font == nil {
		return false
	}
	return face.Font.GlyphIndex(cjkProbeRune) != 0
}

// familyCoversText 判断字体族能否画出对象里的全部字符。
//
// 按字体名或文档内容去猜「这份文档有没有中文」都不可靠：Markdown 转出来的
// OFD 声明的逻辑字体是 sans-serif/Consolas这类通用名，名字里没有任何中文
// 线索，但正文全是中文。字形覆盖是唯一可靠的判据——已经画不出来的字符，
// 换哪个候选都一样 tofu，不如换个真能画的。
//
// 探测按去重后的字符集做，并限制上限：OFD 文字对象可能整页一段文字，逐字符
// 查表在大文档上是热点。超过上限就认为覆盖不足，宁可多换一次字体，也不要
// 在明显缺字的文档上继续用 Latin-only 字体。
func familyCoversText(family *canvas.FontFamily, object models.TextObject, style drawing.FontStyle) bool {
	if family == nil {
		return false
	}
	face := family.Face(cjkProbeSize, canvas.Black, canvasStyle(style), canvas.FontNormal)
	if face == nil || face.Font == nil {
		return false
	}
	seen := make(map[rune]bool, 32)
	for _, code := range object.TextCode {
		for _, r := range code.Value {
			if seen[r] {
				continue
			}
			if face.Font.GlyphIndex(r) == 0 {
				return false
			}
			seen[r] = true
			if len(seen) >= textCoverageProbeLimit {
				return true
			}
		}
	}
	return true
}

// textCoverageProbeLimit 是单个文字对象做字形覆盖探测的最大字符数。
const textCoverageProbeLimit = 512

// fixedWidthFontFamilies 是常见等宽字体的系统族名，按优先级排列。
var fixedWidthFontFamilies = []string{
	"Noto Sans Mono CJK SC",
	"Noto Sans Mono",
	"DejaVu Sans Mono",
	"Liberation Mono",
	"Noto Mono",
	"Ubuntu Mono",
	"Menlo",
	"Monaco",
	"Consolas",
	"Courier New",
	"Source Code Pro",
	"Fira Code",
	"JetBrains Mono",
	"Cascadia Mono",
	"Roboto Mono",
	"FreeMono",
}

// fixedWidthFontFiles 是系统字体目录中常见的等宽字体文件名。
var fixedWidthFontFiles = []string{
	"DejaVuSansMono.ttf",
	"DejaVuSansMono-Bold.ttf",
	"LiberationMono-Regular.ttf",
	"LiberationMono-Bold.ttf",
	"NotoSansMonoCJKsc-Regular.otf",
	"NotoSansMono-Regular.ttf",
	"UbuntuMono-R.ttf",
	"FreeMono.ttf",
	"SourceCodePro-Regular.ttf",
}

// loadFixedWidthFont 为逻辑等宽字体选择系统等宽字体。OFD 的 FontName 通常是
// 资源名，无法直接匹配系统字体，因此这里依据 FixedWidth 标志和族名中的
// monospace 线索，按族名或常见字体文件加载等宽字体。
func loadFixedWidthFont(ft *models.Font, style drawing.FontStyle) (*canvas.FontFamily, bool) {
	if ft == nil || !isFixedWidthFont(ft) {
		return nil, false
	}
	family := canvas.NewFontFamily("fixed-width")
	if name := strings.TrimSpace(ft.FamilyName); !isGenericFontFamily(name) {
		if err := family.LoadSystemFont(name, canvasStyle(style)); err == nil {
			return family, true
		}
	}
	for _, name := range fixedWidthFontFamilies {
		if err := family.LoadSystemFont(name, canvasStyle(style)); err == nil {
			return family, true
		}
	}
	if path, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), fixedWidthFontFiles...); err == nil {
		slog.Debug("load fallback fixed-width font file", "path", path, "style", style)
		if loadFontFileSafely(family, path) {
			return family, true
		}
	}
	return nil, false
}

// isFixedWidthFont 判断逻辑字体是否应按等宽字体处理。
func isFixedWidthFont(ft *models.Font) bool {
	if ft.FixedWidth {
		return true
	}
	return isFixedWidthName(ft.FamilyName) || isFixedWidthName(ft.FontName)
}

func isFixedWidthName(name string) bool {
	normalized := normalizeFallbackName(name)
	if normalized == "" {
		return false
	}
	switch normalized {
	case "monospace", "mono", "fixed", "fixedwidth", "courier", "couriernew", "consolas", "menlo", "monaco":
		return true
	}
	return strings.Contains(normalized, "mono")
}

func isGenericFontFamily(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "monospace", "mono", "fixed", "fixedwidth", "sans-serif", "sansserif", "serif", "cursive", "fantasy":
		return true
	default:
		return false
	}
}

// RegisterFallbackFont 把回退字体注册到进程级全局注册表。
// 同一字体族全局只保存一份解析结果和一份字体数据引用，重复注册幂等
// （首个 RenderDocument/Fonts 完成解析后即锁定）。注册后通过
// Fonts.UseFallbackFont 或 Document.UseFallbackFont 应用到具体文档；
// 没有其他回退字体时，缺失字体默认使用该全局字体族。
