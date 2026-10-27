// Package svg 注册自包含 SVG→OFD 与 SVG→PDF 转换，依赖 Chrome/Chromium。
// SVG 作为独立图片文档等比放入所选纸张，PDF 保留浏览器支持的矢量内容。
//
//	import _ "github.com/zc310/ofd/pkg/converter/import/svg"
//
// 使用 converter.Convert(ctx, "svg", "ofd", input, output)，纸张沿用
// converter.WithPaperSize、WithPaperDimensions 与 WithLandscape。
package svg

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/internal/convert/htmlbridge"
	"github.com/zc310/ofd/pkg/converter"
	"golang.org/x/net/html/charset"
)

func prepareHTML(input any, conv *converter.Converter) ([]byte, error) {
	ctx := conv.Context()
	data, err := htmlbridge.ReadInput(ctx, input, 16<<20)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	// 验证单个 SVG 根节点，避免损坏或误命名输入被浏览器当成空白图片。
	decoder := xml.NewDecoder(contextio.Reader(ctx, bytes.NewReader(data)))
	decoder.CharsetReader = charset.NewReaderLabel
	depth, roots := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("SVG XML 解析失败: %w", err)
		}
		switch item := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots != 1 || item.Name.Local != "svg" || item.Name.Space != "http://www.w3.org/2000/svg" {
					return nil, fmt.Errorf("SVG 必须有唯一的 svg 根节点及 http://www.w3.org/2000/svg 命名空间")
				}
			}
			depth++
			if depth > 128 {
				return nil, fmt.Errorf("SVG 嵌套超过 128 层")
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(item)) != 0 {
				return nil, fmt.Errorf("SVG 根节点之外存在文字")
			}
		}
	}
	if roots != 1 || depth != 0 {
		return nil, fmt.Errorf("SVG 内容为空或不完整")
	}
	width, height := conv.Paper().EffectiveDimensions()
	// 图片上下文独立于 HTML 样式与脚本；使用完整页框等比缩放，不截断宽图。
	page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>SVG</title>
<style>@page{margin:0}html,body{margin:0;padding:0}img{display:block;width:%gmm;height:%gmm;object-fit:contain}</style>
</head><body><img alt="SVG" src="data:image/svg+xml;base64,%s"></body></html>`, width, height, base64.StdEncoding.EncodeToString(data))
	return []byte(page), nil
}

func init() {
	importer := &htmlbridge.Importer{Format: "svg", MIMEType: "image/svg+xml", Prepare: func(input any, conv *converter.Converter) (htmlbridge.Document, error) {
		data, err := prepareHTML(input, conv)
		return htmlbridge.Document{HTML: data}, err
	}}
	converter.RegisterImporter(importer)
	converter.RegisterTransformer(htmlbridge.PDFTransformer{Importer: importer})
}
