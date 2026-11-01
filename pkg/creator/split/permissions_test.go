package split

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/merge"
	"github.com/zc310/ofd/pkg/permission"
)

func permissionDocument(t *testing.T, value *creator.Permissions) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "permission", Pages: []creator.Page{{}}, Permissions: value})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSplitWriteChecksOnlySelectedBodyAndOriginalExport(t *testing.T) {
	denied := false
	allowed := permissionDocument(t, &creator.Permissions{Edit: &denied})
	restricted := permissionDocument(t, &creator.Permissions{Export: &denied})
	var input bytes.Buffer
	if err := merge.Bytes([][]byte{allowed, restricted}, &input, merge.Options{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}}); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, input.Bytes(), Options{})
	var output bytes.Buffer
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, &output); err != nil {
		t.Fatalf("未选文档体或Edit阻止导出：%v", err)
	}
	for _, part := range []Part{{Document: 2, Pages: []int{1}}, {Document: 2, WholeDocument: true}} {
		output.Reset()
		_, err := s.Write(t.Context(), part, &output)
		detail, ok := errors.AsType[*permission.DeniedError](err)
		if !ok || detail.Scope != 1 || detail.Action != permission.Export || output.Len() != 0 {
			t.Fatalf("拆分权限拒绝错误：%v bytes=%d", err, output.Len())
		}
	}
	s = openTest(t, input.Bytes(), Options{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}})
	if _, err := s.Write(t.Context(), Part{Document: 2, Pages: []int{1}}, &output); err != nil {
		t.Fatal(err)
	}
}

type expiryWriter struct {
	bytes.Buffer

	expire func()
}

func (w *expiryWriter) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	w.expire()
	return n, err
}

func TestSplitRechecksValidityBetweenPartsAndDuringLastWrite(t *testing.T) {
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := end
	input := permissionDocument(t, &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}})
	s := openTest(t, input, Options{PermissionPolicy: permission.Policy{Now: func() time.Time { return now }}})
	part := Part{Document: 1, Pages: []int{1}}
	var output bytes.Buffer
	if _, err := s.Write(t.Context(), part, &output); err != nil {
		t.Fatal(err)
	}
	now = end.Add(time.Second)
	output.Reset()
	if _, err := s.Write(t.Context(), part, &output); !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("会话缓存授权跨过有效期：%v", err)
	}
	now = end
	w := &expiryWriter{expire: func() { now = end.Add(time.Second) }}
	if _, err := s.Write(t.Context(), part, w); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("最后写出期间到期仍成功：%v", err)
	}
	if s.parts != 1 {
		t.Fatalf("失败输出计入成功分卷数：%d", s.parts)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Write(ctx, part, &output); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
