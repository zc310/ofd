package sign

import (
	"fmt"
	"path"
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
			return width, height
		}
	}
	return 210, 297
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
