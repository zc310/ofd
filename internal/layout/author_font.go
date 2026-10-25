package layout

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/sfnt"
)

// 只共享只读的字体解析结果，避免试排保留上一轮的页面、锚点和脚注状态。
type authorFontSnapshot struct {
	faces         map[string]*sfnt.Font
	fonts         []creator.Font
	fontResources map[string]creator.Font
	fontFamilies  map[string]FlowFontFamily
}

func (a *authorState) loadAuthorFonts() error {
	if a.opts.balance != nil && a.opts.balance.fonts != nil {
		f := a.opts.balance.fonts
		a.faces, a.fonts, a.fontResources, a.fontFamilies = f.faces, f.fonts, f.fontResources, f.fontFamilies
		return nil
	}
	for i, resource := range a.opts.Fonts {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		if resource.Name == "" || a.faces[resource.Name] != nil {
			return fmt.Errorf("fonts[%d]: 字体名称为空或重复", i)
		}
		if resource.Source != nil {
			r, err := resource.Source.Open()
			if err != nil {
				return fmt.Errorf("fonts[%d]: %w", i, err)
			}
			data, readErr := io.ReadAll(contextio.Reader(a.ctx, r))
			closeErr := r.Close()
			if err := a.ctx.Err(); err != nil {
				return err
			}
			if readErr != nil {
				return fmt.Errorf("fonts[%d]: %w", i, readErr)
			}
			if closeErr != nil {
				return fmt.Errorf("fonts[%d]: %w", i, closeErr)
			}
			resource.Data, resource.Source = data, nil
		}
		index := a.opts.FontFaces[resource.Name]
		if index < 0 {
			return fmt.Errorf("font_faces[%q]: 字体面索引不能为负数", resource.Name)
		}
		if bytes.HasPrefix(resource.Data, []byte("ttcf")) {
			collection, err := sfnt.ParseCollection(resource.Data)
			if err != nil {
				return fmt.Errorf("fonts[%d] %q: 字体集合无法解析: %w", i, resource.Name, err)
			}
			if index >= collection.NumFonts() {
				return fmt.Errorf("font_faces[%q]: 索引 %d 超出集合范围（共 %d 个字体面）", resource.Name, index, collection.NumFonts())
			}
			selected, err := fontrepair.ExtractCollectionFace(resource.Data, index)
			if err != nil {
				return fmt.Errorf("fonts[%d] %q: 提取字体面 %d 失败: %w", i, resource.Name, index, err)
			}
			// 仅提取完整字体面，不重排 CFF 子程序或字形编号。
			resource.Data = selected
			if bytes.HasPrefix(selected, []byte("OTTO")) {
				resource.Format = "otf"
			} else {
				resource.Format = "ttf"
			}
		} else if index != 0 {
			return fmt.Errorf("font_faces[%q]: 单字体文件只支持索引 0", resource.Name)
		}
		face, _ := sfnt.Parse(resource.Data)
		if face != nil && !a.opts.fontNamesResolved {
			// GB/T 33190—2016 §11.1 表 44：资源别名与实际字型名分开。
			// TTC 已提取所选面；在修复前读取，避免把修复器补造的名称当成原字体名称。
			if strings.TrimSpace(resource.FontName) == "" {
				resource.FontName = authorFontName(face, sfnt.NameIDFull, sfnt.NameIDPostScript)
			}
			if strings.TrimSpace(resource.FamilyName) == "" {
				resource.FamilyName = authorFontName(face, sfnt.NameIDTypographicFamily, sfnt.NameIDFamily)
			}
		}
		if repaired, err := fontrepair.Repair(resource.Data); err == nil {
			if parsed, err := sfnt.Parse(repaired); err == nil {
				face, resource.Data = parsed, repaired
			}
		}
		if face == nil {
			face, _ = sfnt.Parse(resource.Data)
		}
		if face == nil {
			return fmt.Errorf("fonts[%d] %q: 需要可解析的嵌入字体", i, resource.Name)
		}
		if bytes.HasPrefix(resource.Data, []byte("OTTO")) {
			resource.Format = "otf"
		} else {
			resource.Format = "ttf"
		}
		a.faces[resource.Name] = face
		a.fontResources[resource.Name] = resource
		a.fonts = append(a.fonts, resource)
	}
	keys := make([]string, 0, len(a.opts.FontFaces))
	for name := range a.opts.FontFaces {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if a.faces[name] == nil {
			return fmt.Errorf("font_faces[%q]: 字体未注册", name)
		}
	}
	for i, family := range a.opts.FontFamilies {
		if err := a.registerAuthorFamily(family); err != nil {
			return fmt.Errorf("font_families[%d]: %w", i, err)
		}
	}
	// 原有全局样式选项只构成正文字体族，不影响其他已注册字体族。
	if _, exists := a.fontFamilies[a.opts.Theme.Body.Font]; !exists {
		family := FlowFontFamily{Regular: a.opts.Theme.Body.Font, Bold: a.opts.BoldFont, Italic: a.opts.ItalicFont, BoldItalic: a.opts.BoldItalicFont}
		// 默认 Go 样式仅用于 Go 正文，修改正文为中文字体后不自动套入拉丁字体。
		if family.Regular != "Flow-Regular" {
			if family.Bold == "Flow-Bold" {
				family.Bold = ""
			}
			if family.Italic == "Flow-Italic" {
				family.Italic = ""
			}
			if family.BoldItalic == "Flow-BoldItalic" {
				family.BoldItalic = ""
			}
		}
		if family.Bold != "" || family.Italic != "" || family.BoldItalic != "" {
			if err := a.registerAuthorFamily(family); err != nil {
				return fmt.Errorf("正文字体族: %w", err)
			}
		}
	}
	for i := range a.fonts {
		a.fonts[i] = a.fontResources[a.fonts[i].Name]
	}
	return nil
}

// 名称按固定优先级读取；缺失或非法名称交由创建器使用资源别名。
func authorFontName(face *sfnt.Font, ids ...sfnt.NameID) string {
	var buffer sfnt.Buffer
	for _, id := range ids {
		name, err := face.Name(&buffer, id)
		name = strings.TrimSpace(name)
		if err == nil && name != "" && ValidateFlowText(name) == nil {
			return name
		}
	}
	return ""
}

// registerAuthorFamily 只标注实际选择的字体资源，不合成粗体或斜体字形。
// Bold/Italic/FixedWidth 是标准字体属性（GB/T 33190—2016 §11.1 表 44）。
func (a *authorState) registerAuthorFamily(family FlowFontFamily) error {
	if family.Regular == "" {
		return fmt.Errorf("regular 字体不能为空")
	}
	roles := []struct {
		name         string
		bold, italic bool
	}{{family.Regular, false, false}, {family.Bold, true, false}, {family.Italic, false, true}, {family.BoldItalic, true, true}}
	seen := map[string]bool{}
	regular := a.fontResources[family.Regular]
	for _, role := range roles {
		if role.name == "" {
			continue
		}
		if a.faces[role.name] == nil {
			return fmt.Errorf("字体 %q 未注册", role.name)
		}
		if seen[role.name] {
			return fmt.Errorf("字体 %q 重复用于多个样式", role.name)
		}
		if _, exists := a.fontFamilies[role.name]; exists {
			return fmt.Errorf("字体 %q 重复属于多个字体族", role.name)
		}
		seen[role.name] = true
	}
	for _, role := range roles {
		if role.name == "" {
			continue
		}
		a.fontFamilies[role.name] = family
		resource := a.fontResources[role.name]
		resource.Bold, resource.Italic = role.bold, role.italic
		// 等宽与衬线类别跟随同族常规字体，回退选择使用同样的类别判断。
		resource.FixedWidth, resource.Serif = regular.FixedWidth, regular.Serif
		if resource.FamilyName == "" {
			resource.FamilyName = regular.FamilyName
		}
		a.fontResources[role.name] = resource
	}
	return nil
}

func (a *authorState) styledAuthorFont(base string, bold, italic bool) string {
	family, ok := a.fontFamilies[base]
	if !ok {
		return base
	}
	switch {
	case bold && italic && family.BoldItalic != "":
		return family.BoldItalic
	case bold && family.Bold != "":
		return family.Bold
	case italic && family.Italic != "":
		return family.Italic
	}
	return base
}
