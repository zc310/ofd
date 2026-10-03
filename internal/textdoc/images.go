package textdoc

import (
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/spec"
)

// 本文件提取页面里的栅格图片。ExtractPage 只处理文字，图片图元被直接丢弃，
// 而 DOCX 需要把它们内嵌，所以单独走一遍。
//
// 图层遍历刻意与 appendPageContentText 保持一致（背景层先于其他图层、模板与
// 批注一并处理），否则同一页的文字与图片会落在不同的图层顺序上，两者在块编排
// 阶段按 Y 交错时就会错位。

// Image 是页面里的一张可内嵌图片。
type Image struct {
	// Data 是原始编码字节（PNG/JPEG 等），按原样内嵌，不重新编码。
	Data []byte
	// Extension 是不含点的扩展名，决定 MIME 与文件名。
	Extension string
	// X 与 Y 是图片左上角位置（毫米，原点在页面左上角）。
	X float64
	Y float64
	// Width 与 Height 是显示尺寸（毫米）。
	Width  float64
	Height float64
}

// rasterExtensions 是 WordprocessingML 能直接内嵌的栅格格式。SVG 等矢量格式
// 没有等价表示，转成位图会损失清晰度，因此不列入。
var rasterExtensions = map[string]bool{
	"png": true, "jpeg": true, "jpg": true,
	"gif": true, "bmp": true, "tif": true, "tiff": true,
	"emf": true, "wmf": true,
}

// ExtractImages 提取一页里可内嵌的图片，按阅读顺序（Y 升序，Y 相同按 X 升序）
// 排列，与 Rows 对文字的排序约定一致。
func ExtractImages(doc *parser.Document, page *parser.Page, limit int) []Image {
	if doc == nil || page == nil {
		return nil
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return nil
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return nil
	}

	images := make([]Image, 0, 8)
	for _, template := range content.Template {
		if templateContent := doc.GetTemplate(models.StID(template.TemplateID)); templateContent != nil {
			appendImageItems(doc, templateContent.Content, &images, 0)
		}
	}
	appendImageItems(doc, content.Content, &images, 0)
	if annot := doc.GetAnnotation(page.ID); annot != nil {
		for _, item := range annot.Annots {
			if item == nil || !item.Visible.Value(true) || item.Appearance == nil {
				continue
			}
			appendImageItemsFrom(doc, item.Appearance.Items, &images, 0)
		}
	}
	sortImagesByReadingOrder(images)
	if limit > 0 && len(images) > limit {
		images = images[:limit]
	}
	return images
}

func appendImageItems(doc *parser.Document, content *models.Content, images *[]Image, depth int) {
	if content == nil {
		return
	}
	// 与 appendPageContentText 同样先处理背景层。
	for _, layer := range content.Layer {
		if layer != nil && layer.Type == spec.LayerBackground {
			appendImageItemsFrom(doc, layer.Items, images, depth)
		}
	}
	for _, layer := range content.Layer {
		if layer != nil && layer.Type != spec.LayerBackground {
			appendImageItemsFrom(doc, layer.Items, images, depth)
		}
	}
}

func appendImageItemsFrom(doc *parser.Document, items []models.PageItem, images *[]Image, depth int) {
	if depth > maxTextCompositeDepth {
		return
	}
	for _, item := range items {
		switch item.Kind {
		case models.PageItemImage:
			if image, ok := imageOf(doc, item.Image); ok {
				*images = append(*images, image)
			}
		case models.PageItemBlock:
			appendImageItemsFrom(doc, item.Block.Items, images, depth)
		case models.PageItemComposite:
			if unit := doc.GetCompositeUnit(models.StID(item.Composite.ResourceID)); unit != nil {
				appendImageItemsFrom(doc, unit.Content.Items, images, depth+1)
			}
		}
	}
}

// imageOf 把一个图像图元转成可内嵌的图片条目，尺寸或资源不合法时返回 false。
func imageOf(doc *parser.Document, object *models.ImageObject) (Image, bool) {
	if object == nil || !object.VisibleValue() {
		return Image{}, false
	}
	boundary := object.Boundary
	if !(boundary.Width > 0) || !(boundary.Height > 0) ||
		!Finite(boundary.X) || !Finite(boundary.Y) {
		return Image{}, false
	}

	media := doc.GetMedia(models.StID(object.ResourceID))
	if media == nil {
		if object.Substitution == 0 {
			return Image{}, false
		}
		media = doc.GetMedia(models.StID(object.Substitution))
		if media == nil {
			return Image{}, false
		}
	}
	extension, ok := rasterExtension(media.Format, media.MediaFile)
	if !ok {
		return Image{}, false
	}
	data, err := doc.FileCache.Read(media.MediaFile.Clean().String())
	if err != nil || len(data) == 0 {
		return Image{}, false
	}
	return Image{
		Data:      data,
		Extension: extension,
		X:         boundary.X,
		Y:         boundary.Y,
		Width:     boundary.Width,
		Height:    boundary.Height,
	}, true
}

// rasterExtension 返回可内嵌的栅格格式扩展名，不含点。
func rasterExtension(format string, location models.StLoc) (string, bool) {
	if strings.EqualFold(format, "SVG") {
		return "", false
	}
	extension := strings.ToLower(location.Ext())
	if !rasterExtensions[strings.TrimPrefix(extension, ".")] {
		return "", false
	}
	return strings.TrimPrefix(extension, "."), true
}

func sortImagesByReadingOrder(images []Image) {
	sort.SliceStable(images, func(i, j int) bool {
		if images[i].Y != images[j].Y {
			return images[i].Y < images[j].Y
		}
		return images[i].X < images[j].X
	})
}
