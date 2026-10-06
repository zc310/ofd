package sign

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// normalizeStampID 把签名 ID 规范化为合法的 xs:ID（不能以数字开头）。
func normalizeStampID(id string) string {
	if id == "" {
		return "Sign"
	}
	if id[0] >= '0' && id[0] <= '9' {
		return "Sign" + id
	}
	return id
}

// resolveStamp 计算 StampAnnot 的 PageRef 与 Boundary。
// PageRef 未指定时使用文档体首页；Boundary 未指定时按首页大小把印章放在右上角。
func resolveStamp(options StampOptions, docDir string, entries []entry) (*stampAnnot, error) {
	pageRef := strings.TrimSpace(options.PageRef)
	width, height, pageID, err := firstPageGeometry(docDir, entries)
	if err != nil {
		return nil, err
	}
	if pageRef == "" {
		if pageID == "" {
			return nil, fmt.Errorf("%s 找不到可签章的页面", docDir)
		}
		pageRef = pageID
	}
	boundary := strings.TrimSpace(options.Boundary)
	if boundary == "" {
		boundary = defaultStampBoundary(width, height)
	}
	return &stampAnnot{pageRef: pageRef, boundary: boundary}, nil
}

func resolveStampSeams(options StampSeamOptions, docDir string, entries []entry) ([]*stampAnnot, error) {
	edges, err := seamEdges(options.Edge)
	if err != nil {
		return nil, err
	}
	pages, err := documentPages(docDir, entries)
	if err != nil {
		return nil, err
	}
	pages, err = selectSeamPages(pages, options.Pages)
	if err != nil {
		return nil, err
	}
	if len(pages) < 2 {
		return nil, fmt.Errorf("%s 少于两页，无法生成骑缝章", docDir)
	}
	if options.Pieces < 0 {
		return nil, fmt.Errorf("骑缝章拆分份数不能为负数，实际 %d", options.Pieces)
	}
	if options.GroupPages < 0 {
		return nil, fmt.Errorf("骑缝章分组页数不能为负数，实际 %d", options.GroupPages)
	}
	if options.GroupPages > 0 && options.Pieces > 0 {
		return nil, fmt.Errorf("骑缝章分组页数与拆分份数不能同时指定")
	}
	for _, value := range []struct {
		name string
		v    float64
	}{
		{"边长", options.Size},
		{"最小条带宽度", options.MinStrip},
		{"X 坐标", options.X},
		{"Y 坐标", options.Y},
	} {
		if math.IsNaN(value.v) || math.IsInf(value.v, 0) {
			return nil, fmt.Errorf("骑缝章%s必须是有限数值，实际 %v", value.name, value.v)
		}
	}
	if options.Size < 0 {
		return nil, fmt.Errorf("骑缝章边长不能为负数，实际 %.2f", options.Size)
	}
	size := options.Size
	if size == 0 {
		size = defaultStampSize
	}
	hasHorizontalEdge := containsSeamEdge(edges, "top") || containsSeamEdge(edges, "bottom")
	hasVerticalEdge := containsSeamEdge(edges, "left") || containsSeamEdge(edges, "right")
	minStrip := options.MinStrip
	if minStrip == 0 {
		minStrip = defaultMinSeamStrip
	}
	if minStrip < 0 {
		return nil, fmt.Errorf("骑缝章最小条带宽度不能为负数，实际 %.2f", minStrip)
	}
	groups := [][]pageGeometry{pages}
	if options.GroupPages > 0 {
		groups = make([][]pageGeometry, 0, (len(pages)+options.GroupPages-1)/options.GroupPages)
		for start := 0; start < len(pages); start += options.GroupPages {
			end := start + options.GroupPages
			if end > len(pages) {
				end = len(pages)
			}
			groups = append(groups, pages[start:end])
		}
	}
	stamps := make([]*stampAnnot, 0, len(pages)*len(edges))
	for _, group := range groups {
		pieces := options.Pieces
		if pieces == 0 {
			pieces = len(group)
		}
		if pieces < 2 && len(groups) == 1 {
			return nil, fmt.Errorf("骑缝章拆分份数必须至少为 2，实际 %d", pieces)
		}
		if pieces != len(group) {
			return nil, fmt.Errorf("骑缝章拆分份数 %d 与参与页面数 %d 不一致", pieces, len(group))
		}
		stripWidth := size / float64(pieces)
		if stripWidth < minStrip {
			return nil, fmt.Errorf("骑缝章每页条带宽度 %.3fmm 小于最小值 %.3fmm；请减少每组页面、增大印章尺寸或降低 --sign-stamp-seam-min-strip", stripWidth, minStrip)
		}
		for _, edge := range edges {
			for index, page := range group {
				if size > page.width || size > page.height {
					return nil, fmt.Errorf("页面 %s 尺寸 %.2f x %.2f 小于骑缝章边长 %.2f", page.id, page.width, page.height, size)
				}
				x := options.X
				if x < 0 {
					x = (page.width - size) / 2
				}
				if hasHorizontalEdge && (x < 0 || x+size > page.width) {
					return nil, fmt.Errorf("页面 %s 的骑缝章 X 坐标 %.2f 超出页面宽度 %.2f", page.id, x, page.width)
				}
				if !hasHorizontalEdge {
					x = 0
				}
				y := options.Y
				if y < 0 {
					y = (page.height - size) / 2
				}
				if hasVerticalEdge && (y < 0 || y+size > page.height) {
					return nil, fmt.Errorf("页面 %s 的骑缝章 Y 坐标 %.2f 超出页面高度 %.2f", page.id, y, page.height)
				}
				clipX, clipY := 0.0, 0.0
				boundaryX, boundaryY := x, y
				clipWidth, clipHeight := stripWidth, size
				switch edge {
				case "left", "right":
					clipX = stripWidth * float64(index)
					boundaryX = -clipX
					if edge == "right" {
						boundaryX = page.width - stripWidth*float64(index+1)
					}
				case "top", "bottom":
					clipY = stripWidth * float64(index)
					boundaryY = -clipY
					if edge == "bottom" {
						boundaryY = page.height - stripWidth*float64(index+1)
					}
					clipWidth, clipHeight = size, stripWidth
				}
				stamps = append(stamps, &stampAnnot{
					pageRef:  page.id,
					boundary: formatBoundary(boundaryX, boundaryY, size, size),
					clip:     formatBoundary(clipX, clipY, clipWidth, clipHeight),
				})
			}
		}
	}
	return stamps, nil
}

func seamEdges(value string) ([]string, error) {
	edge := strings.ToLower(strings.TrimSpace(value))
	if edge == "" {
		edge = "right"
	}
	if edge == "all" {
		return []string{"left", "right", "top", "bottom"}, nil
	}
	if edge != "left" && edge != "right" && edge != "top" && edge != "bottom" {
		return nil, fmt.Errorf("骑缝章边缘只支持 left、right、top、bottom 或 all，实际 %q", value)
	}
	return []string{edge}, nil
}

func containsSeamEdge(edges []string, target string) bool {
	for _, edge := range edges {
		if edge == target {
			return true
		}
	}
	return false
}

func selectSeamPages(pages []pageGeometry, mode string) ([]pageGeometry, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "all"
	}
	if mode == "all" {
		return pages, nil
	}
	if mode != "odd" && mode != "even" {
		selected := make([]bool, len(pages))
		for _, token := range strings.Split(mode, ",") {
			token = strings.TrimSpace(token)
			if token == "" || token == "-" || strings.Count(token, "-") > 1 {
				return nil, fmt.Errorf("骑缝章页码范围无效: %q", token)
			}
			start, end := 0, 0
			var err error
			switch {
			case strings.HasPrefix(token, "-"):
				start = 1
				end, err = parseSeamPageNumber(token[1:], len(pages))
			case strings.HasSuffix(token, "-"):
				start, err = parseSeamPageNumber(token[:len(token)-1], len(pages))
				end = len(pages)
			case strings.Contains(token, "-"):
				fields := strings.SplitN(token, "-", 2)
				start, err = parseSeamPageNumber(fields[0], len(pages))
				if err == nil {
					end, err = parseSeamPageNumber(fields[1], len(pages))
				}
			default:
				start, err = parseSeamPageNumber(token, len(pages))
				end = start
			}
			if err != nil {
				return nil, err
			}
			if end < start {
				return nil, fmt.Errorf("骑缝章页码范围起点大于终点: %q", token)
			}
			for pageNumber := start; pageNumber <= end; pageNumber++ {
				if selected[pageNumber-1] {
					return nil, fmt.Errorf("骑缝章页码重复: %d", pageNumber)
				}
				selected[pageNumber-1] = true
			}
		}
		result := make([]pageGeometry, 0)
		for index, page := range pages {
			if selected[index] {
				result = append(result, page)
			}
		}
		return result, nil
	}
	selected := make([]pageGeometry, 0, (len(pages)+1)/2)
	for index, page := range pages {
		pageNumber := index + 1
		if (mode == "odd" && pageNumber%2 == 1) || (mode == "even" && pageNumber%2 == 0) {
			selected = append(selected, page)
		}
	}
	return selected, nil
}

func parseSeamPageNumber(value string, pageCount int) (int, error) {
	pageNumber, err := strconv.Atoi(value)
	if err != nil || pageNumber < 1 || pageNumber > pageCount {
		return 0, fmt.Errorf("骑缝页码 %q 超出范围 1-%d", value, pageCount)
	}
	return pageNumber, nil
}

type pageGeometry struct {
	id            string
	width, height float64
}

func documentPages(docDir string, entries []entry) ([]pageGeometry, error) {
	documentFile := path.Join(docDir, "Document.xml")
	for _, item := range entries {
		if item.name != documentFile {
			continue
		}
		document := etree.NewDocument()
		if err := document.ReadFromBytes(item.data); err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", documentFile, err)
		}
		root := document.Root()
		if root == nil {
			return nil, fmt.Errorf("%s 根元素无效", documentFile)
		}
		width, height := pageArea(findDescendant(root, "PageArea"))
		var pages []pageGeometry
		for _, page := range findDescendants(root, "Page") {
			id := strings.TrimSpace(page.SelectAttrValue("ID", ""))
			if id == "" {
				return nil, fmt.Errorf("%s 存在没有 ID 的页面", documentFile)
			}
			pageWidth, pageHeight := width, height
			if area := findDescendant(page, "Area"); area != nil {
				if localWidth, localHeight, ok := parsePageArea(area); ok {
					pageWidth, pageHeight = localWidth, localHeight
				}
			}
			baseLoc := strings.TrimSpace(page.SelectAttrValue("BaseLoc", ""))
			if baseLoc != "" {
				pageFile := resolvePageEntryPath(docDir, baseLoc)
				for _, content := range entries {
					if content.name != pageFile {
						continue
					}
					pageDocument := etree.NewDocument()
					if err := pageDocument.ReadFromBytes(content.data); err != nil {
						return nil, fmt.Errorf("解析 %s 失败: %w", pageFile, err)
					}
					if area := findDescendant(pageDocument.Root(), "Area"); area != nil {
						if localWidth, localHeight, ok := parsePageArea(area); ok {
							pageWidth, pageHeight = localWidth, localHeight
						}
					}
					break
				}
			}
			pages = append(pages, pageGeometry{id: id, width: pageWidth, height: pageHeight})
		}
		return pages, nil
	}
	return nil, fmt.Errorf("%s 缺少 Document.xml", docDir)
}

func resolvePageEntryPath(docDir, baseLoc string) string {
	if strings.HasPrefix(baseLoc, "/") {
		return strings.TrimPrefix(path.Clean(baseLoc), "/")
	}
	return path.Join(docDir, baseLoc)
}

func findDescendants(element *etree.Element, local string) []*etree.Element {
	if element == nil {
		return nil
	}
	var result []*etree.Element
	for _, child := range element.ChildElements() {
		if localName(child) == local {
			result = append(result, child)
		}
		result = append(result, findDescendants(child, local)...)
	}
	return result
}

func formatBoundary(x, y, width, height float64) string {
	if x == 0 {
		x = 0
	}
	if y == 0 {
		y = 0
	}
	return fmt.Sprintf("%g %g %g %g", x, y, width, height)
}

// firstPageGeometry 返回文档体首页的宽高（毫米）和页面 ID。
func firstPageGeometry(docDir string, entries []entry) (float64, float64, string, error) {
	documentFile := path.Join(docDir, "Document.xml")
	for _, item := range entries {
		if item.name != documentFile {
			continue
		}
		document := etree.NewDocument()
		if err := document.ReadFromBytes(item.data); err != nil {
			return 0, 0, "", fmt.Errorf("解析 %s 失败: %w", documentFile, err)
		}
		root := document.Root()
		if root == nil {
			return 0, 0, "", fmt.Errorf("%s 根元素无效", documentFile)
		}
		pageID := ""
		if page := findDescendant(root, "Page"); page != nil {
			pageID = page.SelectAttrValue("ID", "")
		}
		width, height := pageArea(findDescendant(root, "PageArea"))
		return width, height, pageID, nil
	}
	return 0, 0, "", fmt.Errorf("%s 缺少 Document.xml", docDir)
}

// pageArea 解析 PhysicalBox/PageArea，返回宽高；解析失败时返回默认 A4 尺寸。
func pageArea(area *etree.Element) (float64, float64) {
	if width, height, ok := parsePageArea(area); ok {
		return width, height
	}
	return 210, 297
}

func parsePageArea(area *etree.Element) (float64, float64, bool) {
	for _, name := range []string{"PhysicalBox", "PageArea"} {
		box := firstElement(area, name)
		if box == nil {
			continue
		}
		fields := strings.Fields(box.Text())
		if len(fields) < 4 {
			continue
		}
		var coords [4]float64
		ok := true
		for index, field := range fields[:4] {
			var value float64
			if _, err := fmt.Sscanf(field, "%g", &value); err != nil {
				ok = false
				break
			}
			coords[index] = value
		}
		if !ok {
			continue
		}
		width := coords[2]
		height := coords[3]
		if len(fields) >= 6 {
			width -= coords[0]
			height -= coords[1]
		}
		if width > 0 && height > 0 {
			return width, height, true
		}
	}
	return 0, 0, false
}

// defaultStampBoundary 把默认大小的印章放在首页右下角。
func defaultStampBoundary(width, height float64) string {
	margin := 10.0
	size := defaultStampSize
	x := width - size - margin
	if x < margin {
		x = margin
	}
	y := height - size - margin
	if y < margin {
		y = margin
	}
	return fmt.Sprintf("%g %g %g %g", x, y, size, size)
}

func firstElement(element *etree.Element, local string) *etree.Element {
	if element == nil {
		return nil
	}
	for _, child := range element.ChildElements() {
		if localName(child) == local {
			return child
		}
	}
	return nil
}

// findDescendant 按文档顺序深度优先查找第一个指定本地名的后代元素。
func findDescendant(element *etree.Element, local string) *etree.Element {
	if element == nil {
		return nil
	}
	for _, child := range element.ChildElements() {
		if localName(child) == local {
			return child
		}
		if found := findDescendant(child, local); found != nil {
			return found
		}
	}
	return nil
}
