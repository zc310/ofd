package canvasfont

import (
	"bytes"
	"crypto/sha256"
	"strings"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/font"
	"github.com/zc310/ofd/pkg/spec"
)

// 内嵌 TTC 按资源声明选择字体面后再修复；不同面的 GID 和修复缓存不能混用。
// GB/T 33190—2016 §11.1 表44给出 FontName、FamilyName 及替代样式信息。
func selectCollectionSource(source *embeddedFontSource, resource *spec.Font, style drawing.FontStyle) (*embeddedFontSource, drawing.FontStyle, error) {
	if !bytes.HasPrefix(source.data, []byte("ttcf")) {
		return source, style, nil
	}
	name := resource.FontName
	if index := strings.IndexByte(name, '+'); index >= 0 {
		name = name[index+1:]
	}
	index, selectedStyle := 0, style
	if selected, err := font.MatchFontFace(source.data, name+","+resource.FamilyName,
		font.ParseStyleCSS(style.CSS(), style.Italic())); err == nil {
		index = selected.Index
		selectedStyle = textFontStyle(int(selected.Weight())*100, selected.Italic())
	}
	// 名称表缺失等非规范数据可能无法匹配元数据，但仍可提取默认面后修复。
	// 不遍历猜测其他面；目录或表越界仍由提取器拒绝，未知原生样式沿用资源声明。
	data, err := fontrepair.ExtractCollectionFace(source.data, index)
	if err != nil {
		return nil, style, err
	}
	return &embeddedFontSource{data: data, digest: sha256.Sum256(data)}, selectedStyle, nil
}
