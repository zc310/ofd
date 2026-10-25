package layout

import (
	"fmt"
	"math"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
)

// 公文的页面和一般行列数来自 GB/T 9704—2012 §5.1、§5.2.1、§5.2.3。
const officialLineHeight = 225.0 / 22
const officialCharWidth = 156.0 / 28

// §3.2 的空行单位：一个三号汉字高度加三号字高的 7/8；与撑满版心的 22 行网格分别计算。
const officialBlankLine = 16 * mmPerPoint * 15 / 8

type FlowOfficialFonts struct{ Body, Title, Hei, Kai, Song string }
type FlowOfficial struct {
	Fonts      FlowOfficialFonts
	Letterhead *FlowOfficialLetterhead
	Signature  *FlowOfficialSignature
	Colophon   *FlowOfficialColophon
}
type FlowOfficialLetterhead struct {
	Org, DocNo, Signatory, SerialNo, Security, Urgency string
	OrgSize                                            float64
}
type FlowOfficialSignature struct{ Org, Date string }
type FlowOfficialColophon struct{ Cc, IssuedBy, IssuedDate string }

// GBT9704Theme 只供 GBT9704Options 组合页面、字体及公文装饰，不独立充当通用 UI 主题。
func GBT9704Theme(fonts FlowOfficialFonts) Theme {
	black, white := creator.Color{}, creator.Color{R: 255, G: 255, B: 255}
	body := TextStyle{Font: fonts.Body, Size: 16, LineHeight: officialLineHeight / ptToMM(16), FirstLineIndent: 2 * officialCharWidth, Align: "justify", Color: &black}
	t := Theme{Body: body, Table: body, Caption: body, BorderColor: &black, HeaderFill: &white}
	for i, name := range []string{fonts.Title, fonts.Hei, fonts.Kai, fonts.Body, fonts.Body, fonts.Body} {
		t.Headings[i] = body
		t.Headings[i].Font = name
	}
	t.Headings[0].Size = 22
	t.Headings[0].LineHeight = (ptToMM(22) + ptToMM(16)*7/8) / ptToMM(22)
	t.Headings[0].FirstLineIndent = 0
	t.Headings[0].Set = StyleFirstLineIndent
	t.Headings[0].Align = "center"
	t.Headings[0].SpaceBefore = 2 * officialBlankLine
	t.Headings[0].SpaceAfter = officialBlankLine
	t.Caption.Size, t.Caption.LineHeight, t.Caption.FirstLineIndent = 14, 1.5, 0
	t.Caption.Set = StyleFirstLineIndent
	t.Caption.Align = "center"
	t.Table.FirstLineIndent = 0
	t.Table.Set = StyleFirstLineIndent
	return t
}

func validateOfficialOptions(opts FlowOptions) error {
	if opts.Official == nil {
		return nil
	}
	f := opts.Official.Fonts
	for _, role := range []struct{ name, value string }{{"body", f.Body}, {"title", f.Title}, {"hei", f.Hei}, {"kai", f.Kai}, {"song", f.Song}} {
		if role.value == "" {
			return fmt.Errorf("official.fonts.%s 必须指定对应的嵌入字体资源", role.name)
		}
		found := false
		for _, resource := range opts.Fonts {
			found = found || resource.Name == role.value
		}
		if !found {
			return fmt.Errorf("official.fonts.%s 引用字体 %q 未注册", role.name, role.value)
		}
	}
	if err := validateOfficialGeometry(opts.PageSize.Width, opts.PageSize.Height, opts.MarginTop, opts.MarginRight, opts.MarginBottom, opts.MarginLeft, opts.Columns); err != nil {
		return err
	}
	if opts.Header != "" || opts.Footer != "" {
		return fmt.Errorf("gbt9704 使用 official.letterhead 和专用页码，不能设置普通 header/footer")
	}
	if head := opts.Official.Letterhead; head != nil {
		if strings.TrimSpace(head.Org) == "" {
			return fmt.Errorf("official.letterhead.org 不能为空")
		}
		if math.IsNaN(head.OrgSize) || math.IsInf(head.OrgSize, 0) || head.OrgSize < 0 {
			return fmt.Errorf("official.letterhead.org_size 必须为有限非负数")
		}
	}
	var texts []struct{ name, text string }
	if h := opts.Official.Letterhead; h != nil {
		texts = append(texts, []struct{ name, text string }{{"letterhead.org", h.Org}, {"letterhead.doc_no", h.DocNo}, {"letterhead.signatory", h.Signatory}, {"letterhead.serial_no", h.SerialNo}, {"letterhead.security", h.Security}, {"letterhead.urgency", h.Urgency}}...)
	}
	if s := opts.Official.Signature; s != nil {
		texts = append(texts, []struct{ name, text string }{{"signature.org", s.Org}, {"signature.date", s.Date}}...)
	}
	if c := opts.Official.Colophon; c != nil {
		texts = append(texts, []struct{ name, text string }{{"colophon.cc", c.Cc}, {"colophon.issued_by", c.IssuedBy}, {"colophon.issued_date", c.IssuedDate}}...)
	}
	for _, v := range texts {
		if err := ValidateFlowText(v.text); err != nil {
			return fmt.Errorf("official.%s: %w", v.name, err)
		}
	}
	return nil
}

func validateOfficialGeometry(width, height, top, right, bottom, left float64, columns FlowColumns) error {
	for _, pair := range [][2]float64{{width, 210}, {height, 297}, {top, 37}, {right, 26}, {bottom, 35}, {left, 28}} {
		if math.IsNaN(pair[0]) || math.IsInf(pair[0], 0) || math.Abs(pair[0]-pair[1]) > 0.000001 {
			return fmt.Errorf("gbt9704 基础版式使用 A4、37/26/35/28mm 页边距及 156×225mm 版心")
		}
	}
	if columns.Count > 1 || columns.Count < 0 || columns.Gap != 0 {
		return fmt.Errorf("gbt9704 基础版式使用单栏")
	}
	return nil
}

func (e *engine) officialStyle(fontName string, size float64) TextStyle {
	return TextStyle{Font: fontName, Size: size, LineHeight: 1, Color: &creator.Color{}, Align: "left"}
}

// 单行要素使用实际字形边界；top 是距页面底部的可见上边缘，返回可见字形高度。
func (e *engine) officialLine(text string, style TextStyle, left, width, top float64) (float64, error) {
	if text == "" {
		return 0, nil
	}
	line, upper, lower, err := e.prepareOfficialLine(text, style, width)
	if err != nil {
		return 0, err
	}
	e.drawAuthorLine(line, style, left, width, top+line.ascent-upper)
	return upper + lower, e.author.err
}

// 度量与绘制分开，同一行的不同字体可共享基线，同时保留各自的文字边界。
func (e *engine) prepareOfficialLine(text string, style TextStyle, width float64) (authorLine, float64, float64, error) {
	if err := ValidateFlowText(text); err != nil {
		return authorLine{}, 0, 0, err
	}
	lines := e.authorLines([]Inline{{Text: text}}, style, width, 0)
	if e.author.err != nil {
		return authorLine{}, 0, 0, e.author.err
	}
	if len(lines) != 1 || e.lineWidth(lines[0].atoms) > width+0.001 {
		return authorLine{}, 0, 0, fmt.Errorf("公文单行要素 %q 无法放入指定宽度", text)
	}
	line := lines[0]
	upper, lower := 0.0, 0.0
	for _, item := range line.atoms {
		face := e.author.faces[item.key.font]
		var buffer sfnt.Buffer
		for _, r := range item.text {
			g, err := face.GlyphIndex(&buffer, r)
			if err != nil {
				return authorLine{}, 0, 0, err
			}
			bounds, _, err := face.GlyphBounds(&buffer, g, 1000*64, font.HintingNone)
			if err != nil {
				return authorLine{}, 0, 0, err
			}
			upper = math.Max(upper, -float64(bounds.Min.Y)/64000*item.size)
			lower = math.Max(lower, float64(bounds.Max.Y)/64000*item.size)
		}
	}
	line.height = line.ascent + line.descent
	return line, upper, lower, nil
}

// 版头位置按 §7.2.1—§7.2.7。发文机关字号由调用方选择，不代替人工验收机关标志。
func (e *engine) emitAuthorOfficialLetterhead() error {
	if e.author.opts.Official == nil || e.author.opts.Official.Letterhead == nil {
		return nil
	}
	e.ensurePage()
	h := e.author.opts.Official.Letterhead
	f := e.author.opts.Official.Fonts
	top := e.contentTop
	row := 0
	for _, mark := range []struct{ text, font string }{{h.SerialNo, f.Body}, {h.Security, f.Hei}, {h.Urgency, f.Hei}} {
		if mark.text == "" {
			continue
		}
		height, err := e.officialLine(mark.text, e.officialStyle(mark.font, 16), e.contentLeft, e.contentWidth, top-float64(row)*officialLineHeight)
		if err != nil {
			return err
		}
		if float64(row)*officialLineHeight+height > 35 {
			return fmt.Errorf("份号、密级或紧急程度侵入发文机关标志区域")
		}
		row++
	}
	size := h.OrgSize
	if size == 0 {
		size = 32
	}
	style := e.officialStyle(f.Title, size)
	style.Color, style.Align = &creator.Color{R: 204}, "center"
	orgTop := top - 35
	height, err := e.officialLine(h.Org, style, e.contentLeft, e.contentWidth, orgTop)
	if err != nil {
		return err
	}
	docTop := orgTop - height - 2*officialBlankLine
	style = e.officialStyle(f.Body, 16)
	if h.Signatory == "" {
		style.Align = "center"
		height, err = e.officialLine(h.DocNo, style, e.contentLeft, e.contentWidth, docTop)
	} else {
		prefix := "签发人："
		prefixLines := e.authorLines([]Inline{{Text: prefix}}, style, e.contentWidth, 0)
		nameStyle := e.officialStyle(f.Kai, 16)
		nameLines := e.authorLines([]Inline{{Text: h.Signatory}}, nameStyle, e.contentWidth, 0)
		if len(prefixLines) != 1 || len(nameLines) != 1 {
			return fmt.Errorf("签发人无法放入一行")
		}
		prefixW, nameW := e.lineWidth(prefixLines[0].atoms), e.lineWidth(nameLines[0].atoms)
		nameX := e.contentRight - officialCharWidth - prefixW - nameW
		docLines := e.authorLines([]Inline{{Text: h.DocNo}}, style, nameX-e.contentLeft-2*officialCharWidth, 0)
		if len(docLines) != 1 {
			return fmt.Errorf("发文字号与签发人无法并排")
		}
		parts := []struct {
			text        string
			style       TextStyle
			left, width float64
		}{
			{h.DocNo, style, e.contentLeft + officialCharWidth, nameX - e.contentLeft - 2*officialCharWidth},
			{prefix, style, nameX, prefixW},
			{h.Signatory, nameStyle, nameX + prefixW, nameW},
		}
		lines := make([]authorLine, len(parts))
		upper, lower := 0.0, 0.0
		for i, part := range parts {
			var up, down float64
			lines[i], up, down, err = e.prepareOfficialLine(part.text, part.style, part.width)
			if err != nil {
				return err
			}
			upper, lower = math.Max(upper, up), math.Max(lower, down)
		}
		// §7.2.5—§7.2.6：发文字号与签发人处于同一行，基线按整行最高字形统一。
		for i, part := range parts {
			e.drawAuthorLine(lines[i], part.style, part.left, part.width, docTop+lines[i].ascent-upper)
		}
		height = upper + lower
	}
	if err != nil {
		return err
	}
	if height == 0 {
		height = ptToMM(16)
	}
	lineTop := docTop - height - 4
	e.fillRect(e.contentLeft, lineTop-0.5, e.contentWidth, 0.5, &creator.Color{R: 204})
	e.y = lineTop - 0.5
	if e.y < e.contentBottom+4*officialLineHeight {
		return fmt.Errorf("版头过高，首页无法容纳标题和正文")
	}
	return nil
}

// 不加盖印章的单一机关落款，按 §7.3.5.2 排两行并在正文之后空一行。
func (e *engine) emitAuthorOfficialSignature() error {
	if e.author.opts.Official == nil || e.author.opts.Official.Signature == nil {
		return nil
	}
	s := e.author.opts.Official.Signature
	if s.Org == "" && s.Date == "" {
		return nil
	}
	style := e.officialStyle(e.author.opts.Official.Fonts.Body, 16)
	measure := func(text string) (float64, error) {
		if text == "" {
			return 0, nil
		}
		lines := e.authorLines([]Inline{{Text: text}}, style, e.contentWidth, 0)
		if len(lines) != 1 {
			return 0, fmt.Errorf("落款无法放入一行")
		}
		return e.lineWidth(lines[0].atoms), e.author.err
	}
	orgW, err := measure(s.Org)
	if err != nil {
		return err
	}
	dateW, err := measure(s.Date)
	if err != nil {
		return err
	}
	orgX := e.contentRight - 2*officialCharWidth - orgW
	dateX := orgX + 2*officialCharWidth
	if dateW > orgW {
		dateX = e.contentRight - 2*officialCharWidth - dateW
		orgX = dateX - 2*officialCharWidth
	}
	if orgX < e.contentLeft || dateX < e.contentLeft {
		return fmt.Errorf("落款文字过宽")
	}
	if err := e.ensureAuthorHeight(3*officialBlankLine, nil); err != nil {
		return err
	}
	e.y -= officialBlankLine
	if _, err = e.officialLine(s.Org, style, orgX, orgW, e.y); err != nil {
		return err
	}
	e.y -= officialBlankLine
	if _, err = e.officialLine(s.Date, style, dateX, dateW, e.y); err != nil {
		return err
	}
	e.y -= officialBlankLine
	return nil
}

// 版记按 §7.4 锚定末页面版心底边；空间不足或该页有脚注时另起一页，避免重叠。
func (e *engine) emitAuthorOfficialColophon() error {
	if e.author.opts.Official == nil {
		return nil
	}
	c := e.author.opts.Official.Colophon
	if c == nil {
		c = &FlowOfficialColophon{}
	}
	if len(e.author.opts.relocatedRecipients) == 0 && strings.TrimSpace(c.Cc) == "" && strings.TrimSpace(c.IssuedBy) == "" && strings.TrimSpace(c.IssuedDate) == "" {
		return nil
	}
	style := e.officialStyle(e.author.opts.Official.Fonts.Body, 14)
	style.LineHeight = 1.4
	pad := ptToMM(14)
	inner := e.contentWidth - 2*pad
	recipients, recipientHeight, err := e.prepareColophonRecipients(style, inner)
	if err != nil {
		return err
	}
	var cc []authorLine
	var prefixLine authorLine
	prefixW := 0.0
	if c.Cc != "" {
		prefix := e.authorLines([]Inline{{Text: "抄送："}}, style, inner, 0)
		if len(prefix) != 1 {
			return fmt.Errorf("版记抄送标签无法放入一行")
		}
		prefixW = e.lineWidth(prefix[0].atoms)
		prefixLine = prefix[0]
		cc = e.authorLines([]Inline{{Text: c.Cc}}, style, inner-prefixW, 0)
	}
	date := c.IssuedDate
	if date != "" {
		date += "印发"
	}
	issued := c.IssuedBy != "" || date != ""
	var byLines, dateLines []authorLine
	if c.IssuedBy != "" {
		byLines = e.authorLines([]Inline{{Text: c.IssuedBy}}, style, inner, 0)
	}
	if date != "" {
		dateLines = e.authorLines([]Inline{{Text: date}}, style, inner, 0)
	}
	if len(byLines) > 1 || len(dateLines) > 1 {
		return fmt.Errorf("版记印发机关或日期过长")
	}
	byW, dateW := 0.0, 0.0
	if len(byLines) > 0 {
		byW = e.lineWidth(byLines[0].atoms)
	}
	if len(dateLines) > 0 {
		dateW = e.lineWidth(dateLines[0].atoms)
	}
	if byW+dateW > inner-pad {
		return fmt.Errorf("版记印发机关与日期重叠")
	}
	height := 0.7 + recipientHeight
	for _, line := range cc {
		if e.lineWidth(line.atoms) > inner-prefixW+0.001 {
			return fmt.Errorf("版记抄送文字过宽")
		}
		height += line.height
	}
	band := ptToMM(14) * 1.4
	if len(byLines) > 0 {
		band = math.Max(band, byLines[0].height)
	}
	if len(dateLines) > 0 {
		band = math.Max(band, dateLines[0].height)
	}
	if issued {
		height += band
	}
	if (len(cc) > 0 || len(recipients) > 0) && issued {
		height += 0.25
	}
	if e.author.err != nil {
		return e.author.err
	}
	if height > e.contentHeight {
		return fmt.Errorf("版记超过整页版心")
	}
	if len(e.currentAuthorFootnotes().notes) > 0 || e.y < e.opts.MarginBottom+height {
		e.newPage()
	}
	e.y = e.opts.MarginBottom + height
	black := &creator.Color{}
	e.fillRect(e.contentLeft, e.y-0.35, e.contentWidth, 0.35, black)
	e.y -= 0.35
	for _, entry := range recipients {
		e.recordAuthorAnchor(entry.block.ID)
		for i, line := range entry.lines {
			if i == 0 && len(entry.label.atoms) > 0 {
				label := entry.label
				label.height = line.height
				e.drawAuthorLine(label, style, e.contentLeft+pad, entry.labelWidth, e.y)
			}
			e.drawAuthorLine(line, style, e.contentLeft+pad+entry.labelWidth, inner-entry.labelWidth, e.y)
			e.y -= line.height
		}
	}
	for i, line := range cc {
		if i == 0 {
			prefixLine.height = line.height
			e.drawAuthorLine(prefixLine, style, e.contentLeft+pad, prefixW, e.y)
		}
		e.drawAuthorLine(line, style, e.contentLeft+pad+prefixW, inner-prefixW, e.y)
		e.y -= line.height
	}
	if (len(cc) > 0 || len(recipients) > 0) && issued {
		e.fillRect(e.contentLeft, e.y-0.25, e.contentWidth, 0.25, black)
		e.y -= 0.25
	}
	if issued {
		if len(byLines) > 0 {
			e.drawAuthorLine(byLines[0], style, e.contentLeft+pad, byW, e.y)
		}
		if len(dateLines) > 0 {
			e.drawAuthorLine(dateLines[0], style, e.contentRight-pad-dateW, dateW, e.y)
		}
		e.y -= band
	}
	e.fillRect(e.contentLeft, e.y-0.35, e.contentWidth, 0.35, black)
	e.y -= 0.35
	return e.author.err
}

// 页码一字线的实际字形上缘距版心底边 7mm（§7.5），用标准 TextCode 定位。
func (e *engine) emitAuthorOfficialPageNumber(page int, label string) error {
	f := e.author.opts.Official.Fonts.Song
	style := e.officialStyle(f, 14)
	lines := e.authorLines([]Inline{{Text: "— " + label + " —"}}, style, e.contentWidth, 0)
	if e.author.err != nil {
		return e.author.err
	}
	if len(lines) != 1 {
		return fmt.Errorf("公文页码无法放入一行")
	}
	face := e.author.faces[f]
	var buffer sfnt.Buffer
	g, err := face.GlyphIndex(&buffer, '—')
	if err != nil {
		return err
	}
	bounds, _, err := face.GlyphBounds(&buffer, g, 1000*64, font.HintingNone)
	if err != nil {
		return err
	}
	line := lines[0]
	line.height = line.ascent + line.descent
	baseline := e.opts.PageHeight - e.opts.MarginBottom + 7 - float64(bounds.Min.Y)/64000*ptToMM(14)
	top := e.opts.PageHeight - (baseline - line.ascent)
	width := e.lineWidth(line.atoms)
	left := e.contentLeft + ptToMM(14)
	if page%2 == 0 {
		left = e.contentRight - ptToMM(14) - width
	}
	e.drawAuthorLine(line, style, left, width, top)
	return e.author.err
}
