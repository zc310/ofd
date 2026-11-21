package flow

import (
	"fmt"
	"math"
	"strings"
)

type sectionState struct {
	geometry                 pageGeometry
	columns                  Columns
	header, footer           string
	pageNumbers              bool
	numberStyle, prefix      string
	firstHeader, firstFooter *string
	hideFirstNumber          bool
}

type pageSettings struct {
	section                   sectionState
	sectionIndex, sectionPage int
	label                     string
}

func (s sectionState) merge(block Block) (sectionState, error) {
	c := block.Section
	if c == nil {
		return s, fmt.Errorf("缺少 section 配置")
	}
	if block.ID != "" || block.Text != "" || len(block.Runs) > 0 || block.Image != nil || block.Table != nil || block.Fixed != nil || block.TOC != nil || block.Style != nil || block.Marker != "" || block.Level != 0 {
		return s, fmt.Errorf("分节块不能携带内容、ID 或文字样式")
	}
	if c.PageSize != nil {
		s.geometry.PageWidth = c.PageSize.Width
		s.geometry.PageHeight = c.PageSize.Height
	}
	if c.Margins != nil {
		s.geometry.MarginTop = c.Margins.Top
		s.geometry.MarginRight = c.Margins.Right
		s.geometry.MarginBottom = c.Margins.Bottom
		s.geometry.MarginLeft = c.Margins.Left
	}
	if c.Columns != nil {
		s.columns = *c.Columns
	}
	if c.Header != nil {
		s.header = *c.Header
	}
	if c.Footer != nil {
		s.footer = *c.Footer
	}
	if c.PageNumbers != nil {
		s.pageNumbers = *c.PageNumbers
	}
	if c.PageNumberStyle != "" {
		s.numberStyle = c.PageNumberStyle
	}
	if c.PageNumberPrefix != nil {
		s.prefix = *c.PageNumberPrefix
	}
	s.firstHeader, s.firstFooter, s.hideFirstNumber = c.FirstPageHeader, c.FirstPageFooter, c.HideFirstPageNumber
	if c.StartPageNumber != nil && *c.StartPageNumber < 1 {
		return s, fmt.Errorf("start_page_number 必须为正数")
	}
	switch s.numberStyle {
	case "decimal", "roman", "ROMAN":
	default:
		return s, fmt.Errorf("不支持的 page_number_style %q", s.numberStyle)
	}
	for _, value := range []float64{s.geometry.PageWidth, s.geometry.PageHeight, s.geometry.MarginTop, s.geometry.MarginRight, s.geometry.MarginBottom, s.geometry.MarginLeft} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return s, fmt.Errorf("页面尺寸与页边距必须为有限非负数")
		}
	}
	if s.geometry.PageWidth <= s.geometry.MarginLeft+s.geometry.MarginRight || s.geometry.PageHeight <= s.geometry.MarginTop+s.geometry.MarginBottom {
		return s, fmt.Errorf("页边距使版心宽度或高度不大于零")
	}
	var columnErr error
	s.columns, columnErr = normalizeColumns(s.columns, s.geometry.PageWidth-s.geometry.MarginLeft-s.geometry.MarginRight)
	if columnErr != nil {
		return s, columnErr
	}
	texts := []string{s.header, s.footer, s.prefix}
	if s.firstHeader != nil {
		texts = append(texts, *s.firstHeader)
	}
	if s.firstFooter != nil {
		texts = append(texts, *s.firstFooter)
	}
	for _, text := range texts {
		if err := ValidateText(text); err != nil {
			return s, err
		}
	}
	if (s.header != "" || s.firstHeader != nil && *s.firstHeader != "") && s.geometry.MarginTop < 8 {
		return s, fmt.Errorf("显示页眉时上页边距至少为 8mm")
	}
	if (s.pageNumbers || s.footer != "" || s.firstFooter != nil && *s.firstFooter != "") && s.geometry.MarginBottom < 8 {
		return s, fmt.Errorf("显示页脚或页码时下页边距至少为 8mm")
	}
	return s, nil
}

// 页面区域遵循 GB/T 33190—2016 §7.7 表 12：页对象 Area 仅作用于该页。
// 分节在创作时展开为独立页面，写出依旧使用标准 PageArea/PhysicalBox。
func (e *engine) setGeometry(settings pageGeometry) { e.geometry = settings; e.computeArea() }

func pageLabel(number int, style, prefix string) (string, error) {
	if number < 1 {
		return "", fmt.Errorf("页码超出有效范围")
	}
	if style == "decimal" {
		return prefix + fmt.Sprint(number), nil
	}
	if number > 3999 {
		return "", fmt.Errorf("罗马页码仅支持 1 到 3999")
	}
	var text strings.Builder
	values := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	letters := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	for i, value := range values {
		for number >= value {
			text.WriteString(letters[i])
			number -= value
		}
	}
	label := text.String()
	if style == "roman" {
		label = strings.ToLower(label)
	}
	return prefix + label, nil
}

func (e *engine) recordPage() {
	a := e.state
	label, err := pageLabel(a.nextNumber, a.section.numberStyle, a.section.prefix)
	if err != nil && a.err == nil {
		a.err = err
	}
	a.pageSettings = append(a.pageSettings, pageSettings{section: a.section, sectionIndex: a.sectionIndex, sectionPage: a.sectionPage, label: label})
	a.pageFootnotes = append(a.pageFootnotes, make([]pageFootnotes, a.section.columns.Count))
	if a.opts.balance != nil {
		a.columnHeights = append(a.columnHeights, make([]float64, a.section.columns.Count))
	}
	if a.nextNumber == math.MaxInt {
		a.err = fmt.Errorf("页码超过整数范围")
	} else {
		a.nextNumber++
	}
	a.sectionPage++
}

func (a *layoutState) referenceLabel(id string) string {
	if a.opts.ReferencePageNumbers == "section" {
		if label, ok := a.opts.previousLabels[id]; ok {
			return label
		}
	}
	return fmt.Sprint(a.opts.previousPages[id] + 1)
}

func (e *engine) emitPageDecorations() error {
	a := e.state
	if a.err != nil {
		return a.err
	}
	counts := map[int]int{}
	for _, page := range a.pageSettings {
		counts[page.sectionIndex]++
	}
	for i, page := range a.pageSettings {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		e.pageIndex = i
		e.setGeometry(page.section.geometry)
		s := page.section
		if a.opts.Official != nil {
			if s.pageNumbers && !(page.sectionPage == 0 && s.hideFirstNumber) {
				if err := e.emitOfficialPageNumber(i, page.label); err != nil {
					return err
				}
			}
			continue
		}
		header, footer := s.header, s.footer
		if page.sectionPage == 0 {
			if s.firstHeader != nil {
				header = *s.firstHeader
			}
			if s.firstFooter != nil {
				footer = *s.firstFooter
			}
		}
		if s.pageNumbers && !(page.sectionPage == 0 && s.hideFirstNumber) {
			footer = strings.TrimSpace(footer + " {page} / {pages}")
		}
		if header == "" && footer == "" {
			continue
		}
		style := a.opts.Theme.Caption
		style.Font = a.opts.Theme.Body.Font
		if err := validateTextStyle(style); err != nil {
			return fmt.Errorf("页码样式: %w", err)
		}
		replacer := strings.NewReplacer("{page}", page.label, "{pages}", fmt.Sprint(len(e.pages)), "{physical_page}", fmt.Sprint(i+1), "{section_page}", fmt.Sprint(page.sectionPage+1), "{section_pages}", fmt.Sprint(counts[page.sectionIndex]))
		for _, area := range []struct {
			text        string
			margin, top float64
			align       string
		}{
			{header, e.geometry.MarginTop, e.geometry.PageHeight - 4, "left"},
			{footer, e.geometry.MarginBottom, e.geometry.MarginBottom - 4, "center"},
		} {
			if area.text == "" {
				continue
			}
			lines := e.breakLines([]Run{{Text: replacer.Replace(area.text)}}, style, e.contentWidth, 0)
			if a.err != nil {
				return a.err
			}
			if len(lines) != 1 || e.lineWidth(lines[0].atoms) > e.contentWidth || lines[0].height > area.margin-4 {
				return fmt.Errorf("第 %d 页页眉或页脚无法放入页边距", i+1)
			}
			style.Align = area.align
			e.drawLine(lines[0], style, e.contentLeft, e.contentWidth, area.top)
		}
	}
	return a.err
}
