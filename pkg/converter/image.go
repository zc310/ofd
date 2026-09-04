package converter

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

// Image 渲染OFD文档
func Image(input interface{}, opts ...Option) error {
	conv := newConverter(opts...)

	// 验证配置
	if err := conv.validateConfig(); err != nil {
		return err
	}

	// 解析 OFD
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return fmt.Errorf("解析OFD失败: %w", err)
	}
	defer func() {
		if err := ofd.Close(); err != nil {
			slog.Error("关闭OFD文档失败", "error", err)
		}
	}()

	// 验证文档
	if len(ofd.Documents) == 0 {
		return errors.New("没有文档")
	}

	// 创建渲染文档。每个文档体保留独立的资源上下文。
	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(conv.bgColor, document))
	}
	if len(collectDocumentPages(documents)) == 0 {
		return errors.New("文档没有页面")
	}

	return conv.renderDocuments(documents)
}

// ImageDocument 将已解析的 OFD 文档渲染为图像或矢量格式。
func ImageDocument(doc *render.Document, opts ...Option) error {
	conv := newConverter(opts...)
	if len(collectDocumentPages([]*render.Document{doc})) == 0 {
		return errors.New("文档没有页面")
	}
	return conv.renderDocuments([]*render.Document{doc})
}

// ImageDocuments 将多个已解析的 OFD 文档体按全局页码渲染为图像或矢量格式。
func ImageDocuments(documents []*render.Document, opts ...Option) error {
	conv := newConverter(opts...)
	if len(collectDocumentPages(documents)) == 0 {
		return errors.New("文档没有页面")
	}
	if err := conv.validateConfig(); err != nil {
		return err
	}
	return conv.renderDocuments(documents)
}
