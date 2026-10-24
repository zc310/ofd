package crypt

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func pageScopeFiles() map[string][]byte {
	xml := func(root, content string) []byte {
		return []byte(`<` + root + ` xmlns="` + Namespace + `">` + content + `</` + root + `>`)
	}
	return map[string][]byte{
		"OFD.xml":               xml("OFD", `<DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody>`),
		"Doc_0/Document.xml":    xml("Document", `<CommonData><PublicRes>Res.xml</PublicRes><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="10" BaseLoc="Template.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page1.xml"/><Page ID="2" BaseLoc="Page2.xml"/></Pages><Annotations>Annotations.xml</Annotations>`),
		"Doc_0/Page1.xml":       xml("Page", `<Template TemplateID="10"/><PageRes>PageRes.xml</PageRes><Content/>`),
		"Doc_0/Page2.xml":       xml("Page", `<Content/>`),
		"Doc_0/Template.xml":    xml("Page", `<PageRes>Res.xml</PageRes><Content/>`),
		"Doc_0/Res.xml":         []byte(`<Res xmlns="` + Namespace + `" BaseLoc="Res"><Fonts><Font ID="20"><FontFile>font.dat</FontFile></Font></Fonts><MultiMedias><MultiMedia ID="21"><MediaFile>image.png</MediaFile></MultiMedia></MultiMedias><ColorSpaces><ColorSpace ID="22" Profile="profile.icc"/></ColorSpaces></Res>`),
		"Doc_0/PageRes.xml":     []byte(`<Res xmlns="` + Namespace + `" BaseLoc="Res"><MultiMedias><MultiMedia ID="23"><MediaFile>extra.png</MediaFile></MultiMedia></MultiMedias></Res>`),
		"Doc_0/Annotations.xml": xml("Annotations", `<Page PageID="1"><FileLoc>Annot1.xml</FileLoc></Page><Page PageID="2"><FileLoc>Annot2.xml</FileLoc></Page>`),
		"Doc_0/Annot1.xml":      xml("PageAnnot", `<Annot ID="a1"><Appearance/></Annot>`),
		"Doc_0/Annot2.xml":      xml("PageAnnot", `<Annot ID="a2"><Appearance/></Annot>`),
		"Doc_0/Res/font.dat":    []byte("font resource bytes"),
		"Doc_0/Res/image.png":   []byte("image resource bytes"),
		"Doc_0/Res/profile.icc": []byte("color profile bytes"),
		"Doc_0/Res/extra.png":   []byte("page image bytes"),
	}
}

func TestPageSelectionIncludesTemplatesAnnotationsAndSharedResources(t *testing.T) {
	original := pageScopeFiles()
	options := testPasswordOptions()
	options.Pages = &PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"1"}}
	options.PlaintextMap = true
	encrypted, report := encryptFiles(t, original, options)
	want := []string{"Doc_0/Page1.xml", "Doc_0/Template.xml", "Doc_0/Annot1.xml", "Doc_0/Res.xml", "Doc_0/PageRes.xml", "Doc_0/Res/font.dat", "Doc_0/Res/image.png", "Doc_0/Res/profile.icc", "Doc_0/Res/extra.png"}
	slices.Sort(want)
	if !report.SharedResources || !slices.Equal(report.SelectedEntries, want) {
		t.Fatalf("页面范围未包含全部依赖，实际 %+v，期望 %v", report, want)
	}
	for _, name := range []string{"OFD.xml", "Doc_0/Document.xml", "Doc_0/Page2.xml", "Doc_0/Annot2.xml"} {
		if !bytes.Equal(encrypted[name], original[name]) {
			t.Errorf("未选中的条目 %s 应原样保留", name)
		}
	}
	output, _ := decryptedFiles(t, testPackage(t, encrypted), DecryptOptions{Password: []byte("secret")})
	if !reflect.DeepEqual(output, original) {
		t.Fatal("按页加密后应恢复所有原始条目及字节")
	}
}

func TestPageSelectionRejectsMissingReferencesAndTemplateCycles(t *testing.T) {
	for _, test := range []struct{ name, document, pageID, overridePath, overrideXML string }{
		{name: "unknown document", document: "Other/Document.xml"},
		{name: "unknown page", pageID: "999"},
		{name: "unknown template", overridePath: "Doc_0/Page1.xml", overrideXML: `<Page xmlns="` + Namespace + `"><Template TemplateID="999"/></Page>`},
		{name: "template cycle", overridePath: "Doc_0/Template.xml", overrideXML: `<Page xmlns="` + Namespace + `"><Template TemplateID="10"/></Page>`},
		{name: "resource outside package", overridePath: "Doc_0/Res.xml", overrideXML: `<Res xmlns="` + Namespace + `" BaseLoc="../../outside"><Fonts><Font><FontFile>font.dat</FontFile></Font></Fonts></Res>`},
		{name: "missing resource file", overridePath: "Doc_0/Res.xml", overrideXML: `<Res xmlns="` + Namespace + `"><Fonts><Font><FontFile>missing.dat</FontFile></Font></Fonts></Res>`},
		{name: "foreign namespace", overridePath: "Doc_0/Res.xml", overrideXML: `<Res xmlns="urn:foreign"/>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := pageScopeFiles()
			options := testPasswordOptions()
			selection := &PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"1"}}
			if test.document != "" {
				selection.Document = test.document
			}
			if test.pageID != "" {
				selection.IDs = []string{test.pageID}
			}
			options.Pages = selection
			if test.overridePath != "" {
				files[test.overridePath] = []byte(test.overrideXML)
			}
			called := false
			report, err := EncryptPackage(t.Context(), testPackage(t, files), options, func(string, []byte) error { called = true; return nil })
			if !errors.Is(err, ErrInvalidMetadata) || called || report != nil {
				t.Fatalf("范围预检失败应拒绝输出并保留错误类别: report=%+v, err=%v, emitted=%t", report, err, called)
			}
		})
	}
}

func TestAppendEncryptionUsesResolverForEachLayerAndPreservesOldMetadata(t *testing.T) {
	original := testPlainFiles()
	first, firstReport := encryptFiles(t, original, testPasswordOptions())
	options := EncryptOptions{Recipients: []EncryptRecipient{{UserName: "reader", Password: []byte("second password")}}, Append: &DecryptOptions{Password: []byte("secret")}}
	second, secondReport := encryptFiles(t, first, options)
	if secondReport.OperationID == firstReport.OperationID || secondReport.Relative != firstReport.OperationID {
		t.Fatalf("追加操作应指向上一层并使用新 ID: first=%+v, second=%+v", firstReport, secondReport)
	}
	oldRoot := string(first[EntryPoint])
	start, end := strings.Index(oldRoot, "<ofd:EncryptInfo "), strings.Index(oldRoot, "</ofd:EncryptInfo>")
	if start < 0 || end < 0 {
		t.Fatal("首层加密记录缺失")
	}
	if !bytes.Contains(second[EntryPoint], []byte(oldRoot[start:end+len("</ofd:EncryptInfo>")])) {
		t.Fatal("追加不应重新序列化或改变旧层记录")
	}
	requested := map[string]bool{}
	output, report := decryptedFiles(t, testPackage(t, second), DecryptOptions{Resolve: func(ctx context.Context, request CredentialRequest) (Credentials, error) {
		if err := ctx.Err(); err != nil {
			return Credentials{}, err
		}
		requested[request.OperationID] = true
		if request.DecryptSeedLoc == "" || request.Scheme != PasswordScheme || len(request.Recipients) != 1 {
			t.Errorf("凭据请求应包含层标识与接收者: %+v", request)
		}
		password := []byte("secret")
		if request.OperationID == secondReport.OperationID {
			password = []byte("second password")
		}
		request.Recipients[0].UserName = "changed by resolver"
		return Credentials{Password: password, UserName: "reader"}, nil
	}})
	if !reflect.DeepEqual(output, original) || len(report.Operations) != 2 || !requested[firstReport.OperationID] || !requested[secondReport.OperationID] {
		t.Fatalf("应逐层恢复全部原始字节: requested=%v, report=%+v", requested, report)
	}
	for _, operation := range report.Operations {
		if operation.Recipient.UserName != "reader" {
			t.Fatalf("凭据请求快照修改不应影响报告: %+v", operation)
		}
	}
}

func TestCredentialAttemptBudgetAndDamagedCiphertext(t *testing.T) {
	options := testPasswordOptions()
	options.PlaintextMap = true
	options.Recipients = append([]EncryptRecipient{{UserName: "other", Password: []byte("other password")}}, options.Recipients...)
	files, _ := encryptFiles(t, testPlainFiles(), options)
	pkg := testPackage(t, files)
	decryptOptions := DecryptOptions{Password: []byte("secret"), Limits: Limits{MaxCredentialAttempts: 1}}
	if _, err := OpenView(t.Context(), pkg, OpenOptions{DecryptOptions: decryptOptions}, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("凭据尝试预算应限制逐个接收者匹配: %v", err)
	}
	decryptOptions.UserName = "reader"
	output, _ := decryptedFiles(t, pkg, decryptOptions)
	if !reflect.DeepEqual(output, testPlainFiles()) {
		t.Fatal("指定接收者后应能在一次尝试内解密")
	}
	info, err := InspectPackage(t.Context(), pkg, InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cipherPath := info.Operations[0].Entries[0].EPath
	files[cipherPath] = bytes.Clone(files[cipherPath][:len(files[cipherPath])-1])
	called := false
	report, err := DecryptPackage(t.Context(), testPackage(t, files), DecryptOptions{Password: []byte("secret"), UserName: "reader"}, func(string, []byte) error { called = true; return nil })
	if !errors.Is(err, ErrDecryptFailed) || called || report != nil {
		t.Fatalf("截断密文应在导出前被拒绝: report=%+v, err=%v, emitted=%t", report, err, called)
	}
}
