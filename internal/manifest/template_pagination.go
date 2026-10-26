package manifest

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

type templateDetailRow struct {
	height float64
	cells  []Item
}

type templateDetailGroup struct {
	data    []any
	config  DetailPagination
	header  templateDetailRow
	rows    []templateDetailRow
	prefix  []float64
	padding float64
	sums    map[string]string
}

type templateDetailPage struct {
	group         *templateDetailGroup
	start, end    int
	number, pages int
	kind          string
	area          Box
}

func validDetailNumber(value float64, allowZero bool) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && (value > 0 || allowZero && value == 0)
}

func validateDetailArea(area Box, width float64) error {
	if !validDetailNumber(area.X, true) || !validDetailNumber(area.Y, true) || !validDetailNumber(area.Width, false) || !validDetailNumber(area.Height, false) {
		return errors.New("明细 area 的 x/y 必须为非负有限数，width/height 必须为有限正数")
	}
	if math.Abs(area.Width-width) > 1e-6 {
		return errors.New("版式 detail_area.width 必须等于 paginate.area.width")
	}
	return nil
}

func validateDetailPagination(page Page) error {
	p := page.Paginate
	if p == nil {
		if page.Variants != nil {
			return errors.New("variants 必须与 paginate 配合使用")
		}
		return nil
	}
	if strings.HasPrefix(p.Path, "@") {
		return errors.New("path 必须指向明细数组")
	}
	for _, key := range strings.Split(strings.TrimPrefix(p.Path, "$."), ".") {
		if key == "" {
			return errors.New("path 必须是非空点分隔路径")
		}
	}
	if err := validateDetailArea(p.Area, p.Area.Width); err != nil {
		return err
	}
	for _, value := range []float64{p.Size, p.HeaderHeight, p.RowHeight, p.LineWidth} {
		if !validDetailNumber(value, true) {
			return errors.New("size/header_height/row_height/line_width 必须为非负有限数")
		}
	}
	padding := 1.5
	if p.Padding != nil {
		padding = *p.Padding
	}
	if !validDetailNumber(padding, true) {
		return errors.New("padding 必须为非负有限数")
	}
	if len(p.Columns) == 0 {
		return errors.New("columns 不能为空")
	}
	width := 0.0
	size := p.Size
	if size == 0 {
		size = 3.5
	}
	for i, column := range p.Columns {
		if !validDetailNumber(column.Width, false) || column.Width <= 2*padding {
			return fmt.Errorf("columns[%d].width 必须大于两倍 padding", i)
		}
		width += column.Width
		bind := column.Bind
		if bind.Aggregate != "" {
			return fmt.Errorf("columns[%d]: 列内不能使用 aggregate", i)
		}
		if strings.HasPrefix(bind.Path, "@") && bind.Path != "@index" && bind.Path != "@number" {
			return fmt.Errorf("columns[%d]: 列内仅支持 @index/@number 序号", i)
		}
		if bind.Overflow == "" {
			bind.Overflow = "wrap"
		}
		if err := validateBinding(Item{Type: "text", Width: column.Width - 2*padding, Height: 1e6, Size: size, Bind: &bind}); err != nil {
			return fmt.Errorf("columns[%d].bind: %w", i, err)
		}
	}
	if math.Abs(width-p.Area.Width) > 1e-6 {
		return errors.New("columns 列宽之和必须等于 area.width")
	}
	if page.Variants != nil {
		for _, variant := range []*PageVariant{page.Variants.First, page.Variants.Continuation, page.Variants.Last, page.Variants.Single} {
			if variant != nil && variant.DetailArea != nil {
				if err := validateDetailArea(*variant.DetailArea, p.Area.Width); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func detailVariant(page Page, kind string) *PageVariant {
	v := page.Variants
	if v == nil {
		return nil
	}
	switch kind {
	case "first":
		return v.First
	case "continuation":
		return v.Continuation
	case "last":
		if v.Last != nil {
			return v.Last
		}
		return v.Continuation
	case "single":
		if v.Single != nil {
			return v.Single
		}
		if v.Last != nil {
			return v.Last
		}
		return v.First
	}
	return nil
}

func detailArea(page Page, kind string) Box {
	if variant := detailVariant(page, kind); variant != nil && variant.DetailArea != nil {
		return *variant.DetailArea
	}
	return page.Paginate.Area
}

func (r *templateRenderer) paginateRecord(page Page, ctx templateContext, budget int) ([]templateContext, error) {
	if budget <= 0 {
		return nil, errors.New("模板展开超过 max_pages 限制")
	}
	if page.Paginate == nil {
		return []templateContext{ctx}, nil
	}
	value, found := templateLookup(ctx.record, page.Paginate.Path)
	if strings.HasPrefix(page.Paginate.Path, "$.") {
		value, found = templateLookup(ctx.root, page.Paginate.Path)
	}
	rows, ok := value.([]any)
	if !found || !ok {
		return nil, fmt.Errorf("paginate.path %q: 需要明细数组", page.Paginate.Path)
	}
	group, err := r.prepareDetailGroup(*page.Paginate, rows, ctx)
	if err != nil {
		return nil, err
	}
	for _, kind := range []string{"single", "first", "continuation", "last"} {
		if group.header.height > detailArea(page, kind).Height+1e-7 {
			return nil, fmt.Errorf("%s 明细区域放不下表头", kind)
		}
	}
	var parts []templateContext
	start := 0
	for {
		if len(parts) >= budget {
			return nil, errors.New("模板展开超过 max_pages 限制")
		}
		kind := "last"
		if len(parts) == 0 {
			kind = "single"
		}
		area := detailArea(page, kind)
		if group.header.height+group.prefix[len(rows)]-group.prefix[start] <= area.Height+1e-7 {
			part := ctx
			part.detail = &templateDetailPage{group: group, start: start, end: len(rows), number: len(parts) + 1, kind: kind, area: area}
			parts = append(parts, part)
			break
		}
		kind = "continuation"
		if len(parts) == 0 {
			kind = "first"
		}
		area = detailArea(page, kind)
		end := start
		for end < len(rows) && group.header.height+group.prefix[end+1]-group.prefix[start] <= area.Height+1e-7 {
			end++
		}
		if end == start {
			return nil, fmt.Errorf("明细行 %d 无法完整放入 %s 区域，高度 %.3fmm；请增大区域或缩小字体", start, kind, group.rows[start].height)
		}
		// 末页能容纳最后一行时留一行给末页，避免不必要的空白合计页。
		if end == len(rows) && end-start > 1 && group.header.height+group.rows[end-1].height <= detailArea(page, "last").Height+1e-7 {
			end--
		}
		part := ctx
		part.detail = &templateDetailPage{group: group, start: start, end: end, number: len(parts) + 1, kind: kind, area: area}
		parts = append(parts, part)
		start = end
	}
	for i := range parts {
		parts[i].detail.pages = len(parts)
	}
	return parts, nil
}

func (r *templateRenderer) prepareDetailGroup(config DetailPagination, rows []any, ctx templateContext) (*templateDetailGroup, error) {
	if config.Size == 0 {
		config.Size = 3.5
	}
	if config.RowHeight == 0 {
		config.RowHeight = 8
	}
	if config.LineWidth == 0 {
		config.LineWidth = 0.2
	}
	if config.BorderColor == nil {
		config.BorderColor = &Color{R: 110, G: 120, B: 135}
	}
	if config.HeaderFill == nil {
		config.HeaderFill = &Color{R: 240, G: 243, B: 247}
	}
	padding := 1.5
	if config.Padding != nil {
		padding = *config.Padding
	}
	g := &templateDetailGroup{data: rows, config: config, padding: padding, sums: map[string]string{}, prefix: []float64{0}}
	measure := func(header bool, record any, index int) (templateDetailRow, error) {
		row := templateDetailRow{height: math.Max(config.RowHeight, 2*padding)}
		if header {
			row.height = 2 * padding
			if config.HeaderHeight == 0 {
				row.height = math.Max(8, row.height)
			}
		}
		x := padding
		for c, column := range config.Columns {
			bind := column.Bind
			if bind.Overflow == "" {
				bind.Overflow = "wrap"
			}
			item := Item{Type: "text", X: x, Width: column.Width - 2*padding, Height: 1e6, Size: config.Size, Font: config.Font, Bind: &bind}
			var height float64
			drop := false
			var err error
			if header {
				if column.Title == "" {
					x += column.Width
					continue
				}
				item.Bind = &Binding{Path: "header", Overflow: "wrap", Align: bind.Align}
				err = r.layoutText(&item, column.Title, &height)
				item.Bind = nil
			} else {
				rowCtx := ctx
				rowCtx.record, rowCtx.index, rowCtx.detail = record, index, nil
				drop, height, err = r.bindItem(&item, rowCtx)
			}
			if err != nil {
				return row, fmt.Errorf("paginate 明细行 %d columns[%d] %q: %w", index, c, column.Bind.Path, err)
			}
			row.height = math.Max(row.height, height+2*padding)
			if !drop {
				row.cells = append(row.cells, item)
			}
			x += column.Width
		}
		if header && config.HeaderHeight > 0 {
			if row.height > config.HeaderHeight+1e-7 {
				return row, errors.New("header_height 放不下表头文字和 padding")
			}
			row.height = config.HeaderHeight
		}
		for i := range row.cells {
			row.cells[i].Height = row.height - 2*padding
		}
		return row, nil
	}
	var err error
	g.header, err = measure(true, nil, -1)
	if err != nil {
		return nil, err
	}
	for i, data := range rows {
		row, err := measure(false, data, i)
		if err != nil {
			return nil, err
		}
		g.rows = append(g.rows, row)
		g.prefix = append(g.prefix, g.prefix[len(g.prefix)-1]+row.height)
	}
	return g, nil
}

// items 复用试排得到的字形位置，仅平移到本页区域，不再进行第二次断行。
func (d *templateDetailPage) items() []Item {
	g := d.group
	height := g.header.height + g.prefix[d.end] - g.prefix[d.start]
	fill := true
	items := []Item{{Type: "path", X: d.area.X, Y: d.area.Y, Width: d.area.Width, Height: g.header.height, Data: fmt.Sprintf("M 0 0 L %g 0 L %g %g L 0 %g C", d.area.Width, d.area.Width, g.header.height, g.header.height), Fill: &fill, FillColor: g.config.HeaderFill}}
	var grid strings.Builder
	fmt.Fprintf(&grid, "M 0 0 L %g 0 L %g %g L 0 %g C", d.area.Width, d.area.Width, height, height)
	x := 0.0
	for _, col := range g.config.Columns[:len(g.config.Columns)-1] {
		x += col.Width
		fmt.Fprintf(&grid, " M %g 0 L %g %g", x, x, height)
	}
	y := 0.0
	appendRow := func(row templateDetailRow) {
		for _, cell := range row.cells {
			cell.X += d.area.X
			cell.Y = d.area.Y + y + g.padding
			items = append(items, cell)
		}
		y += row.height
		if y < height-1e-7 {
			fmt.Fprintf(&grid, " M 0 %g L %g %g", y, d.area.Width, y)
		}
	}
	appendRow(g.header)
	for _, row := range g.rows[d.start:d.end] {
		appendRow(row)
	}
	items = append(items, Item{Type: "path", X: d.area.X, Y: d.area.Y, Width: d.area.Width, Height: height, Data: grid.String(), Stroke: true, LineWidth: g.config.LineWidth, StrokeColor: g.config.BorderColor})
	return items
}

func appendPageItems(page *Page, items []Item) {
	if len(items) == 0 {
		return
	}
	if len(page.Layers) > 0 {
		page.Layers = append(page.Layers, Layer{Type: "Body", Items: items})
	} else {
		page.Items = append(page.Items, items...)
	}
}

func applyPageVariant(page *Page, variant *PageVariant) {
	if variant == nil {
		return
	}
	page.Templates = append(page.Templates, variant.Templates...)
	if len(variant.Layers) > 0 {
		if len(page.Items) > 0 {
			kind := page.LayerType
			if kind == "" {
				kind = "Body"
			}
			page.Layers = append(page.Layers, Layer{Type: kind, Items: page.Items})
			page.Items = nil
		}
		page.Layers = append(page.Layers, variant.Layers...)
	} else {
		appendPageItems(page, variant.Items)
	}
}
