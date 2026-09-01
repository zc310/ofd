package parser

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/utils"
)

type Common struct {
	BaseLoc   models.StLoc
	FileCache *utils.ZipFileCache
}

func (p *Common) Init(fileCache *utils.ZipFileCache, dir models.StLoc) {
	p.FileCache = fileCache
	p.BaseLoc = models.StLoc(path.Dir(dir.String()))
}

type Document struct {
	Common
	models.Document
	Pages          []*Page
	Templates      map[models.StID]*models.PageContent
	DrawParams     map[models.StID]*models.DrawParam
	Res            map[models.StID]*models.MultiMedia
	FontRes        map[models.StID]*models.Font
	CompositeUnits map[models.StID]*models.CompositeGraphicUnit
	PublicRes      []*models.Res
	DocumentRes    []*models.Res
	Signs          map[models.StID]*models.Signature
	Seals          map[models.StID][]*SealInfo
	Annotations    map[models.StID]*models.PageAnnot
}

// collectCompositeUnits 收集资源中的复合图元定义。
func (p *Document) collectCompositeUnits(pr *models.Res) {
	if pr.CompositeGraphicUnits == nil {
		return
	}
	for _, unit := range pr.CompositeGraphicUnits.CompositeGraphicUnit {
		u := unit
		p.CompositeUnits[u.ID] = &u
	}
}

func (p *Document) parsePublicRes() error {
	if len(p.CommonData.PublicRes) == 0 {
		return nil
	}
	p.PublicRes = make([]*models.Res, len(p.CommonData.PublicRes))
	for i, res := range p.CommonData.PublicRes {
		pr, err := p.parseResourceFile(res, false)
		if err != nil {
			return err
		}
		p.PublicRes[i] = pr
	}
	return nil
}
func (p *Document) parseDocumentRes() error {
	if len(p.CommonData.DocumentRes) == 0 {
		return nil
	}
	p.DocumentRes = make([]*models.Res, len(p.CommonData.DocumentRes))
	for i, res := range p.CommonData.DocumentRes {
		pr, err := p.parseResourceFile(res, true)
		if err != nil {
			return err
		}
		p.DocumentRes[i] = pr
	}
	return nil
}

// parseResourceFile 解析单个资源文件，并把其中的图片、复合图元、绘制参数与字体登记到
// 对应表中。resolveMedia 控制是否把图片的相对路径转换为绝对路径（Document 资源需要，
// Public 资源不需要）。
func (p *Document) parseResourceFile(res models.StLoc, resolveMedia bool) (*models.Res, error) {
	var pr models.Res
	if err := p.FileCache.ParseXMLContent(res.Resolve(p.BaseLoc).String(), &pr); err != nil {
		return nil, err
	}

	if pr.MultiMedias != nil {
		for _, media := range pr.MultiMedias.MultiMedia {
			if resolveMedia && !strings.HasPrefix(media.MediaFile.String(), "/") {
				if pr.BaseLoc == "" {
					media.MediaFile = models.StLoc(p.BaseLoc) + "/" + media.MediaFile
				} else {
					media.MediaFile = models.StLoc(p.BaseLoc) + "/" + pr.BaseLoc + "/" + media.MediaFile
				}
			}
			p.Res[media.ID] = media
		}
	}

	p.collectCompositeUnits(&pr)

	if pr.DrawParams != nil {
		for _, param := range pr.DrawParams.DrawParam {
			p.DrawParams[param.ID] = param
		}
	}
	if pr.Fonts != nil {
		for _, font := range pr.Fonts.Font {
			if font.FontFile != "" {
				font.FontFile = font.FontFile.Resolve(p.BaseLoc.Join(string(pr.BaseLoc)))
			}
			p.FontRes[font.ID] = &font
		}
	}
	return &pr, nil
}

func (p *Document) parse(body models.DocBody) error {
	var err error
	if err = p.FileCache.ParseXMLContent(body.DocRoot.Resolve("/").String(), &p.Document); err != nil {
		return err
	}
	for _, page := range p.Document.Pages.Pages {
		var pc models.PageContent
		if err = p.FileCache.ParseXMLContent(page.BaseLoc.Resolve(p.BaseLoc).String(), &pc); err != nil {
			return err
		}
		if pc.Area == nil {
			pc.Area = &p.CommonData.PageArea
		}
		p.Pages = append(p.Pages, &Page{ID: page.ID, PageContent: pc})
	}
	if err = p.parseTemplates(); err != nil {
		return err
	}
	p.DrawParams = make(map[models.StID]*models.DrawParam)
	p.Res = make(map[models.StID]*models.MultiMedia)
	p.FontRes = make(map[models.StID]*models.Font)
	p.CompositeUnits = make(map[models.StID]*models.CompositeGraphicUnit)
	if err = p.parsePublicRes(); err != nil {
		slog.Error(err.Error())
	}
	if err = p.parseDocumentRes(); err != nil {
		return err
	}
	if err = p.parseAnnotations(); err != nil {
		return err
	}

	return nil
}

func (p *Document) parseTemplates() error {
	p.Templates = make(map[models.StID]*models.PageContent)
	var err error
	for _, page := range p.Document.CommonData.TemplatePages {
		var pc models.PageContent
		if err = p.FileCache.ParseXMLContent(page.BaseLoc.Resolve(p.BaseLoc).String(), &pc); err != nil {
			return err
		}
		p.Templates[page.ID] = &pc
	}
	return nil
}

func (p *Document) GetDrawParam(id models.StID) *models.DrawParam {
	dp, ok := p.DrawParams[id]
	if !ok {
		return nil
	}
	if dp.Relative <= 0 {
		return dp
	}

	r := p.GetDrawParam(models.StID(dp.Relative))
	if r == nil {
		return dp
	}
	t := *dp
	if dp.Join != "" {
		t.Join = dp.Join
	}
	if dp.LineWidth > 0 {
		t.LineWidth = dp.LineWidth
	}
	if dp.DashOffset > 0 {
		t.DashOffset = dp.DashOffset
	}
	if dp.DashPattern != nil {
		t.DashPattern = dp.DashPattern
	}
	if dp.Cap != "" {
		t.Cap = dp.Cap
	}
	if dp.MiterLimit > 0 {
		t.MiterLimit = dp.MiterLimit
	}
	if dp.FillColor != nil {
		t.FillColor = dp.FillColor
	}
	if dp.StrokeColor != nil {
		t.StrokeColor = dp.StrokeColor
	}
	return &t
}
func (p *Document) ParseSigns(file *models.StLoc) error {
	p.Signs = make(map[models.StID]*models.Signature)
	p.Seals = make(map[models.StID][]*SealInfo)
	if file == nil {
		return nil
	}
	var err error
	var signatures Signatures
	dir := file.Dir()
	if err = p.FileCache.ParseXMLContent(file.String(), &signatures); err != nil {
		return err
	}

	for _, body := range signatures.Signatures {
		var sig models.Signature
		if err = p.FileCache.ParseXMLContent(body.BaseLoc.Resolve(dir).String(), &sig); err != nil {
			return err
		}
		seDir := body.BaseLoc.Resolve(dir).Dir()
		p.Signs[body.ID] = &sig
		var sealData *SealData
		var buf []byte
		if sig.SignedInfo.Seal != nil {
			seFile := sig.SignedInfo.Seal.BaseLoc.Resolve(seDir).String()
			if buf, err = p.FileCache.ParseContent(seFile); err != nil {
				return err
			}

			if sealData, err = ExtractSealData(buf); err != nil {
				slog.Error(fmt.Sprintf("提取签章失败(%s): %v", seFile, err))
				continue
			}
			for _, annot := range sig.SignedInfo.StampAnnot {
				p.Seals[models.StID(annot.PageRef)] = append(p.Seals[models.StID(annot.PageRef)], &SealInfo{StampAnnot: annot, SealData: sealData})
			}
		} else {
			if len(sig.SignedInfo.StampAnnot) > 0 {
				if buf, err = p.FileCache.ParseContent(sig.SignedValue.Resolve(seDir).String()); err != nil {
					return err
				}
				if sealData, err = ExtractSealData(buf); err != nil {
					return err
				}
				for _, annot := range sig.SignedInfo.StampAnnot {
					p.Seals[models.StID(annot.PageRef)] = append(p.Seals[models.StID(annot.PageRef)], &SealInfo{StampAnnot: annot, SealData: sealData})
				}
			}
		}
	}
	return nil
}

func (p *Document) parseAnnotations() error {
	p.Annotations = make(map[models.StID]*models.PageAnnot)
	if p.Document.Annotations == nil {
		return nil
	}
	var err error
	var annot models.Annotations
	fileName := p.Document.Annotations.Resolve(p.BaseLoc)
	if err = p.FileCache.ParseXMLContent(fileName.String(), &annot); err != nil {
		return err
	}
	dir := fileName.Dir()
	for _, page := range annot.Pages {
		var pa models.PageAnnot
		if strings.HasPrefix(page.FileLoc.String(), "/") {
			fileName = page.FileLoc
		} else {
			fileName = models.StLoc.Join(dir, page.FileLoc.String())
		}
		if err = p.FileCache.ParseXMLContent(fileName.String(), &pa); err != nil {
			slog.Error(err.Error())
			continue
		}
		p.Annotations[models.StID(page.PageID)] = &pa
	}
	return nil
}

type Signatures struct {
	XMLName    xml.Name    `xml:"Signatures"`
	Xmlns      string      `xml:"xmlns,attr"`
	MaxSignID  *string     `xml:"MaxSignId,omitempty"`
	Signatures []Signature `xml:"Signature,omitempty"`
}
type Signature struct {
	ID      models.StID  `xml:"ID,attr"`
	BaseLoc models.StLoc `xml:"BaseLoc,attr"`
}
type SealInfo struct {
	StampAnnot *models.StampAnnot
	SealData   *SealData
}
