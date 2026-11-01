package preserve

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/replace"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/sign"
)

func TestArchivalPreserveDoesNotRequireEditExportOrValidPeriod(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "archival-evidence", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{
		Edit: &denied, Export: &denied, Signature: &denied, ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(data)
	plan, err := Plan(data, Options{DocType: "OFD-A"})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	result, err := Apply(data, &output, Options{DocType: "OFD-A", Validate: true})
	if err != nil {
		t.Fatalf("归档误用普通编辑权限：%v", err)
	}
	if len(result.Changes) == 0 || len(result.Changes) != len(plan.Changes) {
		t.Fatalf("归档计划与执行不一致：%+v %+v", plan, result)
	}
	if !bytes.Equal(data, original) {
		t.Fatal("归档转换修改了原始证据")
	}
	entries := readEntries(t, output.Bytes())
	if strings.Contains(entries["Doc_0/Document.xml"], "Permissions") {
		t.Fatal("归档副本未按6.2.2 a)去除声明")
	}
	// 归档用途不能反过来给普通条目编辑授权。
	output.Reset()
	err = replace.Files(data, []replace.Operation{{Kind: replace.OpAdd, Name: "note.txt", Data: []byte("note")}}, &output, replace.Options{})
	if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("普通编辑门控被归档用途关闭：%v", err)
	}
	if _, err := Apply(data, &output, Options{DocType: "OFD-A", Limits: replace.Limits{MaxEntryBytes: 1}}); err == nil || output.Len() != 0 {
		t.Fatalf("归档权限策略关闭了资源预算：%v", err)
	}
}

func TestArchivalPreserveRetainsSignatureEvidenceAndWarnsAboutChanges(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "signed-evidence", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{Edit: &denied, Export: &denied}})
	if err != nil {
		t.Fatal(err)
	}
	var signed bytes.Buffer
	if err := sign.Sign(t.Context(), data, &signed, sign.Options{Sign: func(context.Context, []byte) ([]byte, error) { return []byte("signature evidence"), nil }}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	var warnings []string
	if _, err := Apply(signed.Bytes(), &output, Options{DocType: "OFD-A", OnWarning: func(message string) { warnings = append(warnings, message) }}); err != nil {
		t.Fatal(err)
	}
	pkg, err := container.OpenBytes(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	value, err := pkg.Read("Doc_0/Signatures/Data/sign-1.dat")
	if err != nil || string(value) != "signature evidence" {
		t.Fatalf("归档移除了签名证据：%q %v", value, err)
	}
	if len(warnings) == 0 {
		t.Fatal("归档转换未提示签名摘要可能失效")
	}
}
