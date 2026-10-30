package reader

import (
	"errors"
	"fmt"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render"
)

// LayerInfo 是页面的正文图层或模板引用，不包含注解及签章。
type LayerInfo struct {
	// Key 用于 SetHiddenLayers；正文层为 ID，模板为 template:<ID>。
	Key     string
	ID      string
	Kind    string
	Name    string
	Type    string
	Objects int
	Visible bool
}

// Layers 按文件顺序返回指定页的图层；仅按需加载这一页。
func (r *Reader) Layers(index int) ([]LayerInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.layersLocked(index)
}

func (r *Reader) layersLocked(index int) ([]LayerInfo, error) {
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	layers := make([]LayerInfo, 0)
	err := r.pages[index].page.WithPageContent(func(content *models.PageContent) error {
		if content == nil {
			return nil
		}
		if content.Content != nil {
			for _, layer := range content.Content.Layer {
				if layer == nil {
					continue
				}
				typ := layer.Type
				if typ == "" {
					typ = "Body"
				}
				id := formatStID(layer.ID)
				layers = append(layers, LayerInfo{Key: id, ID: id, Kind: "layer", Type: typ, Objects: len(layer.Items), Visible: !r.hiddenLayers[index].HiddenLayers[layer.ID]})
			}
		}
		seen := make(map[models.StID]bool)
		for _, ref := range content.Template {
			id := models.StID(ref.TemplateID)
			if seen[id] {
				continue
			}
			seen[id] = true
			typ := "Background"
			if r.pages[index].document.TemplateForeground(ref) {
				typ = "Foreground"
			}
			info := LayerInfo{Key: "template:" + formatStID(id), ID: formatStID(id), Kind: "template", Type: typ, Visible: !r.hiddenLayers[index].HiddenTemplates[id]}
			for _, definition := range r.pages[index].document.CommonData.TemplatePages {
				if definition.ID == id && definition.Name != nil {
					info.Name = *definition.Name
					break
				}
			}
			template, err := r.pages[index].document.LoadTemplate(id)
			if err != nil {
				return fmt.Errorf("读取模板 %s 失败: %w", info.ID, err)
			}
			if template != nil && template.Content != nil {
				for _, layer := range template.Content.Layer {
					if layer != nil {
						info.Objects += len(layer.Items)
					}
				}
			}
			layers = append(layers, info)
		}
		return nil
	})
	return layers, err
}

// SetHiddenLayers 替换单页隐藏项列表，使用 Layers 返回的 Key，空列表恢复全部显示。
// 设置只影响当前 Reader 的显示、文字、搜索、链接和渲染导出，不改写源 OFD。
// 同一页面索引在版本切换后可能指向不同页面，SelectVersion 会清空该设置。
func (r *Reader) SetHiddenLayers(index int, ids []string) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	layers, err := r.layersLocked(index)
	if err != nil {
		return err
	}
	known := make(map[string]LayerInfo, len(layers))
	for _, layer := range layers {
		known[layer.Key] = layer
	}
	hidden := render.PageVisibility{HiddenLayers: make(map[models.StID]bool), HiddenTemplates: make(map[models.StID]bool)}
	for _, id := range ids {
		value, ok := known[id]
		if !ok {
			return fmt.Errorf("第 %d 页不存在图层 %q", index+1, id)
		}
		var valueID models.StID
		if err := valueID.UnmarshalText([]byte(value.ID)); err != nil {
			return err
		}
		if value.Kind == "template" {
			hidden.HiddenTemplates[valueID] = true
		} else {
			hidden.HiddenLayers[valueID] = true
		}
	}
	if r.hiddenLayers == nil {
		r.hiddenLayers = make(map[int]render.PageVisibility)
	}
	if len(ids) == 0 {
		delete(r.hiddenLayers, index)
	} else {
		r.hiddenLayers[index] = hidden
	}
	r.text = newTextCache()
	r.search = newSearchCache(len(r.pages))
	r.metadata = &readerMetadata{}
	return nil
}

func (r *Reader) visibleLayers(index int, layers []*models.Layer) []*models.Layer {
	hidden := r.hiddenLayers[index].HiddenLayers
	if len(hidden) == 0 {
		return layers
	}
	visible := make([]*models.Layer, 0, len(layers))
	for _, layer := range layers {
		if layer != nil && !hidden[layer.ID] {
			visible = append(visible, layer)
		}
	}
	return visible
}
