package ses_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/integrity"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func TestGMT0099AppendPreservesMultipleSignersAndLocksFinalList(t *testing.T) {
	f := newSigningFixture(t, false)
	other := newSigningFixtureAt(t, false, f.now)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	options := *f.options
	options.Trust.Roots.AddCert(other.root)
	options.Revocation = &sign.CertificateRevocationOptions{
		CRLs:    append(f.options.Revocation.CRLs, other.options.Revocation.CRLs...),
		Issuers: [][]byte{f.root.Raw, other.root.Raw},
	}
	image := filepath.Join(t.TempDir(), "seal.png")
	if err := os.WriteFile(image, makeTestSealPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	input := gmtArchive(t, gmtFiles())
	for step := 1; step <= 3; step++ {
		previous := gmtUnpack(t, input)
		var out bytes.Buffer
		opts := sign.Options{Type: "Sign", Append: true, Date: f.now, ProviderName: "co-sign", Sign: digitalSigner(t, f, false), SealImage: image}
		if step > 1 {
			opts.Relative = fmt.Sprintf("sign-%d", step-1)
		}
		if step == 2 {
			opts.Sign = digitalSigner(t, other, false)
		}
		opts.LockSignatures = step == 3
		if err := sign.Sign(t.Context(), input, &out, opts); err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		input = bytes.Clone(out.Bytes())
		current := gmtUnpack(t, input)
		for name, data := range previous {
			if name == "Doc_0/Signatures.xml" || step == 1 && name == "OFD.xml" {
				continue
			}
			if !bytes.Equal(data, current[name]) {
				t.Fatalf("step %d 改写已有文件 %s", step, name)
			}
		}
		results, err := sign.Verify(t.Context(), input, options)
		if err != nil || len(results) != step {
			t.Fatalf("step %d: results=%+v err=%v", step, results, err)
		}
		for _, result := range results {
			if result.Error != "" || !result.DigestValid || !result.Result.FullyValid() {
				t.Fatalf("step %d: %+v", step, result)
			}
		}
		instance, err := validator.New(validator.WithGMT0099(true))
		if err != nil {
			t.Fatal(err)
		}
		report := instance.ValidateReader(t.Context(), bytes.NewReader(input), "co-signed.ofd")
		if report.HasErrors() || report.Summary.Warnings != 0 {
			t.Fatalf("step %d 结构或摘要校验失败: %+v", step, report)
		}
		if step == 2 {
			protected := protectGMT(t, f, input, true)
			report, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: options})
			if err != nil || report.Status != "incomplete" || len(report.Unprotected) != 1 || report.Unprotected[0] != "Doc_0/Signatures.xml" {
				t.Fatalf("未锁定列表被误报为完整: %+v %v", report, err)
			}
		}
	}
	protected := protectGMT(t, f, input, true)
	report, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: options})
	if err != nil || report.Status != "valid" || len(report.Unprotected) != 0 {
		t.Fatalf("多签与清单未完整通过: %+v %v", report, err)
	}
	called := false
	var out bytes.Buffer
	err = sign.Sign(t.Context(), input, &out, sign.Options{Type: "Sign", Append: true, Sign: func(context.Context, []byte) ([]byte, error) {
		called = true
		return nil, errors.New("不应调用签名器")
	}})
	if !errors.Is(err, crypt.ErrProtectedContent) || called || out.Len() != 0 {
		t.Fatalf("已锁定列表仍被追加: %v called=%v size=%d", err, called, out.Len())
	}
	changed := gmtUnpack(t, input)
	value := changed["Doc_0/Signatures/sign-1/Data/sign-1.dat"]
	value[len(value)-1] ^= 1
	results, err := sign.Verify(t.Context(), gmtArchive(t, changed), options)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Result.FullyValid() {
			t.Fatalf("篡改基础签名后仍通过: %+v", result)
		}
	}
}

func TestGMT0099AppendRejectsInvalidInputsBeforeCallingSigner(t *testing.T) {
	f := newSigningFixture(t, false)
	var first, locked bytes.Buffer
	opts := sign.Options{Type: "Sign", Append: true, Date: f.now, Sign: digitalSigner(t, f, false)}
	if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &first, opts); err != nil {
		t.Fatal(err)
	}
	opts.Append = false
	if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &locked, opts); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_protected", "duplicate_id", "missing_relative", "bad_relative", "changed_content", "changed_value", "unknown_xml", "path_collision", "case_collision", "case_fallback", "dangling_relative", "cyclic_relative", "manifest"} {
		t.Run(name, func(t *testing.T) {
			files := gmtUnpack(t, first.Bytes())
			options := sign.Options{Type: "Sign", Append: true}
			protected := false
			switch name {
			case "list_protected":
				files = gmtUnpack(t, locked.Bytes())
				protected = true
			case "duplicate_id":
				options.ID = "sign-1"
			case "missing_relative":
				options.Relative = "missing"
			case "bad_relative":
				options.Relative = "../sign-1"
			case "changed_content":
				files["Doc_0/Pages/Content.xml"] = append(files["Doc_0/Pages/Content.xml"], ' ')
			case "changed_value":
				value := files["Doc_0/Signatures/sign-1/Data/sign-1.dat"]
				value[len(value)-1] ^= 1
			case "unknown_xml":
				files["Doc_0/Signatures.xml"] = bytes.Replace(files["Doc_0/Signatures.xml"], []byte("</Signatures>"), []byte("<Unknown/></Signatures>"), 1)
			case "path_collision":
				files["Doc_0/Signatures/sign-2/payload.bin"] = []byte("do not overwrite")
				protected = true
			case "case_collision":
				files["ofd.xml"] = files["OFD.xml"]
			case "case_fallback":
				files["Doc_0/Signatures.xml"] = bytes.ReplaceAll(files["Doc_0/Signatures.xml"], []byte("Signature_sign-1.xml"), []byte("signature_sign-1.xml"))
			case "dangling_relative":
				files["Doc_0/Signatures.xml"] = bytes.ReplaceAll(files["Doc_0/Signatures.xml"], []byte(`Type="Sign"`), []byte(`Type="Sign" Relative="missing"`))
			case "cyclic_relative":
				files["Doc_0/Signatures.xml"] = bytes.ReplaceAll(files["Doc_0/Signatures.xml"], []byte(`Type="Sign"`), []byte(`Type="Sign" Relative="sign-1"`))
			case "manifest":
				files["OFDEntries.xml"] = []byte("protected")
				protected = true
			}
			called := false
			options.Sign = func(context.Context, []byte) ([]byte, error) {
				called = true
				return nil, errors.New("不应调用签名器")
			}
			var out bytes.Buffer
			err := sign.Sign(t.Context(), gmtArchive(t, files), &out, options)
			if err == nil || called || out.Len() != 0 || protected && !errors.Is(err, crypt.ErrProtectedContent) {
				t.Fatalf("预检未拒绝: %v called=%v size=%d", err, called, out.Len())
			}
		})
	}
}

func TestGMT0099AppendSupportsCustomCatalogAndMultipleDocuments(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(time.Second)
	files := gmtFiles()
	for name, data := range gmtFiles() {
		if strings.HasPrefix(name, "Doc_0/") {
			files[strings.Replace(name, "Doc_0/", "Doc_1/", 1)] = bytes.Clone(data)
		}
	}
	files["OFD.xml"] = bytes.Replace(files["OFD.xml"], []byte("</OFD>"), []byte(`<DocBody><DocInfo><DocID>abcdef1234567890abcdef1234567890</DocID></DocInfo><DocRoot>Doc_1/Document.xml</DocRoot><Signatures>Doc_1/CustomList.xml</Signatures></DocBody></OFD>`), 1)
	files["Doc_1/CustomList.xml"] = []byte(`<Signatures xmlns="http://www.ofdspec.org/2016"/>`)
	input := gmtArchive(t, files)
	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		if err := sign.Sign(t.Context(), input, &out, sign.Options{Type: "Sign", Append: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
			t.Fatal(err)
		}
		input = bytes.Clone(out.Bytes())
		results, err := sign.Verify(t.Context(), input, *f.options)
		if err != nil || len(results) != (i+1)*2 {
			t.Fatalf("results=%+v err=%v", results, err)
		}
		for _, result := range results {
			if result.Error != "" || !result.Result.FullyValid() {
				t.Fatalf("自定义列表或多文档验证失败: %+v", result)
			}
		}
	}
	ofd, err := parser.NewOFD(input)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if len(ofd.Documents) != 2 {
		t.Fatal("文档体数量改变")
	}
}

func TestGMT0099AppendPropagatesCancellationAndWriterFailure(t *testing.T) {
	f := newSigningFixture(t, false)
	input := gmtArchive(t, gmtFiles())
	opts := sign.Options{Type: "Sign", Append: true, Date: f.now, Sign: digitalSigner(t, f, false)}
	want := errors.New("writer failed")
	if err := sign.Sign(t.Context(), input, gmtFailWriter{want}, opts); !errors.Is(err, want) {
		t.Fatalf("输出错误丢失: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	create := opts.Sign
	opts.Sign = func(ctx context.Context, data []byte) ([]byte, error) {
		value, err := create(ctx, data)
		cancel()
		return value, err
	}
	var out bytes.Buffer
	if err := sign.Sign(ctx, input, &out, opts); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("取消未阻止输出: %v size=%d", err, out.Len())
	}
	if err := sign.Sign(t.Context(), input, io.Discard, sign.Options{Type: "Seal", Append: true, Sign: create}); err == nil {
		t.Fatal("追加模式不能写入 SES")
	}
}

func TestGMT0099AppendProtectsRootForNestedDocumentDirectory(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(time.Second)
	files := map[string][]byte{}
	for name, data := range gmtFiles() {
		files[strings.Replace(name, "Doc_0/", "Documents/Doc_0/", 1)] = bytes.ReplaceAll(data, []byte("Doc_0/"), []byte("Documents/Doc_0/"))
	}
	input := gmtArchive(t, files)
	for n := 0; n < 2; n++ {
		var out bytes.Buffer
		if err := sign.Sign(t.Context(), input, &out, sign.Options{Type: "Sign", Append: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
			t.Fatal(err)
		}
		input = bytes.Clone(out.Bytes())
	}
	results, err := sign.Verify(t.Context(), input, *f.options)
	if err != nil || len(results) != 2 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	for _, result := range results {
		if result.Error != "" || !result.Result.FullyValid() {
			t.Fatalf("深层文档目录的根引用无效: %+v", result)
		}
	}
}

func TestGMT0099AppendEnforcesRelativeDepthBeforeCallingSigner(t *testing.T) {
	f := newSigningFixture(t, false)
	var first bytes.Buffer
	if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &first, sign.Options{Type: "Sign", Append: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	files := gmtUnpack(t, first.Bytes())
	data := files["Doc_0/Signatures/sign-1/Signature_sign-1.xml"]
	delete(files, "Doc_0/Signatures/sign-1/Signature_sign-1.xml")
	delete(files, "Doc_0/Signatures/sign-1/Data/sign-1.dat")
	var list strings.Builder
	list.WriteString(`<Signatures xmlns="http://www.ofdspec.org/2016">`)
	for n := 1; n <= 32; n++ {
		id := fmt.Sprintf("base-%d", n)
		original := bytes.ReplaceAll(data, []byte("sign-1"), []byte(id))
		value, err := digitalSigner(t, f, false)(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		name := "Doc_0/Signatures/" + id + "/Signature_" + id + ".xml"
		files[name], files["Doc_0/Signatures/"+id+"/Data/"+id+".dat"] = original, value
		relative := ""
		if n > 1 {
			relative = fmt.Sprintf(` Relative="base-%d"`, n-1)
		}
		fmt.Fprintf(&list, `<Signature ID="%s" Type="Sign" BaseLoc="/%s"%s/>`, id, name, relative)
	}
	list.WriteString(`</Signatures>`)
	files["Doc_0/Signatures.xml"] = []byte(list.String())
	input := gmtArchive(t, files)
	for _, base := range []int{31, 32} {
		called := false
		var out bytes.Buffer
		err := sign.Sign(t.Context(), input, &out, sign.Options{Type: "Sign", Append: true, Date: f.now, Relative: fmt.Sprintf("base-%d", base), Sign: func(ctx context.Context, data []byte) ([]byte, error) {
			called = true
			return digitalSigner(t, f, false)(ctx, data)
		}})
		if base == 31 && (err != nil || !called || out.Len() == 0) {
			t.Fatalf("32 层链被拒绝: %v called=%v", err, called)
		}
		if base == 32 && (err == nil || called || out.Len() != 0) {
			t.Fatalf("33 层链未在签署前拒绝: %v called=%v", err, called)
		}
	}
}
