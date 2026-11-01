package permission

import (
	"archive/zip"
	"bytes"
	"errors"
	"testing"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/permission"
)

func TestReadPackageRetainsRawSpecDeclarationsAndOriginalScopes(t *testing.T) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, value := range map[string]string{
		"OFD.xml":      `<OFD><DocBody><DocRoot>Absent.xml</DocRoot></DocBody><DocBody><DocRoot>Declared.xml</DocRoot></DocBody></OFD>`,
		"Absent.xml":   `<Document/>`,
		"Declared.xml": `<Document><Permissions><Edit>false</Edit><Print/><ValidPeriod/></Permissions></Document>`,
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	pkg, err := container.OpenBytes(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	values, err := ReadPackage(pkg, []int{1, 0}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].Scope != 1 || values[1].Scope != 0 || values[1].Value != nil {
		t.Fatalf("原始范围或缺省声明丢失：%+v", values)
	}
	d := values[0].Value
	if d == nil || d.Edit == nil || *d.Edit || d.Export != nil || d.Print == nil || !d.Print.Printable || d.Print.Copies != -1 || d.ValidPeriod == nil {
		t.Fatalf("包预检未保留原始标准声明：%+v", d)
	}
	err = CheckPackage(pkg, permission.Policy{}, permission.Access, nil)
	denied, ok := errors.AsType[*permission.DeniedError](err)
	if !ok || denied.Scope != 1 || denied.Reason != permission.InvalidPeriod {
		t.Fatalf("空期限未按原始范围拒绝：%v", err)
	}
	if err := CheckPackage(pkg, permission.Policy{}, permission.Export, []int{0}); err != nil {
		t.Fatal(err)
	}
}
