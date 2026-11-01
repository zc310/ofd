package merge

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/creator"
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

func TestZIPMergePreflightsEveryInputBeforeOutput(t *testing.T) {
	denied := false
	allowed := permissionDocument(t, nil)
	for _, value := range []*creator.Permissions{{Export: &denied}, {ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}}} {
		input := [][]byte{allowed, permissionDocument(t, value)}
		var output bytes.Buffer
		callbacks := 0
		err := Bytes(input, &output, Options{OnWarning: func(string) { callbacks++ }, OnSignature: func(SignatureEvent) { callbacks++ }})
		if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 || callbacks != 0 {
			t.Fatalf("预检前产生副作用：%v bytes=%d callbacks=%d", err, output.Len(), callbacks)
		}
		if err := Bytes(input, &output, Options{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestZIPMergeExportIsIndependentOfEdit(t *testing.T) {
	denied := false
	var output bytes.Buffer
	if err := Bytes([][]byte{permissionDocument(t, &creator.Permissions{Edit: &denied})}, &output, Options{}); err != nil {
		t.Fatalf("合并错误地要求Edit：%v", err)
	}
}

func TestZIPMergeUsesSameOpenedInputForPreflightAndCopy(t *testing.T) {
	data := permissionDocument(t, nil)
	calls := 0
	src := source{name: "same-input", open: func() (*container.Package, error) {
		calls++
		return container.OpenBytes(data)
	}}
	var output bytes.Buffer
	if err := mergeSources([]source{src}, &output, Options{}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("预检后重新打开输入：%d", calls)
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

func TestZIPMergeRechecksExpiryAfterWriter(t *testing.T) {
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := end
	input := permissionDocument(t, &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}})
	output := &expiryWriter{expire: func() { now = end.Add(time.Second) }}
	err := Bytes([][]byte{input}, output, Options{PermissionPolicy: permission.Policy{Now: func() time.Time { return now }}})
	if !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("最后写出期间到期仍报告成功：%v", err)
	}
}

func TestPageMergeChecksAllRetainedResourceScopesBeforeCallbacks(t *testing.T) {
	denied := false
	inputs := []Source{{Data: permissionDocument(t, nil)}, {Data: permissionDocument(t, &creator.Permissions{Export: &denied})}}
	for _, workers := range []int{1, 2} {
		var output bytes.Buffer
		calls := 0
		opts := PageOptions{Pages: []int{1}, Concurrency: workers, OnSignature: func(SignatureEvent) { calls++ }}
		err := Pages(inputs, &output, opts)
		if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 || calls != 0 {
			t.Fatalf("选页携带受限来源资源但未拒绝：%v bytes=%d calls=%d", err, output.Len(), calls)
		}
		opts.PermissionPolicy.Mode = permission.IgnoreDeclarations
		if err := Pages(inputs, &output, opts); err != nil {
			t.Fatalf("授权策略未传入清单适配层：%v", err)
		}
	}
}

func TestPageMergeExportIndependentOfEditAndExpiryDuringOutput(t *testing.T) {
	denied := false
	var output bytes.Buffer
	if err := Pages([]Source{{Data: permissionDocument(t, &creator.Permissions{Edit: &denied})}}, &output, PageOptions{}); err != nil {
		t.Fatalf("拼页错误地要求Edit：%v", err)
	}
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := end
	input := permissionDocument(t, &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}})
	w := &expiryWriter{expire: func() { now = end.Add(time.Second) }}
	err := Pages([]Source{{Data: input}}, w, PageOptions{PermissionPolicy: permission.Policy{Now: func() time.Time { return now }}})
	if !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("拼页写出期间到期仍成功：%v", err)
	}
}
