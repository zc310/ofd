package pdf2ofd

import "github.com/zc310/ofd/internal/models"

// layerTexts 返回图层文档序列表中的全部文字对象。
func layerTexts(layer *models.Layer) []models.TextObject {
	var out []models.TextObject
	for i := range layer.Items {
		if layer.Items[i].Kind == models.PageItemText {
			out = append(out, layer.Items[i].Text)
		}
	}
	return out
}

// layerPaths 返回图层文档序列表中的全部路径对象。
func layerPaths(layer *models.Layer) []models.PathObject {
	var out []models.PathObject
	for i := range layer.Items {
		if layer.Items[i].Kind == models.PageItemPath {
			out = append(out, layer.Items[i].Path)
		}
	}
	return out
}

// layerImages 返回图层文档序列表中的全部图像对象。
func layerImages(layer *models.Layer) []models.ImageObject {
	var out []models.ImageObject
	for i := range layer.Items {
		if layer.Items[i].Kind == models.PageItemImage {
			out = append(out, layer.Items[i].Image)
		}
	}
	return out
}
