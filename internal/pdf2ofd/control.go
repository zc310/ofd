package pdf2ofd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/pdf2ofd/config"
	"github.com/zc310/ofd/pkg/creator"
)

type Options = config.Options
type Limits = config.Limits
type Diagnostic = config.Diagnostic
type LimitError = config.LimitError

var ErrLimitExceeded = config.ErrLimitExceeded
var ErrUnsupported = config.ErrUnsupported

type streamKey struct {
	start  *byte
	length int
}

func keyForStream(s *types.StreamDict) streamKey {
	data := s.Raw
	if len(data) == 0 {
		data = s.Content
	}
	if len(data) == 0 {
		return streamKey{}
	}
	return streamKey{&data[0], len(data)}
}

type conversionControl struct {
	ctx                                 context.Context
	options                             Options
	err                                 error
	decoded, pixels, output, operations int64
	streams                             map[streamKey]bool
}

func (c *conversionControl) fail(err error) error {
	if c != nil && c.err == nil {
		c.err = err
	}
	return err
}

func (c *conversionControl) check() error {
	if c == nil {
		return nil
	}
	if err := c.ctx.Err(); err != nil {
		return c.fail(err)
	}
	return c.err
}

func (c *conversionControl) limit(resource string, actual, maximum int64) error {
	return c.fail(&LimitError{Resource: resource, Actual: actual, Limit: maximum})
}

// consumeOperations 先预留处理量，再分配或展开，防止短输入放大为巨量文字映射。
func (c *conversionControl) consumeOperations(count int64) error {
	if c == nil {
		return nil
	}
	if err := c.check(); err != nil {
		return err
	}
	if count > c.options.Limits.MaxOperations-c.operations {
		return c.limit("operations", c.options.Limits.MaxOperations+1, c.options.Limits.MaxOperations)
	}
	c.operations += count
	return nil
}

var parserObjectLimit = regexp.MustCompile(`(?:trailer Size|"Size"|object stream object count|object stream N|xref entry count) (\d+) exceeds limit (\d+)`)
var parserXRefLimit = regexp.MustCompile(`xref (?:entry count|subsection object range) exceeds limit (\d+)`)
var parserStreamLimit = regexp.MustCompile(`stream length (\d+) exceeds maximum supported length (\d+)`)
var parserBlindStreamLimit = regexp.MustCompile(`stream content exceeds maximum blind read length (\d+)`)

// parserError 适配 pdfcpu 尚未导出哨兵的 xref/流大小错误。仅匹配已知的格式，
// 保留原始原因，避免把普通格式错误或不相关的库内限制误判为本次预算超限。
func (c *conversionControl) parserError(err error) error {
	if err == nil {
		return nil
	}
	l := c.options.Limits
	var resource string
	var actual, maximum int64
	switch {
	case errors.Is(err, filter.ErrDecodeLimitExceeded):
		resource, actual, maximum = "decoded_stream_bytes", l.MaxDecodedStreamBytes+1, l.MaxDecodedStreamBytes
	case errors.Is(err, model.ErrMaxRecursionDepthExceeded):
		resource, actual, maximum = "parser_depth", l.MaxDepth+1, l.MaxDepth
	case errors.Is(err, model.ErrInputSizeLimit):
		resource, actual, maximum = "input_bytes", l.MaxInputBytes+1, l.MaxInputBytes
	default:
		if match := parserObjectLimit.FindStringSubmatch(err.Error()); len(match) > 0 {
			actual, _ = strconv.ParseInt(match[1], 10, 64)
			maximum, _ = strconv.ParseInt(match[2], 10, 64)
			if maximum == l.MaxObjects {
				resource = "objects"
			}
		} else if match := parserXRefLimit.FindStringSubmatch(err.Error()); len(match) > 0 {
			maximum, _ = strconv.ParseInt(match[1], 10, 64)
			actual = maximum + 1
			if maximum == l.MaxObjects {
				resource = "objects"
			}
		} else if match := parserStreamLimit.FindStringSubmatch(err.Error()); len(match) > 0 {
			actual, _ = strconv.ParseInt(match[1], 10, 64)
			maximum, _ = strconv.ParseInt(match[2], 10, 64)
			if maximum == l.MaxStreamBytes {
				resource = "stream_bytes"
			}
		} else if match := parserBlindStreamLimit.FindStringSubmatch(err.Error()); len(match) > 0 {
			maximum, _ = strconv.ParseInt(match[1], 10, 64)
			actual = maximum + 1
			if maximum == l.MaxStreamBytes {
				resource = "stream_bytes"
			}
		}
	}
	if resource == "" {
		return err
	}
	return c.fail(fmt.Errorf("%w: %w", &LimitError{Resource: resource, Actual: actual, Limit: maximum}, err))
}

// accountParsedStreams 纳入解析器为 xref/对象流提前完成的解码；普通流仍按需解码。
func (c *conversionControl) accountParsedStreams(ctx *model.Context) error {
	for _, entry := range ctx.Table {
		if err := c.check(); err != nil {
			return err
		}
		if entry == nil {
			continue
		}
		var stream *types.StreamDict
		switch value := entry.Object.(type) {
		case types.StreamDict:
			stream = &value
		case *types.StreamDict:
			stream = value
		case types.ObjectStreamDict:
			stream = &value.StreamDict
		case *types.ObjectStreamDict:
			if value != nil {
				stream = &value.StreamDict
			}
		case types.XRefStreamDict:
			stream = &value.StreamDict
		case *types.XRefStreamDict:
			if value != nil {
				stream = &value.StreamDict
			}
		}
		if stream != nil && len(stream.Content) > 0 {
			if err := c.decodeStream(stream, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *pdfInterpreter) report(code, message string) {
	if p.control == nil || p.control.check() != nil {
		return
	}
	d := p.source
	d.Code, d.Message = code, message
	if f := p.control.options.OnDiagnostic; f != nil {
		f(d)
	}
	if err := p.control.check(); err != nil {
		return
	}
	if p.control.options.Strict {
		p.control.fail(fmt.Errorf("%w: 第 %d 页 %s: %s", ErrUnsupported, d.Page, d.Resource, message))
	}
}

func (p *pdfInterpreter) setSource(object types.Object, resource string) Diagnostic {
	previous := p.source
	p.source.Resource, p.source.Object, p.source.Generation = resource, 0, 0
	if ref, ok := object.(types.IndirectRef); ok {
		p.source.Object, p.source.Generation = int(ref.ObjectNumber), int(ref.GenerationNumber)
	}
	return previous
}

func (p *pdfInterpreter) appendItem(item creator.Item) bool {
	// 恒黑亮度掩码是精确的零透明度，不需要栅格化矢量图元。图片继续使用
	// 原有的图片掩码合成路径，保留其分辨率与缓存语义。
	if p.state.constantMaskBlack {
		switch value := item.(type) {
		case creator.Path:
			value.Alpha = ofdAlpha(0)
			item = value
		case creator.Text:
			item = p.invisibleTransparencyText(value)
		}
	}
	if p.transparency != nil || p.state.softMask != nil || !isNormalBlendMode(p.state.blendMode) {
		if err := p.compositeTransparencyItem(item); err != nil {
			if p.control != nil {
				p.control.fail(err)
			}
			return false
		}
		return true
	}
	return p.appendRawItem(item)
}

func (p *pdfInterpreter) appendRawItem(item creator.Item) bool {
	for _, clip := range p.state.clips {
		if clip.empty {
			if _, text := item.(creator.Text); !text {
				return false
			}
		}
	}
	if p.control != nil {
		if p.control.check() != nil {
			return false
		}
		if p.control.output >= p.control.options.Limits.MaxOutputObjects {
			p.control.limit("output_objects", p.control.output+1, p.control.options.Limits.MaxOutputObjects)
			return false
		}
		p.control.output++
	}
	p.page.Items = append(p.page.Items, item)
	return true
}

func (p *pdfInterpreter) decodeStream(s *types.StreamDict, specificLimit int64) error {
	if p.control == nil {
		return s.Decode()
	}
	return p.control.decodeStream(s, specificLimit)
}

func (c *conversionControl) decodeStream(s *types.StreamDict, specificLimit int64) error {
	if err := c.check(); err != nil {
		return err
	}
	l := c.options.Limits
	if int64(len(s.Raw)) > l.MaxStreamBytes {
		return c.limit("stream_bytes", int64(len(s.Raw)), l.MaxStreamBytes)
	}
	if len(s.Raw) == 0 && len(s.Content) == 0 {
		return nil
	}
	maximum, resource := l.MaxDecodedStreamBytes, "decoded_stream_bytes"
	if specificLimit > 0 && specificLimit < maximum {
		maximum, resource = specificLimit, "font_bytes"
	}
	key := keyForStream(s)
	seen := c.streams[key]
	remaining := l.MaxTotalDecodedBytes - c.decoded
	if !seen && remaining < maximum {
		maximum, resource = remaining, "total_decoded_bytes"
	}
	if maximum <= 0 {
		return c.limit("total_decoded_bytes", c.decoded+1, l.MaxTotalDecodedBytes)
	}
	if err := s.DecodeWithLimit(maximum); err != nil {
		if errors.Is(err, filter.ErrDecodeLimitExceeded) {
			if resource == "total_decoded_bytes" {
				return c.limit(resource, l.MaxTotalDecodedBytes+1, l.MaxTotalDecodedBytes)
			}
			return c.limit(resource, maximum+1, maximum)
		}
		return err
	}
	if int64(len(s.Content)) > maximum {
		if resource == "total_decoded_bytes" {
			return c.limit(resource, c.decoded+int64(len(s.Content)), l.MaxTotalDecodedBytes)
		}
		return c.limit(resource, int64(len(s.Content)), maximum)
	}
	if !seen {
		if c.streams == nil {
			c.streams = make(map[streamKey]bool)
		}
		c.streams[key] = true
		c.decoded += int64(len(s.Content))
	}
	return nil
}

func (c *conversionControl) imagePixels(width, height int64) (int64, error) {
	if width <= 0 || height <= 0 {
		return 0, errors.New("PDF 图像尺寸必须为正数")
	}
	maximum := c.options.Limits.MaxImagePixels
	// 用除法检查乘积，既限制像素数，又避免 width*height 的整数溢出。
	if width > maximum/height {
		return 0, c.limit("image_pixels", maximum+1, maximum)
	}
	pixels := width * height
	// 最大的输出缓冲区是 RGBA64；在 32 位/wasm 平台上也必须能安全分配。
	if pixels > int64(int(^uint(0)>>1))/8 {
		return 0, c.limit("image_pixels_platform", pixels, int64(int(^uint(0)>>1))/8)
	}
	return pixels, nil
}

func (c *conversionControl) addImagePixels(pixels int64) error {
	l := c.options.Limits.MaxTotalImagePixels
	if pixels > l-c.pixels {
		return c.limit("total_image_pixels", l+1, l)
	}
	c.pixels += pixels
	return nil
}

// controlledPageContent 在拼接页面的多个内容流之前检查各流和合计大小。
func controlledPageContent(ctx *model.Context, page types.Dict, c *conversionControl) ([]byte, error) {
	object, err := ctx.XRefTable.Dereference(page["Contents"])
	if err != nil || object == nil {
		return nil, err
	}
	objects := types.Array{object}
	if array, ok := object.(types.Array); ok {
		objects = array
	}
	var result []byte
	for _, object := range objects {
		if err := c.check(); err != nil {
			return nil, err
		}
		stream, _, err := ctx.XRefTable.DereferenceStreamDict(object)
		if err != nil {
			return nil, err
		}
		if stream == nil {
			continue
		}
		if err := c.decodeStream(stream, 0); err != nil {
			return nil, err
		}
		if int64(len(stream.Content)) > c.options.Limits.MaxDecodedStreamBytes-int64(len(result)) {
			return nil, c.limit("page_content_bytes", int64(len(result))+int64(len(stream.Content)), c.options.Limits.MaxDecodedStreamBytes)
		}
		result = append(result, stream.Content...)
	}
	return result, nil
}
