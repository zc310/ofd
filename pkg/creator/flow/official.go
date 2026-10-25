package flow

import (
	"github.com/zc310/ofd/internal/layout"
	"github.com/zc310/ofd/pkg/creator"
)

// OfficialFonts 指定公文使用的已注册字体资源，分别为仿宋、小标宋、黑体、楷体、宋体。
// 本包验证资源存在和字符覆盖，具体字体字形是否符合 GB/T 9704—2012 由调用方确认。
type OfficialFonts struct{ Body, Title, Hei, Kai, Song string }

// Official 配置 GB/T 9704—2012 常规公文的专用要素，不含信函、命令、纪要和联合行文特定格式。
type Official struct {
	Fonts      OfficialFonts
	Letterhead *OfficialLetterhead
	Signature  *OfficialSignature
	Colophon   *OfficialColophon
}

// OfficialLetterhead 是首页版头；Signatory 为单个签发人，OrgSize 为磅，缺省 32。
// 标志上缘距版心上缘 35mm；份号、密级和紧急程度在版心左上角依次排布。
type OfficialLetterhead struct {
	Org, DocNo, Signatory, SerialNo, Security, Urgency string
	OrgSize                                            float64
}

// OfficialSignature 是不加盖印章、单一机关公文的落款。Date 由调用方提供完整成文日期。
type OfficialSignature struct{ Org, Date string }

// OfficialColophon 是末页版记。Cc 不含“抄送：”，IssuedDate 不含“印发”。
type OfficialColophon struct{ Cc, IssuedBy, IssuedDate string }

// GBT9704Options 返回常规公文基础版式：A4、37/26/35/28mm 页边距、三号正文、22 行及 28 全角字网格。
// 五类字体必须在返回选项的 Fonts 中注册；页码为四号宋体、奇右偶左。默认保留内嵌 Go 字体供测试。
func GBT9704Options(fonts OfficialFonts) Options {
	opts := DefaultOptions()
	opts.PageSize = creator.A4
	opts.MarginTop, opts.MarginRight, opts.MarginBottom, opts.MarginLeft = 37, 26, 35, 28
	opts.Theme = themeFromLayout(layout.GBT9704Theme(layout.FlowOfficialFonts(fonts)))
	opts.PageNumbers = true
	opts.Official = &Official{Fonts: fonts}
	return opts
}

func officialToLayout(source *Official) *layout.FlowOfficial {
	if source == nil {
		return nil
	}
	result := &layout.FlowOfficial{Fonts: layout.FlowOfficialFonts(source.Fonts)}
	if source.Letterhead != nil {
		v := layout.FlowOfficialLetterhead(*source.Letterhead)
		result.Letterhead = &v
	}
	if source.Signature != nil {
		v := layout.FlowOfficialSignature(*source.Signature)
		result.Signature = &v
	}
	if source.Colophon != nil {
		v := layout.FlowOfficialColophon(*source.Colophon)
		result.Colophon = &v
	}
	return result
}

// AddRecipient 添加主送机关，顶格排布并尽量与后续正文保持同页；文字应自行包含末尾全角冒号。
// 首页主送过长导致正文无法出现时，自动按四号仿宋迁移到版记，ID 和链接保留；显式分页优先。
func (d *Document) AddRecipient(text string) {
	d.Blocks = append(d.Blocks, Block{Kind: "recipient", Text: text})
}
