package contextio

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestCanceledIOStopsBeforeCallingUnderlyingStream(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	input := Reader(ctx, readerFunc(func([]byte) (int, error) { called = true; return 0, io.EOF }))
	output := Writer(ctx, writerFunc(func(p []byte) (int, error) { called = true; return len(p), nil }))
	if _, err := input.Read(make([]byte, 1)); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("x")); err != context.Canceled {
		t.Fatal(err)
	}
	if called {
		t.Fatal("取消后仍调用底层 I/O")
	}
}

func TestCancellationDuringIOIsReturnedWithoutWrapping(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	input := Reader(ctx, readerFunc(func(p []byte) (int, error) { cancel(); return copy(p, "data"), nil }))
	if _, err := io.ReadAll(input); err != context.Canceled {
		t.Fatalf("读取过程中取消 = %v", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	output := Writer(ctx, writerFunc(func(p []byte) (int, error) { cancel(); return len(p), nil }))
	if _, err := io.Copy(output, strings.NewReader("data")); err != context.Canceled {
		t.Fatalf("写入过程中取消 = %v", err)
	}
}

func TestWriterReportsShortWrite(t *testing.T) {
	w := Writer(t.Context(), writerFunc(func([]byte) (int, error) { return 0, nil }))
	if _, err := w.Write([]byte("data")); err != io.ErrShortWrite {
		t.Fatal(err)
	}
}

func TestReaderPreservesUnderlyingInterfacesWithoutCancellation(t *testing.T) {
	input := strings.NewReader("data")
	wrapped := Reader(context.Background(), input)
	if wrapped != input {
		t.Fatal("不可取消的 Reader 不应增加包装")
	}
	if _, ok := wrapped.(io.WriterTo); !ok {
		t.Fatal("丢失了底层 WriterTo 快速路径")
	}
}

func TestRepeatedWrappingReusesSameContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := Reader(ctx, strings.NewReader("data"))
	if repeated := Reader(ctx, input); repeated != input {
		t.Fatal("同一上下文重复包装了 Reader")
	}
	output := Writer(ctx, io.Discard)
	if repeated := Writer(ctx, output); repeated != output {
		t.Fatal("同一上下文重复包装了 Writer")
	}
	cancel()
	if _, err := input.Read(make([]byte, 1)); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("data")); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestDifferentContextsKeepIndependentCancellation(t *testing.T) {
	for _, cancelOuter := range []bool{false, true} {
		inner, cancelInner := context.WithCancel(t.Context())
		outer, cancelOuterContext := context.WithCancel(t.Context())
		var output bytes.Buffer
		wrapped := Writer(outer, Writer(inner, &output))
		input := Reader(outer, Reader(inner, strings.NewReader("data")))
		if cancelOuter {
			cancelOuterContext()
		} else {
			cancelInner()
		}
		if _, err := wrapped.Write([]byte("data")); err != context.Canceled {
			t.Fatal(err)
		}
		if _, err := input.Read(make([]byte, 1)); err != context.Canceled {
			t.Fatal(err)
		}
		cancelInner()
		cancelOuterContext()
		if output.Len() != 0 {
			t.Fatal("取消后仍写入底层流")
		}
	}
}

type sharedDoneContext struct {
	context.Context
	err error
}

func (c sharedDoneContext) Err() error {
	if c.Context.Err() != nil {
		return c.err
	}
	return nil
}

func TestSharedDoneChannelDoesNotMergeDifferentContexts(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	inner := sharedDoneContext{Context: parent, err: context.Canceled}
	outer := sharedDoneContext{Context: parent, err: context.DeadlineExceeded}
	r := Reader(outer, Reader(inner, strings.NewReader("data")))
	w := Writer(outer, Writer(inner, io.Discard))
	cancel()
	if _, err := r.Read(make([]byte, 1)); err != context.DeadlineExceeded {
		t.Fatalf("错误地复用了内层读取上下文: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != context.DeadlineExceeded {
		t.Fatalf("错误地复用了内层写入上下文: %v", err)
	}
}

type nonComparableContext struct {
	context.Context
	values []int
}
type interfaceValueContext struct {
	context.Context
	value any
}

func TestWrappingCustomNonComparableContextsDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for _, custom := range []context.Context{
		nonComparableContext{Context: ctx, values: []int{1}},
		interfaceValueContext{Context: ctx, value: []int{1}},
	} {
		input := Reader(custom, Reader(custom, strings.NewReader("data")))
		output := Writer(custom, Writer(custom, io.Discard))
		if n, err := io.Copy(output, input); err != nil || n != 4 {
			t.Fatalf("自定义上下文读写 = %d, %v", n, err)
		}
	}
}

type stringOutput struct {
	bytesWrites, stringWrites int
	cancel                    context.CancelFunc
	short                     bool
}

func (w *stringOutput) Write(p []byte) (int, error) { w.bytesWrites++; return len(p), nil }
func (w *stringOutput) WriteString(s string) (int, error) {
	w.stringWrites++
	if w.cancel != nil {
		w.cancel()
	}
	if w.short {
		return len(s) - 1, nil
	}
	return len(s), nil
}

func TestWriterForwardsStringWritesAndNormalizesShortWrites(t *testing.T) {
	for _, short := range []bool{false, true} {
		output := &stringOutput{short: short}
		_, err := io.WriteString(Writer(context.Background(), output), "data")
		if (short && err != io.ErrShortWrite) || (!short && err != nil) {
			t.Fatal(err)
		}
		if output.stringWrites != 1 || output.bytesWrites != 0 {
			t.Fatalf("未使用字符串快速路径: %+v", output)
		}
	}
	if _, err := Writer(context.Background(), writerFunc(func([]byte) (int, error) { return 0, nil })).Write([]byte("data")); err != io.ErrShortWrite {
		t.Fatal(err)
	}
}

func TestStringWritesPreserveCancellationBeforeAndAfterIO(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	output := &stringOutput{cancel: cancel}
	w := Writer(ctx, output)
	if n, err := io.WriteString(w, "data"); err != context.Canceled || n != 4 {
		t.Fatalf("字符串写入中取消 = %d, %v", n, err)
	}
	if n, err := io.WriteString(w, "late"); err != context.Canceled || n != 0 {
		t.Fatalf("取消后字符串写入 = %d, %v", n, err)
	}
	if output.stringWrites != 1 || output.bytesWrites != 0 {
		t.Fatalf("取消后仍访问底层流: %+v", output)
	}
}

func TestStringWritesFallBackToByteWriter(t *testing.T) {
	writes := 0
	w := Writer(t.Context(), writerFunc(func(p []byte) (int, error) { writes++; return len(p), nil }))
	if n, err := io.WriteString(w, "data"); err != nil || n != 4 || writes != 1 {
		t.Fatalf("字符串写入回退失败: %d, %v, writes=%d", n, err, writes)
	}
}
