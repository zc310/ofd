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
	Pages       []*Page
	Templates   map[models.StID]*models.PageContent
	DrawParams  map[models.StID]*models.DrawParam
	Res         map[models.StID]*models.MultiMedia
	FontRes     map[models.StID]*models.Font
	PublicRes   []*models.Res
	DocumentRes []*models.Res
	Signs       map[models.StID]*models.Signature
	Seals       map[models.StID][]*SealInfo
	Annotations map[models.StID]*models.PageAnnot
}

func (p *Document) parsePublicRes() error {
	if len(p.CommonData.PublicRes) == 0 {
		return nil
	}
	p.PublicRes = make([]*models.Res, len(p.CommonData.PublicRes))
	var err error
	for i, res := range p.CommonData.PublicRes {
		var pr models.Res
		if err = p.FileCache.ParseXMLContent(res.Resolve(p.BaseLoc).String(), &pr); err != nil {
			return err
		}
		p.PublicRes[i] = &pr
		if pr.MultiMedias != nil {
			for _, media := range pr.MultiMedias.MultiMedia {
				p.Res[media.ID] = media
			}
		}

		if pr.DrawParams != nil {
			for _, param := range pr.DrawParams.DrawParam {
				p.DrawParams[param.ID] = param
			}
		}
		if pr.Fonts != nil {
			for _, font := range pr.Fonts.Font {
				if font.FontFile != "" {
					font.FontFile = models.StLoc(p.BaseLoc) + "/" + pr.BaseLoc + "/" + font.FontFile
				}
				p.FontRes[font.ID] = &font
			}
		}
	}
	return nil
}
func (p *Document) parseDocumentRes() error {
	if len(p.CommonData.DocumentRes) == 0 {
		return nil
	}
	p.DocumentRes = make([]*models.Res, len(p.CommonData.DocumentRes))
	var err error
	for i, res := range p.CommonData.DocumentRes {
		var pr models.Res
		if err = p.FileCache.ParseXMLContent(res.Resolve(p.BaseLoc).String(), &pr); err != nil {
			return err
		}
		p.DocumentRes[i] = &pr
		if pr.MultiMedias != nil {
			for _, media := range pr.MultiMedias.MultiMedia {
				if !strings.HasPrefix(media.MediaFile.String(), "/") {
					if pr.BaseLoc == "" {
						media.MediaFile = models.StLoc(p.BaseLoc) + "/" + media.MediaFile
					} else {
						media.MediaFile = models.StLoc(p.BaseLoc) + "/" + pr.BaseLoc + "/" + media.MediaFile
					}
				}

				p.Res[media.ID] = media
			}
		}

		if pr.DrawParams != nil {
			for _, param := range pr.DrawParams.DrawParam {
				p.DrawParams[param.ID] = param
			}
		}
		if pr.Fonts != nil {
			for _, font := range pr.Fonts.Font {
				if font.FontFile != "" {
					font.FontFile = models.StLoc(p.BaseLoc) + "/" + pr.BaseLoc + "/" + font.FontFile
				}
				p.FontRes[font.ID] = &font
			}
		}
	}
	return nil
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
	var dp *models.DrawParam
	var ok bool
	if dp, ok = p.DrawParams[id]; ok {
		if dp.Relative > 0 {
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
				dp.DashPattern = t.DashPattern
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
		return dp
	}
	return nil
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
