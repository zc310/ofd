package webreader

import (
	"bytes"
	"encoding/xml"
	"errors"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

// 两个文档体和独立历史入口分别声明权限，保护选页、版本与资源范围。
func permissionsPackage(t *testing.T) []byte {
	t.Helper()
	document := func(permissions string) []byte {
		return []byte(`<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>9</MaxUnitID><PageArea><PhysicalBox>0 0 40 30</PhysicalBox></PageArea><DocumentRes>/Res.xml</DocumentRes></CommonData><Pages><Page ID="1" BaseLoc="/Page.xml"/></Pages>` + permissions + `<Attachments>/Attachments.xml</Attachments></Document>`)
	}
	entries := map[string][]byte{
		"OFD.xml":         []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>restricted</DocID></DocInfo><DocRoot>Restricted.xml</DocRoot><Versions><Version ID="old" Index="1" BaseLoc="Version.xml"/></Versions></DocBody><DocBody><DocInfo><DocID>allowed</DocID></DocInfo><DocRoot>Allowed.xml</DocRoot></DocBody></OFD>`),
		"Restricted.xml":  document(`<Permissions><Export>false</Export><Print Printable="false"/></Permissions>`),
		"Allowed.xml":     document(`<Permissions><Print Printable="true"/></Permissions>`),
		"History.xml":     document(""),
		"Version.xml":     []byte(`<DocVersion xmlns="http://www.ofdspec.org/2016" ID="old"><FileList><File ID="d">History.xml</File></FileList><DocRoot>History.xml</DocRoot></DocVersion>`),
		"Res.xml":         []byte(`<Res xmlns="http://www.ofdspec.org/2016"><Fonts><Font ID="4" FontName="Go Regular"><FontFile>/font.ttf</FontFile></Font></Fonts></Res>`),
		"font.ttf":        goregular.TTF,
		"Page.xml":        []byte(`<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="2"><TextObject ID="3" Font="4" Size="3" Boundary="1 1 35 10"><TextCode X="0" Y="4">Secret report content</TextCode></TextObject></Layer></Content></Page>`),
		"Attachments.xml": []byte(`<Attachments xmlns="http://www.ofdspec.org/2016"><Attachment ID="note" Name="note.txt"><FileLoc>/note.txt</FileLoc></Attachment></Attachments>`),
		"note.txt":        []byte("attachment"),
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestPermissionsDefaultsAndPrintCopies(t *testing.T) {
	for _, test := range []struct {
		xml       string
		printable bool
		copies    int
	}{
		{`<Permissions/>`, true, -1},
		{`<Permissions><Print Printable="true"/></Permissions>`, true, -1},
		{`<Permissions><Print Printable="true" Copies="0"/></Permissions>`, false, 0},
		{`<Permissions><Print Printable="true" Copies="-5"/></Permissions>`, true, -5},
		{`<Permissions><Print Printable="true" Copies="2"/></Permissions>`, true, 2},
		{`<Permissions><Print Printable="false" Copies="2"/></Permissions>`, false, 2},
	} {
		var value models.CT_Permission
		if err := xml.Unmarshal([]byte(test.xml), &value); err != nil {
			t.Fatal(err)
		}
		permission := documentPermissions(0, &value)
		if !permission.Export || !permission.Edit || !permission.Annot || !permission.PrintScreen || !permission.Signature || !permission.Watermark || permission.Printable != test.printable || permission.PrintCopies != test.copies {
			t.Fatalf("%s: %+v", test.xml, permission)
		}
	}
}

func TestPermissionsEnforceSelectedDocumentBeforeOutput(t *testing.T) {
	r, err := Open(permissionsPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	permissions, err := r.Permissions()
	if err != nil || len(permissions) != 2 || permissions[0].Export || permissions[0].Printable || !permissions[1].Export || !permissions[1].Printable || permissions[1].PrintCopies != -1 {
		t.Fatalf("权限: %+v err=%v", permissions, err)
	}
	permissions[0].Pages[0] = 999
	permissions[0].Export = true
	for _, action := range []PermissionAction{PermissionCopy, PermissionExport, PermissionPrint} {
		if err := r.CheckPermission(action, []int{1}); err != nil {
			t.Fatal(err)
		}
		if err := r.CheckPermission(action, []int{1, 0}); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("%s: %v", action, err)
		}
	}
	for _, indices := range [][]int{nil, {-1}, {2}} {
		if err := r.CheckPermission(PermissionExport, indices); err == nil {
			t.Fatalf("接受非法选页: %v", indices)
		}
	}
	if err := r.CheckPermission("unknown", []int{0}); err == nil {
		t.Fatal("接受未知操作")
	}
	if _, err := r.Text(0); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("受限文字: %v", err)
	}
	if text, err := r.Text(1); err != nil || len(text) == 0 {
		t.Fatalf("允许文字: %v %v", text, err)
	}
	if _, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 12}); err != nil {
		t.Fatalf("普通预览被阻止: %v", err)
	}
	for _, purpose := range []PermissionAction{PermissionExport, PermissionPrint} {
		if _, err := r.RenderPage(t.Context(), 0, RenderOptions{Purpose: purpose, DPI: 12}); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("用途 %s: %v", purpose, err)
		}
		if _, err := r.RenderPages(t.Context(), []int{1, 0}, RenderOptions{Purpose: purpose}); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("批量用途 %s: %v", purpose, err)
		}
	}
	var output bytes.Buffer
	if err := r.RenderPDFTo(t.Context(), &output, []int{1, 0}, RenderOptions{}); !errors.Is(err, ErrPermissionDenied) || output.Len() != 0 {
		t.Fatalf("混合 PDF: err=%v bytes=%d", err, output.Len())
	}
	if err := r.RenderPDFTo(t.Context(), &output, []int{1}, RenderOptions{}); err != nil || !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
		t.Fatalf("允许 PDF: %v", err)
	}
	if _, err := r.AttachmentData(0, "note", 0); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("附件权限: %v", err)
	}
	if data, err := r.AttachmentData(1, "note", 0); err != nil || string(data) != "attachment" {
		t.Fatalf("允许附件: %q %v", data, err)
	}
	report, err := r.Search(t.Context(), "Secret", SearchOptions{})
	if err != nil || len(report.Results) != 2 || report.Results[0].Text != "Secret" || report.Results[1].Text != "Secret report content" {
		t.Fatalf("搜索泄露受限上下文: %+v %v", report, err)
	}
}

func TestPermissionsFollowVersionAndRestoreAfterFailure(t *testing.T) {
	r, err := Open(permissionsPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.SelectVersion(0, "old"); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckPermission(PermissionExport, []int{0, 1}); err != nil {
		t.Fatalf("历史版本权限未更新: %v", err)
	}
	if err := r.SelectVersion(0, "missing"); err == nil {
		t.Fatal("缺失版本应失败")
	}
	if err := r.CheckPermission(PermissionExport, []int{0}); err != nil {
		t.Fatalf("失败损坏权限: %v", err)
	}
	if err := r.SelectVersion(0, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckPermission(PermissionExport, []int{0}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("恢复主文档未恢复权限: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Permissions(); err == nil {
		t.Fatal("关闭后仍返回权限")
	}
}
