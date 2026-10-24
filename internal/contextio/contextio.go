// Package contextio 在读写边界检查取消，不负责中断底层已经阻塞的 I/O。
package contextio

import (
	"context"
	"io"
	"reflect"
)

// Reader 在每次读取前后检查 ctx，取消时原样返回 ctx.Err()。
// 不可取消的上下文直接复用 input；同一上下文不会重复包装。
func Reader(ctx context.Context, input io.Reader) io.Reader {
	if wrapped, ok := input.(reader); ok && sameContext(ctx, wrapped.ctx) {
		return input
	}
	if ctx.Done() == nil {
		return input
	}
	return reader{ctx: ctx, input: input}
}

type reader struct {
	ctx   context.Context
	input io.Reader
}

func (r reader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.input.Read(data)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

// Writer 在每次写入前后检查 ctx，取消时原样返回 ctx.Err()。
// 保留字符串写入快速路径；同一上下文不会重复包装。
func Writer(ctx context.Context, output io.Writer) io.Writer {
	if wrapped, ok := output.(writer); ok && sameContext(ctx, wrapped.ctx) {
		return output
	}
	return writer{ctx: ctx, output: output}
}

type writer struct {
	ctx    context.Context
	output io.Writer
}

func (w writer) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.output.Write(data)
	if canceled := w.ctx.Err(); canceled != nil {
		return n, canceled
	}
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (w writer) WriteString(data string) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := io.WriteString(w.output, data)
	if canceled := w.ctx.Err(); canceled != nil {
		return n, canceled
	}
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}

// 只复用同一个上下文实例。Value 的身份比较不会比较自定义 Context 的字段，
// 因此包含切片等不可比较字段的 Context 也不会 panic；无法确认同一实例时保留包装。
func sameContext(a, b context.Context) bool {
	return reflect.ValueOf(a) == reflect.ValueOf(b)
}
