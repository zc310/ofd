package preserve

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/replace"
)

type partialOutput struct {
	err     error
	written int
}

func (w *partialOutput) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.written = len(data) / 2
	return w.written, nil
}

func TestApplyDetectsShortWriteAndDoesNotConfirmDeletions(t *testing.T) {
	w := &partialOutput{}
	result, err := Apply(buildWithOrphan(t), w, Options{DocType: "OFD-A", DropUnreferenced: true})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("短写应返回 io.ErrShortWrite: %v", err)
	}
	if w.written == 0 {
		t.Fatal("样例应触发部分写入")
	}
	if result.UnreferencedDropped != 0 {
		t.Fatalf("输出不完整时不能报告已完成删除: %+v", result)
	}
}

func TestApplyPreservesOutputErrorAndDoesNotConfirmDeletions(t *testing.T) {
	want := errors.New("output unavailable")
	w := &partialOutput{err: want}
	result, err := Apply(buildWithOrphan(t), w, Options{DocType: "OFD-A", DropUnreferenced: true})
	if !errors.Is(err, want) {
		t.Fatalf("输出错误应可识别: %v", err)
	}
	if result.UnreferencedDropped != 0 {
		t.Fatalf("输出失败时不应报告实际删除: %+v", result)
	}
}

func TestApplyConfirmsDeletionsOnlyAfterSuccessfulOutput(t *testing.T) {
	var output bytes.Buffer
	result, err := Apply(buildWithOrphan(t), &output, Options{DocType: "OFD-A", DropUnreferenced: true})
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 || result.UnreferencedDropped != 1 {
		t.Fatalf("完整写出后应报告删除数: %+v", result)
	}
}

func TestApplyRebuildFailureDoesNotConfirmDeletions(t *testing.T) {
	files := pageAreaFiles("")
	files["orphan.bin"] = "unused"
	maxSize := 0
	for _, data := range files {
		if len(data) > maxSize {
			maxSize = len(data)
		}
	}
	var out bytes.Buffer
	result, err := Apply(buildOFD(t, files), &out, Options{DocType: "OFD-A", DropUnreferenced: true, Limits: replace.Limits{MaxEntryBytes: int64(maxSize)}})
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("重写后 XML 超过条目上限应在重建阶段失败: %v", err)
	}
	if out.Len() != 0 || result.UnreferencedDropped != 0 {
		t.Fatalf("重建失败时不能写出或报告删除成功: %+v", result)
	}
}
