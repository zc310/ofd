package converter

import (
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/zc310/ofd/internal/render"
)

// 本文件给 HTML 输出加一层透明文字层：页面图像负责视觉，文字层负责可选中、
// 浏览器内查找与朗读。文字设为 color:transparent，因此不内嵌任何字体也不影响
// 外观——与 cmd/ofd-wasm 的阅读器是同一套方案（index.html 的 .text-run）。
//
// 字号必须随页面尺寸等比缩放，否则浏览器用系统字体的度量与 OFD 版面对不上，
// 文字会被 overflow:hidden 裁掉、选不中。写两条 font-size：
//
//	font-size:3.5mm                    不支持容器查询时按毫米定位（页面本就是固定毫米）
//	font-size:calc(1.6667 * 1cqw)     支持时按容器宽度换算，页面缩放后仍然对齐
//
// 1cqw 是容器内联尺寸的 1%，所以 size/pageWidth×100×1cqw 在容器等于页宽时正好
// 等于 size 毫米；页面若被 max-width 之类的规则缩小，字号会跟着缩。

// textRunSeg 是合并后的一个文字片段，对应一个 w:span。
type textRunSeg struct {
	// text 是片段文本。
	text string
	// x/y 是片段左上角（毫米，原点页面左上角）。
	x, y float64
	// width/height 是片段尺寸（毫米）。
	width, height float64
	// size 是字号（毫米），用于换算 CSS 字号。
	size float64
	// bold/italic 与 angle 分别是粗体、斜体与旋转角（度）。
	bold, italic bool
	angle        float64
}

// writeHTMLTextLayer 输出一页的文字层。pageWidthMM 与 pageHeightMM 是页面物理
// 尺寸（毫米），用于把绝对坐标换算成百分比与 cqw 字号。
func writeHTMLTextLayer(output io.Writer, layouts []render.TextLayout, pageWidthMM, pageHeightMM float64) error {
	if len(layouts) == 0 || !(pageWidthMM > 0) || !(pageHeightMM > 0) {
		return nil
	}
	segments := nonEmptySegments(mergeTextRuns(layouts))
	if len(segments) == 0 {
		return nil
	}
	if _, err := io.WriteString(output, "<div class=\"text-layer\">"); err != nil {
		return err
	}
	for index := range segments {
		seg := &segments[index]
		cqw := seg.size / pageWidthMM * 100
		if _, err := fmt.Fprintf(output,
			"<span class=\"text-run\" style=\"left:%s%%;top:%s%%;width:%s%%;height:%s%%;font-size:%smm;font-size:calc(%s * 1cqw)\"",
			htmlNumber(seg.x/pageWidthMM*100),
			htmlNumber(seg.y/pageHeightMM*100),
			htmlNumber(seg.width/pageWidthMM*100),
			htmlNumber(seg.height/pageHeightMM*100),
			htmlNumber(seg.size),
			htmlNumber(cqw),
		); err != nil {
			return err
		}
		if seg.bold {
			if _, err := io.WriteString(output, ";font-weight:700"); err != nil {
				return err
			}
		}
		if seg.italic {
			if _, err := io.WriteString(output, ";font-style:italic"); err != nil {
				return err
			}
		}
		if seg.angle != 0 {
			if _, err := fmt.Fprintf(output, ";transform-origin:top left;transform:rotate(%sdeg)", htmlNumber(seg.angle)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(output, ">"+html.EscapeString(seg.text)+"</span>"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(output, "</div>\n")
	return err
}

// nonEmptySegments 去掉纯空白的片段，避免输出没有文字的空 span。
func nonEmptySegments(segments []textRunSeg) []textRunSeg {
	kept := make([]textRunSeg, 0, len(segments))
	for _, seg := range segments {
		if strings.TrimSpace(seg.text) != "" {
			kept = append(kept, seg)
		}
	}
	return kept
}

// mergeTextRuns 把相邻可合并的文字片段并成一个 span。
//
// OFD 里一句话常被拆成许多 TextCode，逐个建 span 会让 DOM 膨胀到几万个节点。
// 合并条件与 cmd/ofd-wasm 的 textLayerSegments 一致：字号、字重、斜体、旋转角
// 相同，纵向基线接近，且水平方向紧邻。
func mergeTextRuns(layouts []render.TextLayout) []textRunSeg {
	segments := make([]textRunSeg, 0, len(layouts))
	for _, layout := range layouts {
		// Size 是 OFD 声明的字号，正常总为正；但它为 0 时用行高兜底，
		// 否则 CSS 会算出 0 字号，文字既看不见也选不中。
		size := layout.Size
		if !(size > 0) {
			size = layout.Height
		}
		seg := textRunSeg{
			text:   layout.Text,
			x:      layout.X,
			y:      layout.Y,
			width:  layout.Width,
			height: layout.Height,
			size:   size,
			bold:   layout.Bold,
			italic: layout.Italic,
			angle:  runAngle(layout),
		}
		if len(segments) == 0 {
			segments = append(segments, seg)
			continue
		}
		last := &segments[len(segments)-1]
		if !mergeable(last, &seg) {
			segments = append(segments, seg)
			continue
		}
		last.text += seg.text
		if end := seg.x + seg.width; end > last.x+last.width {
			last.width = end - last.x
		}
		if bottom := seg.y + seg.height; bottom > last.y+last.height {
			last.height = bottom - last.y
		}
	}
	return segments
}

// mergeable 判断两个片段能否合并成一个 span。
func mergeable(a, b *textRunSeg) bool {
	if a.size != b.size || a.bold != b.bold || a.italic != b.italic || a.angle != b.angle {
		return false
	}
	// 纵向：基线接近，容差取半行高，最少 1mm。
	if absFloat(a.y-b.y) > max(1, min(a.height, b.height)*0.5) {
		return false
	}
	// 水平：必须紧邻，中间空隙不超过两倍行高。
	return b.x <= a.x+a.width+max(a.height, b.height)*2
}

// runAngle 取文字的旋转角，优先用字形级角度，其次用字符方向。
func runAngle(layout render.TextLayout) float64 {
	if len(layout.Glyphs) > 0 && layout.Glyphs[0].Angle != 0 {
		return layout.Glyphs[0].Angle
	}
	return float64(layout.CharDirection)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
