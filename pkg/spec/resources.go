package spec

import (
	"encoding/xml"
	"strings"
)

// ColorSpaces 是 GB/T 33190—2016 §7.9 表18的颜色空间资源集合。
type ColorSpaces struct {
	// ColorSpace 按资源文件中的声明顺序保存颜色空间。
	ColorSpace []ColorSpace `xml:"ColorSpace"`
}

// ColorSpace 是 §8.3.1 表25的颜色空间声明，按§7.9扩展资源 ID。
// 不加载 Profile、不转换颜色，也不将省略的 BitsPerComponent 补为默认值8。
type ColorSpace struct {
	// ID 是文档作用域内的资源标识。
	ID StID `xml:"ID,attr"` //nolint:tagliatelle // OFD 标准属性名为 ID。
	// Type 可为 GRAY、RGB 或 CMYK。
	Type string `xml:"Type,attr"`
	// BitsPerComponent 为每个颜色分量的位数，缺省时保持零值。
	BitsPerComponent int `xml:"BitsPerComponent,attr,omitempty"`
	// Profile 是包内颜色配置文件的词法路径。
	Profile StLoc `xml:"Profile,attr,omitempty"`
	// Palette 是可选调色板。
	Palette *Palette `xml:"Palette,omitempty"`
}

// Palette 是 §8.3.1 表25的调色板，CV 的行下标即从0开始的颜色索引。
// 分量保留词法值，不在模型层按位深转换或校验。
type Palette struct {
	CV [][]string `xml:"CV"` //nolint:tagliatelle // OFD 标准元素名为 CV。
}

type paletteXML struct {
	CV []string `xml:"CV"` //nolint:tagliatelle // OFD 标准元素名为 CV。
}

// UnmarshalXML 将每个 CV 元素的空白分隔颜色分量解析为独立的一行。
func (p *Palette) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value paletteXML
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	p.CV = make([][]string, len(value.CV))
	for i, entry := range value.CV {
		p.CV[i] = strings.Fields(entry)
	}
	return nil
}

// Fonts 是 §7.9 表18的字体资源集合。
type Fonts struct {
	Font []Font `xml:"Font"`
}

// Font 是 §11.1 表44的字体声明，按§7.9扩展资源 ID，不包含字体加载状态。
// 没有 FontFile 时是逻辑字体；实际字体匹配与回退由使用方负责。
type Font struct {
	// ID 是文档作用域内的资源标识。
	ID StID `xml:"ID,attr"` //nolint:tagliatelle // OFD 标准属性名为 ID。
	// FontName 是字型名称。
	FontName string `xml:"FontName,attr"`
	// FamilyName 是用于匹配替代字体的字型族名。
	FamilyName string `xml:"FamilyName,attr,omitempty"`
	// Charset 省略时保持空字符串，不提前补入标准缺省值 unicode。
	Charset string `xml:"Charset,attr,omitempty"`
	// Italic 指示斜体；字型标志按表44缺省为 false，不区分显式 false 与省略。
	Italic bool `xml:"Italic,attr,omitempty"`
	// Bold 指示粗体。
	Bold bool `xml:"Bold,attr,omitempty"`
	// Serif 指示衬线字体。
	Serif bool `xml:"Serif,attr,omitempty"`
	// FixedWidth 指示等宽字体。
	FixedWidth bool `xml:"FixedWidth,attr,omitempty"`
	// FontFile 是包内内嵌字体文件的词法路径。
	FontFile StLoc `xml:"FontFile,omitempty"`
}

// MultiMedias 是 §7.9 表18的多媒体资源集合。
type MultiMedias struct {
	MultiMedia []*MultiMedia `xml:"MultiMedia"`
}

// MultiMedia 是 §7.9 表19的多媒体声明，不加载或探测媒体内容。
type MultiMedia struct {
	// ID 是文档作用域内的资源标识。
	ID StID `xml:"ID,attr"` //nolint:tagliatelle // OFD 标准属性名为 ID。
	// Type 可为 Image、Audio 或 Video。
	Type string `xml:"Type,attr"`
	// Format 保留原声明，不根据扩展名或文件内容推断。
	Format string `xml:"Format,attr,omitempty"`
	// MediaFile 是包内媒体文件的词法路径。
	MediaFile StLoc `xml:"MediaFile"`
}
