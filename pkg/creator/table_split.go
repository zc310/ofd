package creator

import (
	"fmt"
	"sort"
)

// splittingPages 在 PageProvider 外层把放不下的表格拆到续页上。
//
// 为什么不放在 buildState 里追加页面：pageCount 在 prepare 阶段就按
// pages.PageCount() 定死，pageIDs、跳转、书签、注解、印章的页码下标都以它为界，
// 写出阶段再长出页面会让这些引用整体错位。表格又不是线级图元——它在生成阶段
// 展开成 Text 与 Path，所以只要在页面这一层把一张超页表格换成几张各自不超页的
// 表格，后续流程完全不用知道发生过拆分。
type splittingPages struct {
	inner PageProvider
	size  PageSize
	// prefix[i] 是内部页 i 之前累计的输出页数，长度为内部页数加一；
	// 用它把输出页下标二分映射回 (内部页, 该页内的第几片)。
	prefix []int
	total  int
}

func newSplittingPages(inner PageProvider, size PageSize) (*splittingPages, error) {
	count := inner.PageCount()
	s := &splittingPages{
		inner:  inner,
		size:   size,
		prefix: make([]int, count+1),
	}
	for index := 0; index < count; index++ {
		page, err := inner.PageAt(index)
		if err != nil {
			return nil, fmt.Errorf("页面 %d 读取失败: %w", index+1, err)
		}
		parts, err := splitPageByTable(page, size)
		if err != nil {
			return nil, fmt.Errorf("页面 %d 无效: %w", index+1, err)
		}
		s.prefix[index+1] = s.prefix[index] + len(parts)
	}
	s.total = s.prefix[count]
	return s, nil
}

func (s *splittingPages) PageCount() int {
	return s.total
}

func (s *splittingPages) PageAt(index int) (Page, error) {
	if index < 0 || index >= s.total {
		return Page{}, fmt.Errorf("页码 %d 超出范围（共 %d 页）", index+1, s.total)
	}
	// prefix 单调不减（二分取最大的 prefix[i] <= index）。
	inner := sort.Search(len(s.prefix), func(i int) bool {
		return s.prefix[i] > index
	}) - 1
	page, err := s.inner.PageAt(inner)
	if err != nil {
		return Page{}, fmt.Errorf("页面 %d 读取失败: %w", inner+1, err)
	}
	parts, err := splitPageByTable(page, s.size)
	if err != nil {
		return Page{}, fmt.Errorf("页面 %d 无效: %w", inner+1, err)
	}
	return parts[index-s.prefix[inner]], nil
}

// splitPageByTable 把一页里所有放不下的表格各自拆成多片，输出页数取各表片数的
// 最大值。页内没有需要拆分的表格时原样返回该页——绝大多数页面走的是这条路径。
//
// 表格高度按文档页面尺寸计算，与 expandTable 保持一致：页面区域（Area）不参与
// 表格排版，两处若各按各的算，拆出来的片段与展开结果会对不上。
func splitPageByTable(page Page, size PageSize) ([]Page, error) {
	layers := page.Layers
	if len(layers) == 0 {
		layers = []Layer{{Type: page.LayerType, Items: page.Items}}
	}

	splits := make([]layerSplit, len(layers))
	parts := 1
	for i, layer := range layers {
		split := layerSplit{layer: layer}
		for _, item := range layer.Items {
			table, ok := item.(Table)
			if !ok {
				split.plain = append(split.plain, item)
				continue
			}
			layout, err := layoutTable(table, size.Width, size.Height)
			if err != nil {
				return nil, fmt.Errorf("图层 %d 的表格无效: %w", i+1, err)
			}
			fragments := layout.tableFragments()
			if len(fragments) > parts {
				parts = len(fragments)
			}
			split.tables = append(split.tables, fragments)
		}
		splits[i] = split
	}
	if parts == 1 {
		return []Page{page}, nil
	}

	pages := make([]Page, parts)
	for part := 0; part < parts; part++ {
		pages[part] = continuationPage(page, splits, part, parts)
	}
	return pages, nil
}

// layerSplit 是一层里表格与非表格图元的拆分结果：tables 按出现顺序存每张表的
// 片段，plain 是其余图元（只落在首片所在的那一页）。
type layerSplit struct {
	layer  Layer
	plain  []Item
	tables [][]Table
}

// items 拼出该层在第 part 片上的图元。part 之后该层没有内容时返回 nil。
func (s layerSplit) items(part int) []Item {
	items := make([]Item, 0, len(s.plain)+len(s.tables))
	if part == 0 {
		items = append(items, s.plain...)
	}
	for _, fragments := range s.tables {
		if part < len(fragments) {
			items = append(items, fragments[part])
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// continuationPage 生成拆出的第 part 页。页面区域、模板引用与页面资源跟着走，
// 动作不跟：跳转之类的作用在逻辑页上，续页是排版产物，不是新的逻辑页。
func continuationPage(page Page, splits []layerSplit, part, parts int) Page {
	out := Page{
		Area:      page.Area,
		Resources: page.Resources,
		Templates: page.Templates,
	}
	if part == 0 {
		// 首片沿用原来的 items/layers 形态，避免把单图层页面改写成 Layers。
		if len(page.Layers) == 0 {
			out.LayerType = page.LayerType
			out.Items = splits[0].items(0)
			return out
		}
		out.Layers = make([]Layer, 0, len(splits))
		for _, split := range splits {
			items := split.items(0)
			if items == nil {
				items = []Item{}
			}
			out.Layers = append(out.Layers, Layer{
				Type:      split.layer.Type,
				DrawParam: split.layer.DrawParam,
				Items:     items,
			})
		}
		return out
	}

	out.Layers = make([]Layer, 0, len(splits))
	for _, split := range splits {
		items := split.items(part)
		if items == nil {
			continue
		}
		out.Layers = append(out.Layers, Layer{
			Type:      split.layer.Type,
			DrawParam: split.layer.DrawParam,
			Items:     items,
		})
	}
	return out
}

// ensureTableFits 检查表格能否落在当前上下文里。页面顶层的表格已在
// splittingPages 里拆过，剩下放不下的只可能出现在模板、复合图元、图案、注解与
// PageBlock 内——这些上下文不能凭空多出页面，与其把行画到页底之外让人以为表丢了，
// 不如直接报错。
func ensureTableFits(layout *tableLayout, rows int) error {
	if len(layout.segments) <= 1 {
		return nil
	}
	return fmt.Errorf("表格 %d 行放不下一页（需拆成 %d 页），"+
		"而该表格位于不能跨页的上下文中；请减少行数或把它放到页面顶层", rows, len(layout.segments))
}
