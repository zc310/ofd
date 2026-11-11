package session

import (
	"context"
	"io"

	"github.com/zc310/ofd/pkg/reader"
)

// RenderPDF 与页面图片共用有界渲染名额；取消或出错时 output 可能包含部分数据，调用方应丢弃。
func (s *Session) RenderPDF(ctx context.Context, output io.Writer, indices []int, options reader.RenderOptions) error {
	_, err := limited(ctx, s, s.renders, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.doc.RenderPDF(ctx, output, indices, options)
	})
	return err
}

// RenderPage 渲染单页；取消或出错时 output 可能包含部分数据，调用方应丢弃。
// 排队及渲染都受 ctx 和会话关闭控制，底层绘制的取消粒度由 reader.RenderPage 决定。
func (s *Session) RenderPage(ctx context.Context, output io.Writer, index int, options reader.RenderOptions) error {
	_, err := limited(ctx, s, s.renders, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.doc.RenderPage(ctx, output, index, options)
	})
	return err
}
