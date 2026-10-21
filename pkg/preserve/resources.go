package preserve

import (
	"fmt"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/spec"
)

// resourceEntries 沿文档、正文页面及模板的资源声明定位 XML，沿用 ST_Loc 基准。
// 同一资源即使多次声明也只改一次，二进制字体和媒体不参与 XML 动作清理。
func resourceEntries(pkg *inputPackage, docEntry string, contents []string, operations map[string][]byte) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	collect := func(source string, parent *etree.Element, tags ...string) error {
		if parent == nil {
			return nil
		}
		for _, element := range parent.ChildElements() {
			if element.NamespaceURI() != spec.Namespace {
				continue
			}
			matches := false
			for _, tag := range tags {
				matches = matches || element.Tag == tag
			}
			location := strings.TrimSpace(element.Text())
			if !matches || location == "" {
				continue
			}
			name, err := resolveEntryLocation(models.StLoc(source).Dir(), location)
			if err != nil {
				return fmt.Errorf("%s 的资源路径 %q 无效: %w", source, location, err)
			}
			entry, exists := pkg.Lookup(name)
			if !exists || seen[entry.Path] {
				continue // 缺失引用由闭包检查和 validator 报告。
			}
			seen[entry.Path] = true
			names = append(names, entry.Path)
		}
		return nil
	}
	doc, err := loadCurrentXML(pkg, docEntry, operations)
	if err != nil {
		return nil, err
	}
	if err := collect(docEntry, findChild(doc.Root(), "CommonData"), "PublicRes", "DocumentRes"); err != nil {
		return nil, err
	}
	visited := map[string]bool{}
	for _, name := range contents {
		if visited[name] {
			continue
		}
		visited[name] = true
		page, err := loadCurrentXML(pkg, name, operations)
		if err != nil {
			return nil, err
		}
		if err := collect(name, page.Root(), "PageRes"); err != nil {
			return nil, err
		}
	}
	return names, nil
}
