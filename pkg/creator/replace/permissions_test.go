package replace

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/permission"
)

func TestFilesChecksOriginalEditDeclarationBeforeAnyOutput(t *testing.T) {
	denied := false
	for _, value := range []*creator.Permissions{
		{Edit: &denied},
		{ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}},
	} {
		data, err := creator.Marshal(creator.Document{ID: "restricted", Pages: []creator.Page{{}}, Permissions: value})
		if err != nil {
			t.Fatal(err)
		}
		// 直接替换权限声明，不能用替换后的允许值给本次编辑授权。
		operations := []Operation{{Kind: OpSet, Name: "Doc_0/Document.xml", Data: []byte(`<Document xmlns="http://www.ofdspec.org/2016"/>`)}}
		var output bytes.Buffer
		err = Files(data, operations, &output, Options{})
		if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
			t.Fatalf("未在输出前拒绝：%v bytes=%d", err, output.Len())
		}
		if err := Files(data, operations, &output, Options{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFilesRechecksValidityBeforePublishingBufferedOutput(t *testing.T) {
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	data, err := creator.Marshal(creator.Document{ID: "expires", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	options := Options{PermissionPolicy: permission.Policy{Now: func() time.Time {
		calls++
		if calls == 1 {
			return end
		}
		return end.Add(time.Second)
	}}}
	var output bytes.Buffer
	err = Files(data, []Operation{{Kind: OpAdd, Name: "note.txt", Data: []byte("note")}}, &output, options)
	if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("过期后发布了缓冲输出：%v bytes=%d", err, output.Len())
	}
}
