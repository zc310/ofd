package export

import (
	"errors"
	"fmt"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
)

func (e *documentExporter) exportPages(result *manifest.Manifest) error {
	for index, page := range e.document.Pages {
		if page == nil {
			continue
		}
		var content *models.PageContent
		err := page.WithPageContent(func(value *models.PageContent) error {
			content = value
			return nil
		})
		if err != nil {
			return fmt.Errorf("读取第 %d 页失败: %w", index+1, err)
		}
		converted, err := e.convertPageContent(content)
		if err != nil {
			return fmt.Errorf("转换第 %d 页失败: %w", index+1, err)
		}
		if err := e.exportPageResources(index, page, &converted); err != nil {
			return fmt.Errorf("导出第 %d 页资源失败: %w", index+1, err)
		}
		result.Pages = append(result.Pages, converted)
	}
	return nil
}

func (e *documentExporter) exportTemplates(result *manifest.Manifest) error {
	for _, definition := range e.document.CommonData.TemplatePages {
		content, err := e.document.LoadTemplate(definition.ID)
		if err != nil {
			return fmt.Errorf("加载模板页 %d 失败: %w", definition.ID, err)
		}
		if content == nil {
			return fmt.Errorf("模板页 %d 内容为空", definition.ID)
		}
		page, err := e.convertPageContent(content)
		if err != nil {
			return fmt.Errorf("转换模板页 %d 失败: %w", definition.ID, err)
		}
		name := valueOrEmpty(definition.Name)
		result.Templates = append(result.Templates, manifest.Template{ID: uint64(definition.ID), Name: name, ZOrder: definition.ZOrder.String(), Area: page.Area, Layers: page.Layers, Items: page.Items})
	}
	return nil
}

func (e *documentExporter) convertPageContent(content *models.PageContent) (manifest.Page, error) {
	page := manifest.Page{}
	if content == nil {
		return page, errors.New("页面内容为空")
	}
	page.Area = exportPageArea(content.Area)
	for _, template := range content.Template {
		page.Templates = append(page.Templates, manifest.TemplateRef{ID: uint64(template.TemplateID), ZOrder: template.ZOrder.String()})
	}
	if content.Actions != nil {
		actions, err := e.exportActionList(content.Actions.Action)
		if err != nil {
			return page, fmt.Errorf("转换页面动作失败: %w", err)
		}
		page.Actions = actions
	}
	if content.Content == nil {
		return page, nil
	}
	if len(content.Content.Layer) > 0 {
		for _, layer := range content.Content.Layer {
			if layer == nil {
				continue
			}
			items, err := e.convertItems(layer.Items)
			if err != nil {
				return page, err
			}
			page.Layers = append(page.Layers, manifest.Layer{Type: layer.Type, DrawParam: e.drawParamName(layer.DrawParam), Items: items})
		}
		return page, nil
	}
	return page, nil
}

func exportPageArea(area *models.CtPageArea) *manifest.PageArea {
	if area == nil {
		return nil
	}
	return &manifest.PageArea{PhysicalBox: exportBox(&area.PhysicalBox), ApplicationBox: exportBox(area.ApplicationBox), ContentBox: exportBox(area.ContentBox), BleedBox: exportBox(area.BleedBox)}
}
