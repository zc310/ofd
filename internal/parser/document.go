package parser

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/utils"
)

type Common struct {
	BaseLoc   models.StLoc
	FileCache *core.Package
}

func (p *Common) Init(fileCache *core.Package, dir models.StLoc) {
	p.FileCache = fileCache
	p.BaseLoc = models.StLoc(path.Dir(dir.String()))
}

type Document struct {
	Common
	models.Document
	Pages         []*Page
	pageCacheMu   sync.Mutex
	pageCache     *utils.LRU[*Page, struct{}]
	pageCacheSize int

	Templates           map[models.StID]*models.PageContent
	DrawParams          map[models.StID]*models.DrawParam
	Res                 map[models.StID]*models.MultiMedia
	FontRes             map[models.StID]*models.Font
	CompositeUnits      map[models.StID]*models.CompositeGraphicUnit
	PublicRes           []*models.Res
	DocumentRes         []*models.Res
	Signs               map[string]*models.Signature
	SignedValues        map[string]*SignedValue
	SignedValueErrors   map[string]error
	DigestResults       map[string]*SignatureDigestResult
	VerificationResults map[string]*SignatureVerificationResult
	VerificationErrors  map[string]error
	Seals               map[models.StID][]*SealInfo
	Annotations         map[models.StID]*models.PageAnnot
	Attachments         *models.Attachments
	CustomTags          *models.CustomTags
	Extensions          *models.Extensions
	Versions            map[string]*models.DocVersion
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
	return p.parseResourceFilePath(res.Resolve(p.BaseLoc), resolveMedia)
}

func (p *Document) parseResourceFilePath(path models.StLoc, resolveMedia bool) (*models.Res, error) {
	var pr models.Res
	if err := p.FileCache.ReadXML(path.String(), &pr); err != nil {
		return nil, err
	}

	if pr.MultiMedias != nil {
		for _, media := range pr.MultiMedias.MultiMedia {
			if resolveMedia && !strings.HasPrefix(media.MediaFile.String(), "/") {
				base := path.Dir()
				if pr.BaseLoc != "" && pr.BaseLoc != "." {
					base = base.Join(string(pr.BaseLoc))
				}
				media.MediaFile = base.Join(string(media.MediaFile)).Clean()
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
				font.FontFile = font.FontFile.Resolve(path.Dir().Join(string(pr.BaseLoc)))
			}
			p.FontRes[font.ID] = &font
		}
	}
	return &pr, nil
}

func (p *Document) ensurePageLoaded(page *Page) error {
	p.pageCacheMu.Lock()
	defer p.pageCacheMu.Unlock()
	page.mu.Lock()
	defer page.mu.Unlock()
	if page.loaded {
		if p.pageCache != nil {
			_, _ = p.pageCache.Get(page)
		}
		return page.loadErr
	}
	if page.load != nil {
		page.loadErr = page.load(page)
	}
	page.loaded = true
	if page.loadErr != nil {
		return page.loadErr
	}
	if p.pageCache == nil {
		capacity := p.pageCacheSize
		if capacity <= 0 {
			capacity = defaultPageCacheSize
		}
		p.pageCache = utils.NewLRU[*Page, struct{}](capacity, func(victim *Page, _ struct{}) {
			victim.mu.Lock()
			victim.PageContent = models.PageContent{}
			victim.loaded = false
			victim.loadErr = nil
			victim.mu.Unlock()
		})
	}
	p.pageCache.Add(page, struct{}{})
	return nil
}

func (p *Document) parse(body models.DocBody) error {
	var err error
	if err = p.FileCache.ReadXML(body.DocRoot.Resolve("/").String(), &p.Document); err != nil {
		return err
	}
	p.Pages = make([]*Page, 0, len(p.Document.Pages.Pages))
	for _, page := range p.Document.Pages.Pages {
		pageDef := page
		p.Pages = append(p.Pages, &Page{
			ID:       pageDef.ID,
			document: p,
			load: func(target *Page) error {
				var content models.PageContent
				if err := p.FileCache.ReadXML(pageDef.BaseLoc.Resolve(p.BaseLoc).String(), &content); err != nil {
					return err
				}
				if content.Area == nil {
					content.Area = &p.CommonData.PageArea
				}
				target.PageContent = content
				pagePath := pageDef.BaseLoc.Resolve(p.BaseLoc)
				for _, resource := range content.PageRes {
					resourcePath := pagePath.Dir().Join(resource.String())
					pr, resourceErr := p.parseResourceFilePath(resourcePath, true)
					if resourceErr != nil {
						return resourceErr
					}
					if pr.MultiMedias != nil {
						for _, media := range pr.MultiMedias.MultiMedia {
							p.Res[media.ID] = media
						}
					}
				}
				return nil
			},
		})
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
	if p.Document.Attachments != nil {
		var attachments models.Attachments
		attachmentPath := p.Document.Attachments.Resolve(p.BaseLoc)
		if err := p.FileCache.ReadXML(attachmentPath.String(), &attachments); err != nil {
			return err
		}
		p.Attachments = &attachments
	}
	if p.Document.CustomTags != nil {
		var customTags models.CustomTags
		customTagsPath := p.Document.CustomTags.Resolve(p.BaseLoc)
		if err := p.FileCache.ReadXML(customTagsPath.String(), &customTags); err != nil {
			return err
		}
		p.CustomTags = &customTags
	}
	if p.Document.Extensions != nil {
		var extensions models.Extensions
		extensionsPath := p.Document.Extensions.Resolve(p.BaseLoc)
		if err := p.FileCache.ReadXML(extensionsPath.String(), &extensions); err != nil {
			return err
		}
		p.Extensions = &extensions
	}
	p.Versions = make(map[string]*models.DocVersion)
	if body.Versions != nil {
		for _, version := range body.Versions.VersionList {
			var docVersion models.DocVersion
			versionPath := version.BaseLoc.Resolve("/")
			if err := p.FileCache.ReadXML(versionPath.String(), &docVersion); err != nil {
				return err
			}
			p.Versions[version.ID] = &docVersion
		}
	}

	return nil
}

func (p *Document) parseTemplates() error {
	p.Templates = make(map[models.StID]*models.PageContent)
	var err error
	for _, page := range p.Document.CommonData.TemplatePages {
		var pc models.PageContent
		if err = p.FileCache.ReadXML(page.BaseLoc.Resolve(p.BaseLoc).String(), &pc); err != nil {
			return err
		}
		p.Templates[page.ID] = &pc
	}
	return nil
}

func (p *Document) GetDrawParam(id models.StID) *models.DrawParam {
	return p.resolveDrawParam(id, make(map[models.StID]bool))
}

func (p *Document) resolveDrawParam(id models.StID, resolving map[models.StID]bool) *models.DrawParam {
	dp, ok := p.DrawParams[id]
	if !ok || resolving[id] {
		return nil
	}
	resolving[id] = true
	defer delete(resolving, id)

	result := models.DrawParam{}
	if dp.Relative > 0 {
		if relative := p.resolveDrawParam(models.StID(dp.Relative), resolving); relative != nil {
			result = *relative
		}
	}
	result.ID = dp.ID
	result.Relative = dp.Relative
	result.Override(dp)
	if !result.HasLineWidth() {
		result.LineWidth = 0.353
	}
	if result.Join == "" {
		result.Join = "Miter"
	}
	if result.Cap == "" {
		result.Cap = "Butt"
	}
	if !result.HasMiterLimit() {
		result.MiterLimit = 3.528
	}
	return &result
}
func (p *Document) ParseSigns(file *models.StLoc) error {
	p.Signs = make(map[string]*models.Signature)
	p.SignedValues = make(map[string]*SignedValue)
	p.SignedValueErrors = make(map[string]error)
	p.DigestResults = make(map[string]*SignatureDigestResult)
	p.VerificationResults = make(map[string]*SignatureVerificationResult)
	p.VerificationErrors = make(map[string]error)
	p.Seals = make(map[models.StID][]*SealInfo)
	if file == nil {
		return nil
	}
	var err error
	var signatures Signatures
	dir := file.Dir()
	if err = p.FileCache.ReadXML(file.String(), &signatures); err != nil {
		return err
	}

	for _, body := range signatures.Signatures {
		var sig models.Signature
		if err = p.FileCache.ReadXML(body.BaseLoc.Resolve(dir).String(), &sig); err != nil {
			return err
		}
		seDir := body.BaseLoc.Resolve(dir).Dir()
		p.Signs[body.ID] = &sig
		var signedValue *SignedValue
		if sig.SignedValue != "" {
			signedValuePath := sig.SignedValue.Resolve(seDir).String()
			var signedValueBytes []byte
			if signedValueBytes, err = p.FileCache.Read(signedValuePath); err != nil {
				// 签名值属于签名扩展数据。文件缺失只影响当前签名，不能中止普通文档解析。
				signedValueErr := fmt.Errorf("%s: 读取签名值失败: %w", signedValuePath, err)
				p.SignedValueErrors[body.ID] = signedValueErr
				slog.Warn("读取签名值失败", "signature", body.ID, "file", signedValuePath, "error", err)
			} else if signedValue, err = ParseSignedValue(signedValueBytes); err != nil {
				// SignedValue 是二进制扩展点。非 ASN.1 生产者数据继续保持历史兼容，
				// 同时将解析错误保存下来供调用方查看。
				signedValue = &SignedValue{
					Raw:    append([]byte(nil), signedValueBytes...),
					Format: "unknown",
				}
				p.SignedValues[body.ID] = signedValue
				p.SignedValueErrors[body.ID] = fmt.Errorf("%s: %w", signedValuePath, err)
				slog.Warn("解析签名值失败", "file", signedValuePath, "error", err)
			} else {
				p.SignedValues[body.ID] = signedValue
			}
		}
		if digestResult, digestErr := VerifySignatureDigest(p.FileCache, body.BaseLoc.Resolve(dir).String(), &sig, signedValue); digestErr != nil {
			slog.Warn("校验签名摘要失败", "signature", body.ID, "error", digestErr)
		} else {
			p.DigestResults[body.ID] = digestResult
		}
		if verification, verificationErr := VerifySESSignedValue(signedValue); verificationErr != nil {
			if signedValue != nil && signedValue.SES != nil {
				p.VerificationErrors[body.ID] = verificationErr
				slog.Warn("验证签名失败", "signature", body.ID, "error", verificationErr)
			}
		} else if verification != nil {
			p.VerificationResults[body.ID] = verification
		}
		var sealData *SealData
		var buf []byte
		if sig.SignedInfo.Seal != nil {
			seFile := sig.SignedInfo.Seal.BaseLoc.Resolve(seDir).String()
			if buf, err = p.FileCache.Read(seFile); err != nil {
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
				if buf, err = p.FileCache.Read(sig.SignedValue.Resolve(seDir).String()); err != nil {
					slog.Warn("读取签名值失败", "signature", body.ID, "file", sig.SignedValue.Resolve(seDir).String(), "error", err)
					continue
				}
				if sealData, err = ExtractSealData(buf); err != nil {
					slog.Warn("提取签章失败", "file", sig.SignedValue.Resolve(seDir).String(), "error", err)
					continue
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
	if err = p.FileCache.ReadXML(fileName.String(), &annot); err != nil {
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
		if err = p.FileCache.ReadXML(fileName.String(), &pa); err != nil {
			slog.Warn("读取页面注释失败", "file", fileName.String(), "page_id", page.PageID, "error", err)
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
	ID      string       `xml:"ID,attr"`
	BaseLoc models.StLoc `xml:"BaseLoc,attr"`
}
type SealInfo struct {
	StampAnnot *models.StampAnnot
	SealData   *SealData
}
