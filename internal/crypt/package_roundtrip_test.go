package crypt

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/container"
)

func testPackage(t *testing.T, files map[string][]byte) *container.Package {
	t.Helper()
	pkg, err := container.OpenBytes(memoryZIP(t, files))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pkg.Close(); err != nil {
			t.Error(err)
		}
	})
	return pkg
}

func testPlainFiles() map[string][]byte {
	return map[string][]byte{
		"OFD.xml":            []byte(`<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc_0/Document.xml": []byte(`<Document xmlns="http://www.ofdspec.org/2016"/>`),
		"Doc_0/Res/data.bin": {0, 1, 2, 0xff, '\r', '\n'},
	}
}

func testPasswordOptions() EncryptOptions {
	return EncryptOptions{Recipients: []EncryptRecipient{{UserName: "reader", Password: []byte("secret")}}}
}

// encryptFiles 必须复制回调字节：EncryptPackage 在回调返回后会清零内部缓冲区。
func encryptFiles(t *testing.T, files map[string][]byte, options EncryptOptions) (map[string][]byte, *EncryptReport) {
	t.Helper()
	output := map[string][]byte{}
	report, err := EncryptPackage(t.Context(), testPackage(t, files), options, func(name string, data []byte) error {
		if _, exists := output[name]; exists {
			t.Fatalf("输出重复条目 %q", name)
		}
		output[name] = bytes.Clone(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return output, report
}

func decryptedFiles(t *testing.T, pkg *container.Package, options DecryptOptions) (map[string][]byte, *DecryptReport) {
	t.Helper()
	output := map[string][]byte{}
	report, err := DecryptPackage(t.Context(), pkg, options, func(name string, data []byte) error { output[name] = bytes.Clone(data); return nil })
	if err != nil {
		t.Fatal(err)
	}
	return output, report
}

func TestPackagePasswordRoundTripPreservesOriginalBytes(t *testing.T) {
	for _, test := range []struct {
		name                                string
		keepRoot, plaintextMap, independent bool
		password                            []byte
	}{
		{"encrypted map", false, false, false, []byte("secret")},
		{"plaintext map", true, true, false, []byte("secret")},
		{"independent entry keys", false, false, true, []byte("secret")},
		{"independent keys and plaintext map", true, true, true, []byte("secret")},
		{"explicit empty password", false, false, false, []byte{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := testPlainFiles()
			options := EncryptOptions{KeepRoot: test.keepRoot, PlaintextMap: test.plaintextMap, IndependentKeys: test.independent, Recipients: []EncryptRecipient{
				{UserName: "other", Password: []byte("other password")}, {UserName: "reader", UserType: "Owner", Password: test.password},
			}}
			encrypted, encReport := encryptFiles(t, original, options)
			pkg := testPackage(t, encrypted)
			info, err := InspectPackage(t.Context(), pkg, InspectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !info.Encrypted || info.State != "locked" || len(info.Operations) != 1 || info.Operations[0].Seed.Scheme != PasswordScheme {
				t.Fatalf("加密检查结果错误: %+v", info)
			}
			mapState := "encrypted"
			if test.plaintextMap {
				mapState = "plaintext"
			}
			if info.Operations[0].MapState != mapState {
				t.Fatalf("映射表状态=%q，期望 %q", info.Operations[0].MapState, mapState)
			}
			if test.keepRoot && !bytes.Equal(encrypted["OFD.xml"], original["OFD.xml"]) {
				t.Fatal("KeepRoot 应原样保留主入口")
			}
			output, report := decryptedFiles(t, pkg, DecryptOptions{Password: test.password})
			if !reflect.DeepEqual(output, original) {
				t.Fatalf("解密后未恢复全部原始字节: %+v", output)
			}
			wantEncrypted := len(original)
			if test.keepRoot {
				wantEncrypted--
			}
			if report.State != "unlocked" || report.DecryptedEntries != wantEncrypted || encReport.EncryptedEntries != wantEncrypted || encReport.IndependentKeys != test.independent {
				t.Fatalf("加解密报告错误: encrypted=%+v decrypted=%+v", encReport, report)
			}
			if !test.independent || !test.plaintextMap {
				if report.Recipient.UserName != "reader" || report.Recipient.UserType != "Owner" {
					t.Fatalf("通用密钥的接收者错误: %+v", report.Recipient)
				}
			}
			for _, operation := range report.Operations {
				for _, key := range operation.Keys {
					if key.Recipient.UserName != "reader" || key.Recipient.UserType != "Owner" {
						t.Fatalf("条目密钥的接收者错误: %+v", key)
					}
				}
			}
			var outputBytes int64
			for _, data := range original {
				outputBytes += int64(len(data))
			}
			if report.OutputEntries != len(original) || report.OutputBytes != outputBytes {
				t.Fatalf("输出计数错误: %+v", report)
			}
		})
	}
}

func encryptionCertificate(t *testing.T) (*sm2.PrivateKey, []byte) {
	t.Helper()
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	template := &smx509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "crypt test recipient"}, NotBefore: now, NotAfter: now.AddDate(1, 0, 0), KeyUsage: smx509.KeyUsageKeyEncipherment}
	der, err := smx509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return key, der
}

func TestPackageCertificateRoundTripAndDecrypterFailure(t *testing.T) {
	key, certificate := encryptionCertificate(t)
	original := testPlainFiles()
	encrypted, encReport := encryptFiles(t, original, EncryptOptions{Recipients: []EncryptRecipient{{UserName: "certificate reader", Certificate: certificate}}})
	pkg := testPackage(t, encrypted)
	output, report := decryptedFiles(t, pkg, DecryptOptions{PrivateKey: key})
	if !reflect.DeepEqual(output, original) || report.Scheme != CertificateScheme || report.Recipient.CertificateSHA256 != fmt.Sprintf("%x", sha256.Sum256(certificate)) || encReport.Scheme != CertificateScheme {
		t.Fatalf("SM2 证书加解密结果不正确: %+v", report)
	}
	serviceError := errors.New("密钥设备不可用")
	for _, test := range []struct {
		name    string
		options DecryptOptions
		want    error
	}{
		{"no key", DecryptOptions{}, ErrCredentialsRequired},
		{"decrypter failure", DecryptOptions{PrivateKey: errorDecrypter{Decrypter: key, err: serviceError}}, serviceError},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			report, err := DecryptPackage(t.Context(), pkg, test.options, func(string, []byte) error { called = true; return nil })
			if !errors.Is(err, test.want) || report != nil || called {
				t.Fatalf("失败时应保留错误且不产生输出: report=%+v, err=%v, emitted=%t", report, err, called)
			}
		})
	}
}

type errorDecrypter struct {
	crypto.Decrypter
	err error
}

func (d errorDecrypter) Decrypt(io.Reader, []byte, crypto.DecrypterOpts) ([]byte, error) {
	return nil, d.err
}

func TestPackageOperationsPreserveCancellationAndEmitErrors(t *testing.T) {
	plain := testPackage(t, testPlainFiles())
	encryptedFiles, _ := encryptFiles(t, testPlainFiles(), testPasswordOptions())
	encrypted := testPackage(t, encryptedFiles)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	emitError := errors.New("输出写入失败")
	for _, test := range []struct {
		name            string
		encrypt         bool
		ctx             context.Context
		emitError, want error
	}{
		{"cancel encrypt", true, ctx, nil, context.Canceled},
		{"cancel decrypt", false, ctx, nil, context.Canceled},
		{"encrypt output failure", true, t.Context(), emitError, emitError},
		{"decrypt output failure", false, t.Context(), emitError, emitError},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			emit := func(string, []byte) error { calls++; return test.emitError }
			var err error
			if test.encrypt {
				_, err = EncryptPackage(test.ctx, plain, testPasswordOptions(), emit)
			} else {
				_, err = DecryptPackage(test.ctx, encrypted, DecryptOptions{Password: []byte("secret")}, emit)
			}
			if !errors.Is(err, test.want) || test.emitError == nil && calls != 0 || test.emitError != nil && calls != 1 {
				t.Fatalf("应保留取消或输出错误并停止后续输出: calls=%d, err=%v", calls, err)
			}
		})
	}
	if _, err := DecryptPackage(t.Context(), plain, DecryptOptions{}, func(string, []byte) error { return nil }); !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("普通包不应被当成加密包: %v", err)
	}
	if _, err := EncryptPackage(t.Context(), encrypted, testPasswordOptions(), func(string, []byte) error { return nil }); !errors.Is(err, ErrAlreadyEncrypted) {
		t.Fatalf("重复加密需要显式追加: %v", err)
	}
}

func TestViewSnapshotsReadsAndPhysicalLease(t *testing.T) {
	original := testPlainFiles()
	options := testPasswordOptions()
	options.IndependentKeys, options.PlaintextMap = true, true
	encrypted, _ := encryptFiles(t, original, options)
	pkg := testPackage(t, encrypted)
	view, err := OpenView(t.Context(), pkg, OpenOptions{DecryptOptions: DecryptOptions{Password: []byte("secret")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = view.Close() })
	if view.InputSize() != pkg.InputSize() {
		t.Fatal("视图应报告原始 ZIP 大小")
	}
	info := view.EncryptionInfo()
	info.Operations[0].Seed.Recipients[0].UserName = "changed"
	info.Operations[0].Entries[0].Seed.Recipients[0].UserName = "changed"
	if fresh := view.EncryptionInfo(); fresh.Operations[0].Seed.Recipients[0].UserName != "reader" || fresh.Operations[0].Entries[0].Seed.Recipients[0].UserName != "reader" {
		t.Fatal("检查信息快照不应共享嵌套的可修改切片")
	}
	entries := view.Entries()
	var names []string
	for _, entry := range entries {
		if !entry.Virtual || entry.Locked || entry.SizeUnknown {
			t.Fatalf("解锁视图的条目元数据错误: %+v", entry)
		}
		names = append(names, entry.Path)
	}
	if !slices.IsSorted(names) || len(names) != len(original) {
		t.Fatalf("逻辑条目应排序且不包含加密控制文件: %v", names)
	}
	for name, want := range original {
		data, err := view.Read("/" + name)
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("读取逻辑条目 %s 失败: %v", name, err)
		}
		if len(data) > 0 {
			data[0] ^= 1
		}
		again, err := view.Read(name)
		if err != nil || !bytes.Equal(again, want) {
			t.Fatalf("调用方修改返回值不应影响再次解密: %s, %v", name, err)
		}
	}
	if _, err := view.Read("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("缺失条目应保留不存在错误: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := view.ReadContext(ctx, "OFD.xml"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	physical, err := view.OpenPhysicalPackage()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = physical.Close() })
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := physical.Read(EntryPoint); err != nil || !bytes.Equal(data, encrypted[EntryPoint]) {
		t.Fatalf("物理租约应在视图所有者关闭后保持原始密文可读: %v", err)
	}
	if _, err := view.Read("OFD.xml"); !errors.Is(err, container.ErrPackageClosed) {
		t.Fatalf("视图关闭后应拒绝读取: %v", err)
	}
	if _, err := view.OpenPhysicalPackage(); !errors.Is(err, container.ErrPackageClosed) {
		t.Fatalf("视图关闭后不能创建物理租约: %v", err)
	}
}

func TestAllowPartialDoesNotSuppressWrongCredentials(t *testing.T) {
	for _, plaintextMap := range []bool{false, true} {
		t.Run(fmt.Sprintf("plaintextMap=%t", plaintextMap), func(t *testing.T) {
			options := testPasswordOptions()
			options.PlaintextMap, options.KeepRoot = plaintextMap, true
			files, _ := encryptFiles(t, testPlainFiles(), options)
			pkg := testPackage(t, files)
			view, err := OpenView(t.Context(), pkg, OpenOptions{AllowPartial: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if info := view.EncryptionInfo(); info.State != "locked" || info.Opaque == plaintextMap {
				t.Fatalf("缺失凭据的视图状态错误: %+v", info)
			}
			if _, err := view.OFDPackage(); !errors.Is(err, ErrLocked) {
				t.Fatalf("未完整解锁的视图不能交给文档处理器: %v", err)
			}
			if _, err := view.Export(t.Context(), func(string, []byte) error { t.Error("锁定视图不应导出"); return nil }); !errors.Is(err, ErrLocked) {
				t.Fatalf("锁定视图应拒绝导出: %v", err)
			}
			if _, err := view.Read("Doc_0/Document.xml"); !errors.Is(err, ErrLocked) {
				t.Fatalf("加密条目应保持锁定: %v", err)
			}
			if plaintextMap {
				if data, err := view.Read("OFD.xml"); err != nil || !bytes.Equal(data, testPlainFiles()["OFD.xml"]) {
					t.Fatalf("明文映射允许读取未加密入口: %v", err)
				}
			} else if len(view.Entries()) != 0 {
				t.Fatal("不透明映射不能枚举逻辑路径")
			}
			if bad, err := OpenView(t.Context(), pkg, OpenOptions{AllowPartial: true, DecryptOptions: DecryptOptions{Password: []byte("wrong")}}, nil); !errors.Is(err, ErrDecryptFailed) || bad != nil {
				t.Fatalf("AllowPartial 不能忽略错误凭据: view=%v, err=%v", bad, err)
			}
		})
	}
}
