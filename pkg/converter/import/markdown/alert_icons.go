package markdown

import (
	"embed"
	"encoding/xml"
	"fmt"
	"strings"
	"sync"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// 五种原创 SVG 使用与 GitHub Alerts 相同的语义：信息、灯泡、提示气泡、警告三角形、停止标志。
// 只内嵌固定矢量轮廓，不访问网络、不依赖图标字体，也不将 SVG 栅格化。
//
//go:embed icons/*.svg
var alertIconFiles embed.FS

//nolint:gochecknoglobals // 固定五个内置图标的只读、有界解析快照，供并发转换复用。
var loadAlertIcons = sync.OnceValues(func() (map[string]flow.Icon, error) {
	icons := make(map[string]flow.Icon, 5)
	for _, entry := range []struct{ title, file string }{
		{"Note", "info"}, {"Tip", "light-bulb"}, {"Important", "report"}, {"Warning", "alert"}, {"Caution", "stop"},
	} {
		data, err := alertIconFiles.ReadFile("icons/" + entry.file + ".svg")
		if err != nil {
			return nil, fmt.Errorf("读取提示图标 %s 失败: %w", entry.title, err)
		}
		icon, err := parseAlertIcon(data)
		if err != nil {
			return nil, fmt.Errorf("解析提示图标 %s 失败: %w", entry.title, err)
		}
		icons[entry.title] = icon
	}
	return icons, nil
})

// 这里只转换仓库内受控的 16×16 SVG，不是通用 SVG 导入器。
// 源路径仅使用绝对 M/L/Q/C/A/Z；OFD 的三次曲线是 B，闭合才是 C（§9.1 表 36）。
func parseAlertIcon(data []byte) (flow.Icon, error) {
	//nolint:tagliatelle // SVG 元素和属性采用协议规定的大小写与连字符。
	var source struct {
		XMLName xml.Name `xml:"svg"`
		ViewBox string   `xml:"viewBox,attr"`
		Paths   []struct {
			Data string `xml:"d,attr"`
			Rule string `xml:"fill-rule,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(data, &source); err != nil {
		return flow.Icon{}, err
	}
	if source.ViewBox != "0 0 16 16" || len(source.Paths) != 1 || source.Paths[0].Rule != "evenodd" {
		return flow.Icon{}, fmt.Errorf("%w：内置提示图标需为 16×16 单路径 evenodd SVG", errMarkdownContent)
	}
	path := source.Paths[0].Data
	if path == "" {
		return flow.Icon{}, fmt.Errorf("%w：内置提示图标路径为空", errMarkdownContent)
	}
	for _, r := range path {
		if (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') && !strings.ContainsRune("MLQCAZ", r) {
			return flow.Icon{}, fmt.Errorf("%w：内置提示图标不支持 SVG 命令 %q", errMarkdownContent, r)
		}
	}
	path = strings.NewReplacer("C", "B", "Z", "C").Replace(path)
	if _, err := models.ParsePathData(path); err != nil {
		return flow.Icon{}, err
	}
	return flow.Icon{Width: 16, Height: 16, Data: path, Rule: "Even-Odd"}, nil
}
