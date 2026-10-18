package compare

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/url"
	"strings"

	"github.com/goccy/go-json"
)

type byteBudget struct {
	remaining int64
	label     string
}

func (b *byteBudget) take(n int64) error {
	if n > b.remaining {
		return fmt.Errorf("%s超过累计字节预算，请调整相应限制", b.label)
	}
	b.remaining -= n
	return nil
}

type budgetWriter struct {
	w      io.Writer
	budget *byteBudget
}

func (w budgetWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.budget.remaining {
		return 0, fmt.Errorf("%s超过累计字节预算，请调整相应限制", w.budget.label)
	}
	n, err := w.w.Write(p)
	w.budget.remaining -= int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

// 按每个保留条目的紧凑 JSON 大小扣预算，避免累计输入和明细占用过多内存。
// 输出时另按实际字节检查，覆盖缩进、模板和重复展示造成的增长。
func reserveData(options Options, value any) error {
	if options.dataBudget == nil {
		return nil
	}
	return json.NewEncoder(budgetWriter{io.Discard, options.dataBudget}).Encode(value)
}

type imageStore struct {
	remaining int64
	writer    func([]byte) (string, error)
}

func (s *imageStore) encode(ctx context.Context, img image.Image) (string, error) {
	var buf bytes.Buffer
	budget := &byteBudget{remaining: s.remaining, label: "预览图"}
	if err := png.Encode(budgetWriter{contextWriter{ctx, &buf}, budget}, img); err != nil {
		return "", err
	}
	s.remaining = budget.remaining
	if s.writer != nil {
		uri, err := s.writer(buf.Bytes())
		if err != nil {
			return "", err
		}
		if !relativeImageURL(uri) {
			return "", fmt.Errorf("外部图片地址必须为安全的相对路径: %q", uri)
		}
		return uri, ctx.Err()
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), ctx.Err()
}

func encodePreview(ctx context.Context, img image.Image, options Options) (string, error) {
	store := options.images
	if store == nil {
		limit := options.MaxPreviewBytes
		if limit == 0 {
			limit = 64 << 20
		}
		store = &imageStore{remaining: limit, writer: options.ImageWriter}
	}
	return store.encode(ctx, img)
}

type contextWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

// 只接受相对 PNG 路径，避免报告中的链接触发网络请求或其他 URL 协议。
func relativeImageURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || value == "" || u.IsAbs() || u.Host != "" || u.ForceQuery || u.RawQuery != "" || strings.Contains(value, "#") || strings.HasPrefix(u.Path, "/") || strings.ContainsAny(u.Path, "\\\x00\r\n") || !strings.HasSuffix(strings.ToLower(u.Path), ".png") {
		return false
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "" || part == "." || part == ".." || strings.Contains(part, ":") {
			return false
		}
	}
	return true
}
