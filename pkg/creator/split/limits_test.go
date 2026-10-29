package split

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

type countedReader struct {
	reader *bytes.Reader
	reads  int
}

func (r *countedReader) ReadAt(data []byte, offset int64) (int, error) {
	r.reads++
	return r.reader.ReadAt(data, offset)
}

func TestSplitReaderLimitsAreCheckedBeforeReading(t *testing.T) {
	data := splitFixture(t)
	reader := &countedReader{reader: bytes.NewReader(data)}
	if _, err := Open(t.Context(), Source{Reader: reader, Size: int64(len(data))}, Options{Limits: Limits{MaxInputBytes: 1}}); err == nil || reader.reads != 0 {
		t.Fatalf("预算检查前读取了输入: %v, %d", err, reader.reads)
	}
	s, err := Open(t.Context(), Source{Reader: reader, Size: int64(len(data))}, Options{Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	other := openTest(t, data, Options{Deterministic: true})
	a, _ := writeTest(t, s, Part{Document: 1, Pages: []int{2}})
	b, _ := writeTest(t, other, Part{Document: 1, Pages: []int{2}})
	if !bytes.Equal(a, b) {
		t.Fatal("Reader 与字节输入结果不同")
	}
	if _, err := reader.reader.ReadAt(make([]byte, 1), 0); err != nil {
		t.Fatal("调用方 Reader 被关闭")
	}
}

func TestSplitThousandPagesChecksEveryPartAndBoundaryOrder(t *testing.T) {
	pages := make([]creator.Page, 1000)
	for i := range pages {
		pages[i].Items = []creator.Item{creator.Path{Width: 1, Height: 1, Name: "page-" + strconv.Itoa(i+1), Data: "M 0 0 L 1 1", Stroke: true}}
	}
	data, err := creator.Marshal(creator.Document{ID: "thousand-pages", PageSize: creator.A4, Pages: pages})
	if err != nil {
		t.Fatal(err)
	}
	s := openTest(t, data, Options{Deterministic: true})
	parts, err := s.Plan(Selection{Every: 100})
	if err != nil || len(parts) != 10 {
		t.Fatalf("千页计划错误: %v", err)
	}
	for i, part := range parts {
		result, report := writeTest(t, s, part)
		if len(report.Pages) != 100 || report.Bytes == 0 {
			t.Fatal("分卷页数/大小错误")
		}
		parsed, err := parser.NewOFD(result)
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed.Documents[0].Pages) != 100 {
			t.Fatal("实际页数错误")
		}
		_ = parsed.Close()
		files := readFiles(t, result)
		first := "Doc_0/Pages/Page_" + strconv.Itoa(i*100) + "/Content.xml"
		last := "Doc_0/Pages/Page_" + strconv.Itoa(i*100+99) + "/Content.xml"
		if !bytes.Contains(files[first], []byte(`Name="page-`+strconv.Itoa(i*100+1)+`"`)) || !bytes.Contains(files[last], []byte(`Name="page-`+strconv.Itoa(i*100+100)+`"`)) {
			t.Fatal("首尾页顺序错误")
		}
	}
}

func TestSplitCumulativeOutputAndPartBudgets(t *testing.T) {
	data := splitFixture(t)
	baseline := openTest(t, data, Options{Deterministic: true})
	first, _ := writeTest(t, baseline, Part{Document: 1, Pages: []int{1}})
	s := openTest(t, data, Options{Deterministic: true, Limits: Limits{MaxOutputBytes: int64(len(first))}})
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, io.Discard); err == nil || !strings.Contains(err.Error(), "累计输出") {
		t.Fatalf("累计输出预算未生效: %v", err)
	}
	s = openTest(t, data, Options{Limits: Limits{MaxParts: 1}})
	if _, err := s.Plan(Selection{Every: 1}); err == nil {
		t.Fatal("计划未检查分卷上限")
	}
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, io.Discard); !errors.Is(err, ErrInvalid) {
		t.Fatalf("累计分卷预算未生效: %v", err)
	}
}

type cancelAfterWrite struct {
	cancel context.CancelFunc
}

func (w cancelAfterWrite) Write(data []byte) (int, error) {
	w.cancel()
	return len(data), nil
}

func TestSplitCancellationDuringFinalWriteIsNotSuccess(t *testing.T) {
	s := openTest(t, splitFixture(t), Options{Deterministic: true, Limits: Limits{MaxParts: 1}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	report, err := s.Write(ctx, Part{Document: 1, Pages: []int{1}}, cancelAfterWrite{cancel: cancel})
	if !errors.Is(err, context.Canceled) || report.Bytes != 0 {
		t.Fatalf("最终输出期间取消仍被报告为成功: %+v %v", report, err)
	}
	if s.written == 0 {
		t.Fatal("取消前实际写入的字节未计入累计预算")
	}
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, io.Discard); err != nil {
		t.Fatalf("取消不应消耗成功分卷数: %v", err)
	}
}
