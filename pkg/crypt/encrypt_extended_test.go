package crypt_test

import (
	"bytes"
	"context"
	"crypto/cipher"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/emmansun/gmsm/sm4"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/sign"
)

func passwordRecipient(password string) []crypt.EncryptRecipient {
	return []crypt.EncryptRecipient{{UserName: "user", Password: []byte(password)}}
}

func TestEncryptIndependentSeedsUseDistinctKeysAndIVs(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		for _, plaintextMap := range []bool{false, true} {
			originals := minimalOriginals()
			originals["same-a.bin"], originals["same-b.bin"] = strings.Repeat("identical", 8), strings.Repeat("identical", 8)
			opt := crypt.EncryptOptions{Recipients: passwordRecipient("secret"), IndependentKeys: true, PlaintextMap: plaintextMap}
			decrypt := crypt.DecryptOptions{Password: []byte("secret")}
			if certificate {
				der, err := base64.StdEncoding.DecodeString(readVectors(t).SM2.Certificate)
				if err != nil {
					t.Fatal(err)
				}
				opt.Recipients = []crypt.EncryptRecipient{{UserName: "cert", Certificate: der}}
				decrypt = crypt.DecryptOptions{PrivateKey: privateKey(t)}
			}
			var encrypted, plain bytes.Buffer
			report, err := crypt.Encrypt(t.Context(), archive(t, originals), &encrypted, opt)
			if err != nil || !report.IndependentKeys || report.OutputEntries != len(originals)*2+3 {
				t.Fatalf("certificate=%v plaintextMap=%v report=%+v err=%v", certificate, plaintextMap, report, err)
			}
			view, err := crypt.Open(t.Context(), encrypted.Bytes(), crypt.OpenOptions{DecryptOptions: decrypt})
			if err != nil {
				t.Fatal(err)
			}
			op := view.EncryptionInfo().Operations[0]
			view.Close()
			physical := unpack(t, encrypted.Bytes())
			seenKeys, seenIVs := map[string]bool{}, map[string]bool{}
			paths := []string{op.DecryptSeedLoc}
			for _, mapping := range op.Entries {
				if mapping.DecryptSeedLoc == "" || mapping.Seed == nil {
					t.Fatal("条目未声明独立 seed")
				}
				paths = append(paths, mapping.DecryptSeedLoc)
			}
			for _, path := range paths {
				var seed struct {
					Users []struct {
						WK string `xml:"EncryptedWK"`
						IV string `xml:"IVValue"`
					} `xml:"UserInfo"`
				}
				if err := xml.Unmarshal([]byte(physical[path]), &seed); err != nil || len(seed.Users) != 1 {
					t.Fatalf("seed=%s err=%v", path, err)
				}
				iv, err := base64.StdEncoding.DecodeString(seed.Users[0].IV)
				if err != nil || len(iv) != 16 || bytes.Equal(iv, make([]byte, 16)) || seenIVs[string(iv)] {
					t.Fatal("独立 IV 缺失、重复或无效")
				}
				wk, err := base64.StdEncoding.DecodeString(seed.Users[0].WK)
				if err != nil {
					t.Fatal(err)
				}
				var key []byte
				if certificate {
					key, err = privateKey(t).Decrypt(nil, wk, nil)
				} else {
					block, blockErr := sm4.NewCipher(hexBytes(t, readVectors(t).Passwords[1].KEK))
					if blockErr != nil {
						t.Fatal(blockErr)
					}
					cipher.NewCBCDecrypter(block, iv).CryptBlocks(wk, wk)
					key = wk[:16]
				}
				if err != nil || len(key) != 16 || seenKeys[string(key)] {
					t.Fatalf("FEK 重复或无效: %v", err)
				}
				seenKeys[string(key)], seenIVs[string(iv)] = true, true
			}
			if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &plain, decrypt); err != nil {
				t.Fatal(err)
			}
			checkOriginals(t, plain.Bytes(), originals)
		}
	}
}

func TestEncryptAppendPreservesOldRecordsAndRestoresThreeLayers(t *testing.T) {
	originals := minimalOriginals()
	var inner bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), archive(t, originals), &inner, crypt.EncryptOptions{Recipients: passwordRecipient("first"), IndependentKeys: true}); err != nil {
		t.Fatal(err)
	}
	data := inner.Bytes()
	passwords := map[string]string{"enc1": "first", "enc2": "second", "enc3": "third"}
	resolver := func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
		return crypt.Credentials{Password: []byte(passwords[r.OperationID])}, nil
	}
	for _, password := range []string{"second", "third"} {
		previous := unpack(t, data)["Encryptions.xml"]
		// 旧记录可以带有未建模的扩展，追加时必须逐字节保留。
		previous = strings.Replace(previous, "</ofd:Encryptions>", "<!-- retained extension -->\r\n</ofd:Encryptions>", 1)
		files := unpack(t, data)
		files["Encryptions.xml"] = previous
		data = archive(t, files)
		var next bytes.Buffer
		report, err := crypt.Encrypt(t.Context(), data, &next, crypt.EncryptOptions{Recipients: passwordRecipient(password), IndependentKeys: password == "second", Append: &crypt.DecryptOptions{Resolve: resolver}})
		if err != nil || report.Relative == "" || report.OperationID == report.Relative {
			t.Fatalf("report=%+v err=%v", report, err)
		}
		root := unpack(t, next.Bytes())["Encryptions.xml"]
		prefix := strings.TrimSuffix(previous, "</ofd:Encryptions>")
		if !strings.HasPrefix(root, prefix) || !strings.Contains(root, `Relative="`+report.Relative+`"`) {
			t.Fatal("追加改写了旧操作记录")
		}
		data = next.Bytes()
	}
	var plain bytes.Buffer
	report, err := crypt.Decrypt(t.Context(), data, &plain, crypt.DecryptOptions{Resolve: resolver})
	if err != nil || len(report.Operations) != 3 || report.Operations[0].ID != "enc3" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	checkOriginals(t, plain.Bytes(), originals)
}

func TestEncryptAppendRejectsMissingCredentialsProtectionAndBudgets(t *testing.T) {
	var inner bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), archive(t, minimalOriginals()), &inner, crypt.EncryptOptions{Recipients: passwordRecipient("secret")}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		old    crypt.DecryptOptions
		limits crypt.Limits
		want   error
	}{
		{"no_credentials", crypt.DecryptOptions{}, crypt.Limits{}, crypt.ErrCredentialsRequired},
		{"bad_password", crypt.DecryptOptions{Password: []byte("wrong")}, crypt.Limits{}, crypt.ErrDecryptFailed},
		{"depth", crypt.DecryptOptions{Password: []byte("secret")}, crypt.Limits{MaxLayers: 1}, crypt.ErrLimitExceeded},
		{"operations", crypt.DecryptOptions{Password: []byte("secret")}, crypt.Limits{MaxOperations: 1}, crypt.ErrLimitExceeded},
		{"recipient_budget", crypt.DecryptOptions{Password: []byte("secret")}, crypt.Limits{MaxRecipients: 1}, crypt.ErrLimitExceeded},
		{"mapping_budget", crypt.DecryptOptions{Password: []byte("secret")}, crypt.Limits{MaxMappings: 5}, crypt.ErrLimitExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := crypt.Encrypt(t.Context(), inner.Bytes(), io.Discard, crypt.EncryptOptions{Recipients: passwordRecipient("new"), Append: &test.old, Limits: test.limits})
			if report != nil || !errors.Is(err, test.want) {
				t.Fatalf("report=%+v err=%v want=%v", report, err, test.want)
			}
		})
	}
	originals := minimalOriginals()
	originals["hidden-signature.xml"] = `<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><References><Reference FileRef="/Encryptions.xml"><CheckValue>AA==</CheckValue></Reference></References></SignedInfo></Signature>`
	inner.Reset()
	if _, err := crypt.Encrypt(t.Context(), archive(t, originals), &inner, crypt.EncryptOptions{Recipients: passwordRecipient("secret")}); err != nil {
		t.Fatal(err)
	}
	report, err := crypt.Encrypt(t.Context(), inner.Bytes(), io.Discard, crypt.EncryptOptions{Recipients: passwordRecipient("new"), Append: &crypt.DecryptOptions{Password: []byte("secret")}})
	if report != nil || !errors.Is(err, crypt.ErrProtectedContent) {
		t.Fatalf("隐藏的入口签名未阻止追加: %+v %v", report, err)
	}
}

func pageScopeFiles() map[string]string {
	return map[string]string{
		"OFD.xml":             `<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocRoot>D/Document.xml</DocRoot></DocBody></OFD>`,
		"D/Document.xml":      `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><PublicRes>Shared.xml</PublicRes><TemplatePage ID="10" BaseLoc="T/Content.xml"/></CommonData><Pages><Page ID="1" BaseLoc="P1/Content.xml"/><Page ID="2" BaseLoc="P2/Content.xml"/></Pages><Annotations>A/Annotations.xml</Annotations></Document>`,
		"D/P1/Content.xml":    `<Page xmlns="http://www.ofdspec.org/2016"><Template TemplateID="10"/><PageRes>Res.xml</PageRes><Content><Layer ID="11"><ImageObject ID="12" ResourceID="20"/></Layer></Content></Page>`,
		"D/P2/Content.xml":    `<Page xmlns="http://www.ofdspec.org/2016"><Content/></Page>`,
		"D/T/Content.xml":     `<Page xmlns="http://www.ofdspec.org/2016"><PageRes>Res.xml</PageRes><Content/></Page>`,
		"D/P1/Res.xml":        `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="../Data"><MultiMedias><MultiMedia ID="20"><MediaFile>image.bin</MediaFile></MultiMedia></MultiMedias></Res>`,
		"D/T/Res.xml":         `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="/D/Data"><Fonts><Font ID="21"><FontFile>template.font</FontFile></Font></Fonts></Res>`,
		"D/Shared.xml":        `<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Data"><Fonts><Font ID="22"><FontFile>shared.font</FontFile></Font></Fonts><ColorSpaces><ColorSpace ID="23" Profile="colors.icc"/></ColorSpaces><MultiMedias><MultiMedia ID="24"><MediaFile>annot.bin</MediaFile></MultiMedia></MultiMedias></Res>`,
		"D/A/Annotations.xml": `<Annotations xmlns="http://www.ofdspec.org/2016"><Page PageID="1"><FileLoc>one.xml</FileLoc></Page><Page PageID="2"><FileLoc>two.xml</FileLoc></Page></Annotations>`,
		"D/A/one.xml":         `<PageAnnot xmlns="http://www.ofdspec.org/2016"><Annot ID="30"><Appearance><ImageObject ID="31" ResourceID="24"/></Appearance></Annot></PageAnnot>`,
		"D/A/two.xml":         `<PageAnnot xmlns="http://www.ofdspec.org/2016"/>`,
		"D/Data/image.bin":    "image", "D/Data/template.font": "template font", "D/Data/shared.font": "shared font", "D/Data/colors.icc": "profile", "D/Data/annot.bin": "annotation image",
		"unreferenced.bin": "untouched",
	}
}

func TestEncryptPageScopeIncludesTemplatesAnnotationsAndWholeResources(t *testing.T) {
	originals := pageScopeFiles()
	var encrypted, plain bytes.Buffer
	report, err := crypt.Encrypt(t.Context(), archive(t, originals), &encrypted, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), IndependentKeys: true, Pages: &crypt.PageSelection{Document: "D/Document.xml", IDs: []string{"1"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"D/P1/Content.xml": true, "D/P1/Res.xml": true, "D/T/Content.xml": true, "D/T/Res.xml": true, "D/Shared.xml": true, "D/A/one.xml": true, "D/Data/image.bin": true, "D/Data/template.font": true, "D/Data/shared.font": true, "D/Data/colors.icc": true, "D/Data/annot.bin": true}
	if len(report.SelectedEntries) != len(want) || !report.SharedResources {
		t.Fatalf("report=%+v", report)
	}
	physical := unpack(t, encrypted.Bytes())
	for name, data := range originals {
		if want[name] {
			if _, exists := physical[name]; exists {
				t.Fatalf("所选页面的明文残留: %s", name)
			}
		} else if physical[name] != data {
			t.Fatalf("范围外条目改变: %s", name)
		}
	}
	if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &plain, crypt.DecryptOptions{Password: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), originals)
}

func TestEncryptPageScopeRejectsIncompleteOrAmbiguousReferences(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing_resource", func(f map[string]string) { delete(f, "D/Data/annot.bin") }},
		{"unknown_template", func(f map[string]string) {
			f["D/P1/Content.xml"] = strings.ReplaceAll(f["D/P1/Content.xml"], `TemplateID="10"`, `TemplateID="99"`)
		}},
		{"template_cycle", func(f map[string]string) {
			f["D/T/Content.xml"] = strings.Replace(f["D/T/Content.xml"], "<Content/>", `<Template TemplateID="10"/><Content/>`, 1)
		}},
		{"indirect_template_cycle", func(f map[string]string) {
			f["D/Document.xml"] = strings.Replace(f["D/Document.xml"], "</CommonData>", `<TemplatePage ID="20" BaseLoc="T/Other.xml"/></CommonData>`, 1)
			f["D/T/Content.xml"] = strings.Replace(f["D/T/Content.xml"], "<Content/>", `<Template TemplateID="20"/><Content/>`, 1)
			f["D/T/Other.xml"] = `<Page xmlns="http://www.ofdspec.org/2016"><Template TemplateID="10"/><Content/></Page>`
		}},
		{"page_path_escapes_package", func(f map[string]string) {
			f["D/Document.xml"] = strings.Replace(f["D/Document.xml"], `BaseLoc="P1/Content.xml"`, `BaseLoc="../../outside.xml"`, 1)
		}},
		{"document_not_referenced_by_root", func(f map[string]string) {
			f["OFD.xml"] = strings.Replace(f["OFD.xml"], "D/Document.xml", "Other/Document.xml", 1)
			f["Other/Document.xml"] = f["D/Document.xml"]
		}},
		{"escaping_base", func(f map[string]string) {
			f["D/Shared.xml"] = strings.Replace(f["D/Shared.xml"], `BaseLoc="Data"`, `BaseLoc="../../outside"`, 1)
		}},
		{"foreign_namespace", func(f map[string]string) {
			f["D/P1/Content.xml"] = strings.ReplaceAll(f["D/P1/Content.xml"], "http://www.ofdspec.org/2016", "foreign")
		}},
		{"duplicate_page", func(f map[string]string) {
			f["D/Document.xml"] = strings.Replace(f["D/Document.xml"], `ID="2"`, `ID="1"`, 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := pageScopeFiles()
			test.mutate(files)
			report, err := crypt.Encrypt(t.Context(), archive(t, files), io.Discard, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), Pages: &crypt.PageSelection{Document: "D/Document.xml", IDs: []string{"1"}}})
			if report != nil || !errors.Is(err, crypt.ErrInvalidMetadata) {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}

func TestEncryptPageScopeTemplateDepthBoundary(t *testing.T) {
	files := pageScopeFiles()
	var templates strings.Builder
	// 所选页面加 31 个模板，依赖深度恰好为 32；各 XML 本身的深度不超过 6。
	for i := 0; i < 31; i++ {
		fmt.Fprintf(&templates, `<TemplatePage ID="%d" BaseLoc="T/Page%d.xml"/>`, 10+i, i)
		body := ""
		if i < 30 {
			body = fmt.Sprintf(`<Template TemplateID="%d"/>`, 11+i)
		}
		files[fmt.Sprintf("D/T/Page%d.xml", i)] = `<Page xmlns="http://www.ofdspec.org/2016">` + body + `<Content/></Page>`
	}
	files["D/Document.xml"] = strings.Replace(files["D/Document.xml"], `<TemplatePage ID="10" BaseLoc="T/Content.xml"/>`, templates.String(), 1)
	for _, maximum := range []int{31, 32} {
		t.Run(fmt.Sprint(maximum), func(t *testing.T) {
			var encrypted bytes.Buffer
			report, err := crypt.Encrypt(t.Context(), archive(t, files), &encrypted, crypt.EncryptOptions{
				Recipients: passwordRecipient("secret"), Limits: crypt.Limits{MaxXMLDepth: maximum},
				Pages: &crypt.PageSelection{Document: "D/Document.xml", IDs: []string{"1"}},
			})
			if maximum == 31 {
				if report != nil || !errors.Is(err, crypt.ErrLimitExceeded) || encrypted.Len() != 0 {
					t.Fatalf("模板深度超限未在输出前拒绝: report=%+v err=%v bytes=%d", report, err, encrypted.Len())
				}
				return
			}
			if err != nil || report == nil {
				t.Fatalf("模板深度等于上限时失败: report=%+v err=%v", report, err)
			}
			var plain bytes.Buffer
			if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &plain, crypt.DecryptOptions{Password: []byte("secret")}); err != nil {
				t.Fatal(err)
			}
			checkOriginals(t, plain.Bytes(), files)
		})
	}
}

func TestEncryptIndependentAndAppendedSignedDocumentKeepsSignatureResults(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 testdata/ofdrw/999.ofd")
	}
	if err != nil {
		t.Fatal(err)
	}
	var inner, outer, plain bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), input, &inner, crypt.EncryptOptions{Recipients: passwordRecipient("inner"), IndependentKeys: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := crypt.Encrypt(t.Context(), inner.Bytes(), &outer, crypt.EncryptOptions{Recipients: passwordRecipient("outer"), Append: &crypt.DecryptOptions{Password: []byte("inner")}}); err != nil {
		t.Fatal(err)
	}
	resolver := func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
		password := "inner"
		if r.OperationID == "enc2" {
			password = "outer"
		}
		return crypt.Credentials{Password: []byte(password)}, nil
	}
	if _, err := crypt.Decrypt(t.Context(), outer.Bytes(), &plain, crypt.DecryptOptions{Resolve: resolver}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), unpack(t, input))
	results, err := sign.Verify(t.Context(), plain.Bytes(), sign.VerificationOptions{})
	if err != nil || len(results) == 0 || !results[0].DigestValid || !results[0].Result.Valid {
		t.Fatalf("签章恢复失败: %+v %v", results, err)
	}
}

func TestEncryptIndependentSeedBudgetsAndReadAttemptLimit(t *testing.T) {
	originals := minimalOriginals()
	for n := 0; n < 35; n++ {
		originals[fmt.Sprintf("asset-%02d.bin", n)] = "same bytes"
	}
	for _, limits := range []crypt.Limits{{MaxRecipients: 10}, {MaxTotalMetadataBytes: 2048}, {MaxEntries: 50}} {
		report, err := crypt.Encrypt(t.Context(), archive(t, originals), io.Discard, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), IndependentKeys: true, Limits: limits})
		if report != nil || !errors.Is(err, crypt.ErrLimitExceeded) {
			t.Fatalf("limits=%+v report=%+v err=%v", limits, report, err)
		}
	}
	var encrypted, plain bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), archive(t, originals), &encrypted, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), IndependentKeys: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), io.Discard, crypt.DecryptOptions{Password: []byte("secret")}); !errors.Is(err, crypt.ErrLimitExceeded) {
		t.Fatalf("独立 seed 未累计凭据预算: %v", err)
	}
	if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &plain, crypt.DecryptOptions{Password: []byte("secret"), Limits: crypt.Limits{MaxCredentialAttempts: 64}}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), originals)
}

func TestEncryptPageScopeProtectsSignedFixtureAndPreservesSignature(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 testdata/ofdrw/999.ofd")
	}
	if err != nil {
		t.Fatal(err)
	}
	originals := unpack(t, input)
	var doc struct {
		Pages []struct {
			ID string `xml:"ID,attr"`
		} `xml:"Pages>Page"`
	}
	if err := xml.Unmarshal([]byte(originals["Doc_0/Document.xml"]), &doc); err != nil || len(doc.Pages) == 0 {
		t.Fatalf("文档页面无法解析: %v", err)
	}
	selection := crypt.PageSelection{Document: "Doc_0/Document.xml"}
	for _, page := range doc.Pages {
		selection.IDs = append(selection.IDs, page.ID)
	}
	var encrypted, plain bytes.Buffer
	report, err := crypt.Encrypt(t.Context(), input, &encrypted, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), Pages: &selection})
	if err != nil {
		t.Fatal(err)
	}
	physical := unpack(t, encrypted.Bytes())
	if _, retained := physical["Doc_0/Signs/Sign_0/SignedValue.dat"]; retained {
		t.Fatalf("页面上的印章签名值仍为明文: %+v", report)
	}
	if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &plain, crypt.DecryptOptions{Password: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), originals)
	results, err := sign.Verify(t.Context(), plain.Bytes(), sign.VerificationOptions{})
	if err != nil || len(results) == 0 || !results[0].DigestValid || !results[0].Result.Valid {
		t.Fatalf("页面范围恢复后签章失败: %+v %v", results, err)
	}
}

func TestEncryptAppendSupportsDefaultNamespaceAndPhysicalEntrySelection(t *testing.T) {
	var inner, outer, plain bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), archive(t, minimalOriginals()), &inner, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), KeepRoot: true}); err != nil {
		t.Fatal(err)
	}
	files := unpack(t, inner.Bytes())
	root := strings.ReplaceAll(strings.ReplaceAll(files["Encryptions.xml"], "xmlns:ofd", "xmlns"), "ofd:", "")
	files["Encryptions.xml"] = root
	report, err := crypt.Encrypt(t.Context(), archive(t, files), &outer, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), Append: &crypt.DecryptOptions{Password: []byte("secret")}, Entries: []string{"Encrypt_1/decryptseed.dat"}})
	if err != nil || report.EncryptedEntries != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	physical := unpack(t, outer.Bytes())
	if !strings.HasPrefix(physical["Encryptions.xml"], strings.TrimSuffix(root, "</Encryptions>")) || physical["OFD.xml"] != files["OFD.xml"] {
		t.Fatal("默认命名空间记录或未选主入口被改写")
	}
	if _, err := crypt.Decrypt(t.Context(), outer.Bytes(), &plain, crypt.DecryptOptions{Password: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), minimalOriginals())
}

func TestEncryptAppendUsesRecoveredRootInsteadOfPlaceholder(t *testing.T) {
	originals := minimalOriginals()
	files := encryptedFiles(t, originals, 1, false, false)
	files["OFD.xml"] = "non-confidential placeholder"
	var outer, plain bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), archive(t, files), &outer, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), Append: &crypt.DecryptOptions{Password: []byte("secret")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := crypt.Decrypt(t.Context(), outer.Bytes(), &plain, crypt.DecryptOptions{Password: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), originals)
}

func TestEncryptAppendRejectsIndependentOperationBranches(t *testing.T) {
	originals := minimalOriginals()
	first := encryptedFiles(t, map[string]string{"Doc_0/data.bin": originals["Doc_0/data.bin"]}, 1, false, false)
	second := encryptedFiles(t, map[string]string{"Doc_0/empty.bin": originals["Doc_0/empty.bin"]}, 1, false, false)
	files := map[string]string{}
	for name, data := range originals {
		if name != "Doc_0/data.bin" && name != "Doc_0/empty.bin" {
			files[name] = data
		}
	}
	for name, data := range first {
		if name != "Encryptions.xml" && name != "untouched.bin" {
			files[name] = data
		}
	}
	for name, data := range second {
		if name == "Encryptions.xml" || name == "untouched.bin" {
			continue
		}
		data = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(data, `ID="enc1"`, `ID="enc2"`), "Security/", "Second/Security/"), `EPath="/Cipher/`, `EPath="/Second/Cipher/`)
		files["Second/"+name] = data
	}
	other := strings.ReplaceAll(strings.ReplaceAll(operationElement(second["Encryptions.xml"]), `ID="enc1"`, `ID="enc2"`), "Security/", "Second/Security/")
	files["Encryptions.xml"] = strings.Replace(first["Encryptions.xml"], "</ofd:Encryptions>", other+"</ofd:Encryptions>", 1)
	report, err := crypt.Encrypt(t.Context(), archive(t, files), io.Discard, crypt.EncryptOptions{Recipients: passwordRecipient("secret"), Append: &crypt.DecryptOptions{Password: []byte("secret")}})
	if report != nil || !errors.Is(err, crypt.ErrUnsupported) {
		t.Fatalf("独立操作分支未拒绝: %+v %v", report, err)
	}
}
