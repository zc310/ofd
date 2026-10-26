package report

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/canvas"
	fontfile "github.com/zc310/font"
	"github.com/zc310/ofd/internal/fontrepair"
	"golang.org/x/image/font/sfnt"
)

// Font 是排版度量与嵌入共用的字体快照。
type Font struct {
	Data []byte
	Face *sfnt.Font
	path string
}

// LoadFont 加载显式字体或自动查找中文系统字体。
func LoadFont(explicit string) (Font, error) {
	if explicit != "" {
		f, err := loadFontFile(explicit)
		if err != nil {
			return Font{}, fmt.Errorf("加载报告字体 %q: %w", explicit, err)
		}
		return f, nil
	}
	seen := map[string]bool{}
	for _, name := range []string{"Noto Sans CJK SC", "Noto Sans SC", "Source Han Sans SC", "Microsoft YaHei", "微软雅黑", "等线", "SimSun", "宋体", "WenQuanYi Micro Hei", "仿宋", "楷体", "黑体"} {
		path, ok := canvas.FindSystemFont(name, canvas.FontRegular)
		if !ok || seen[path] {
			continue
		}
		seen[path] = true
		if f, err := loadFontFile(path); err == nil {
			return f, nil
		}
	}
	return Font{}, fmt.Errorf("未找到可用于 OFD/PDF 报告的中文字体，请通过 --font 指定 TTF、OTF 或 TTC 文件")
}

func loadFontFile(path string) (Font, error) {
	return loadFontFamilyFile(path, "")
}

// loadBoldFont 只接受同族的实际粗体，避免系统匹配器返回常规字体或其他字体族。
func loadBoldFont(regular Font) (Font, bool) {
	family := fontFamily(regular.Face)
	if family == "" {
		return Font{}, false
	}
	var paths []string
	if path, ok := canvas.FindSystemFont(family, canvas.FontBold); ok {
		paths = append(paths, path)
	}
	if regular.path != "" {
		// 显式字体可能位于系统索引之外；同目录的候选仍须核对字体族和实际字重。
		name := filepath.Base(regular.path)
		for _, replacement := range [][2]string{{"Regular", "Bold"}, {"regular", "bold"}, {"REGULAR", "BOLD"}} {
			if strings.Contains(name, replacement[0]) {
				paths = append(paths, filepath.Join(filepath.Dir(regular.path), strings.ReplaceAll(name, replacement[0], replacement[1])))
			}
		}
		// 同一个 TTC 也可能同时包含常规和粗体字体面。
		paths = append(paths, regular.path)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		if bold, err := loadFontFamilyFace(path, family, true); err == nil {
			return bold, true
		}
	}
	return Font{}, false
}

func fontFamily(face *sfnt.Font) string {
	var buffer sfnt.Buffer
	for _, id := range []sfnt.NameID{sfnt.NameIDTypographicFamily, sfnt.NameIDFamily} {
		if name, err := face.Name(&buffer, id); err == nil && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func loadFontFamilyFile(path, family string) (Font, error) {
	return loadFontFamilyFace(path, family, false)
}

func isBoldFont(data []byte) bool {
	parsed, err := fontfile.ParseSFNT(data, 0)
	return err == nil && parsed.OS2 != nil && parsed.OS2.UsWeightClass >= 600
}

func loadFontFamilyFace(path, family string, bold bool) (Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Font{}, err
	}
	if bytes.HasPrefix(data, []byte("ttcf")) {
		collection, err := sfnt.ParseCollection(data)
		if err != nil {
			return Font{}, err
		}
		selected := false
		for i := 0; i < collection.NumFonts(); i++ {
			face, err := collection.Font(i)
			if err != nil {
				return Font{}, err
			}
			if supportsChinese(face) && (family == "" || strings.EqualFold(fontFamily(face), family)) {
				faceData, err := fontrepair.ExtractCollectionFace(data, i)
				if err != nil {
					return Font{}, err
				}
				if bold && !isBoldFont(faceData) {
					continue
				}
				data = faceData
				selected = true
				break
			}
		}
		if !selected {
			return Font{}, fmt.Errorf("字体集合中没有匹配的中文字体面")
		}
	}
	face, parseErr := sfnt.Parse(data)
	// 度量和 flow 写出使用同一可解析快照；修复产物不可用时仍尝试原始数据。
	if repaired, err := fontrepair.Repair(data); err == nil {
		if parsed, err := sfnt.Parse(repaired); err == nil {
			data, face, parseErr = repaired, parsed, nil
		}
	}
	if parseErr != nil {
		return Font{}, parseErr
	}
	if !supportsChinese(face) {
		return Font{}, fmt.Errorf("字体不包含可用的中文字符")
	}
	if family != "" && !strings.EqualFold(fontFamily(face), family) {
		return Font{}, fmt.Errorf("字体族与 %q 不匹配", family)
	}
	if bold && !isBoldFont(data) {
		return Font{}, fmt.Errorf("字体不是实际粗体字体面")
	}
	return Font{Data: data, Face: face, path: path}, nil
}

func supportsChinese(face *sfnt.Font) bool {
	var buffer sfnt.Buffer
	for _, r := range "中文校验报告" {
		glyph, err := face.GlyphIndex(&buffer, r)
		if err != nil || glyph == 0 {
			return false
		}
	}
	return true
}
