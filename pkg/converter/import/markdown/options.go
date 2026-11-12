package markdown

import (
	"errors"
	"fmt"
	"math"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

var errOfficialFonts = errors.New("公文 front matter 需要 flow.GBT9704Options 及五类实际字体资源")
var errMarkdownContent = errors.New("无效的 Markdown 内容")

// 默认沿用 Markdown 的逻辑字体契约；不把系统中偶然找到的字体嵌入产物。
func markdownOptions() flow.Options {
	opts := flow.DefaultOptions()
	opts.Fonts = nil
	opts.AllowLogicalFonts = true
	body := registerLogicalFamily(&opts, "MD-Body", "sans-serif", false)
	mono := registerLogicalFamily(&opts, "MD-Code", "Consolas", true)
	opts.Theme.Body.Font, opts.Theme.Table.Font, opts.Theme.Caption.Font = body.Regular, body.Regular, body.Regular
	for i := range opts.Theme.Headings {
		opts.Theme.Headings[i].Font = body.Bold
	}
	opts.BoldFont, opts.ItalicFont, opts.BoldItalicFont = body.Bold, body.Italic, body.BoldItalic
	opts.CodeFont = mono.Regular
	return opts
}

func registerLogicalFamily(opts *flow.Options, name, family string, mono bool) flow.FontFamily {
	f := flow.FontFamily{Regular: name, Bold: name + "-Bold", Italic: name + "-Italic", BoldItalic: name + "-BoldItalic"}
	for i, alias := range []string{f.Regular, f.Bold, f.Italic, f.BoldItalic} {
		opts.Fonts = append(opts.Fonts, creator.Font{Name: alias, FontName: family, FamilyName: family,
			Charset: "unicode", FixedWidth: mono, Bold: i == 1 || i == 3, Italic: i >= 2})
	}
	opts.FontFamilies = append(opts.FontFamilies, f)
	return f
}

func (front *frontMatter) options(opts flow.Options) (flow.Options, error) {
	// 仅页码配置不是公文：仍可用于普通 Markdown 文档。
	if front.Letterhead == nil && front.Sign == nil && front.Colophon == nil {
		opts.PageNumbers = front.Footer.PageNumber
		if front.Footer.Size != 0 {
			if math.IsNaN(front.Footer.Size) || math.IsInf(front.Footer.Size, 0) || front.Footer.Size < 0 {
				return opts, fmt.Errorf("%w：footer.size 必须为有限正数，实际 %g", errMarkdownContent, front.Footer.Size)
			}
			opts.Theme.Caption.Size = front.Footer.Size
		}
		return opts, nil
	}
	var fonts flow.OfficialFonts
	switch {
	case opts.Official != nil:
		fonts = opts.Official.Fonts
	case opts.AllowLogicalFonts:
		fonts = flow.OfficialFonts{
			Body:  registerLogicalFamily(&opts, "MD-FangSong", "FangSong", false).Regular,
			Title: registerLogicalFamily(&opts, "MD-Title", "STZhongsong", false).Regular,
			Hei:   registerLogicalFamily(&opts, "MD-Hei", "SimHei", false).Regular,
			Kai:   registerLogicalFamily(&opts, "MD-Kai", "KaiTi", false).Regular,
			Song:  registerLogicalFamily(&opts, "MD-Song", "SimSun", false).Regular,
		}
	default:
		return opts, errOfficialFonts
	}
	profile := flow.GBT9704Options(fonts)
	opts.PageSize = profile.PageSize
	opts.MarginTop, opts.MarginRight, opts.MarginBottom, opts.MarginLeft = profile.MarginTop, profile.MarginRight, profile.MarginBottom, profile.MarginLeft
	opts.Theme = profile.Theme
	opts.Official = profile.Official
	// 页码继续由 front matter 显式启用，不因公文默认值而新增页码。
	opts.PageNumbers = front.Footer != nil && front.Footer.PageNumber
	if front.Footer != nil {
		opts.Official.PageNumberSize = front.Footer.Size
	}
	if h := front.Letterhead; h != nil {
		opts.Official.Letterhead = &flow.OfficialLetterhead{Org: h.Org, DocNo: h.DocNo, Signatory: h.Signatory,
			SerialNo: h.SerialNo, Security: h.Security, Urgency: h.Urgency, OrgSize: h.OrgSize, Height: h.Height, DocNoSize: h.DocNoSize}
	}
	if s := front.Sign; s != nil {
		opts.Official.Signature = &flow.OfficialSignature{Org: s.Org, Date: s.Date, Size: s.Size}
	}
	if c := front.Colophon; c != nil {
		opts.Official.Colophon = &flow.OfficialColophon{Cc: c.Cc, IssuedBy: c.IssuedBy, IssuedDate: c.IssuedDate, Size: c.Size}
	}
	return opts, nil
}
