package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/reader"
)

func desktopWithSignature(t *testing.T) (*Desktop, *openedDocument, reader.SignatureInfo, string) {
	t.Helper()
	return desktopWithSignatureFixture(t, "999.ofd")
}

func desktopWithSignatureFixture(t *testing.T, name string) (*Desktop, *openedDocument, reader.SignatureInfo, string) {
	t.Helper()
	fixture := filepath.Join("..", "..", "testdata", "ofdrw", name)
	data, err := os.ReadFile(fixture)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少签名样本 %s", fixture)
	}
	if err != nil {
		t.Fatal(err)
	}
	// 测试只对临时副本执行保存和输入保护检查。
	input := filepath.Join(t.TempDir(), "signed.ofd")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	desktop := &Desktop{document: service}
	opened, err := desktop.openPath(input)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := reader.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	infos, err := doc.Signatures()
	if err != nil || len(infos) == 0 {
		t.Fatalf("签名清单 = %+v, %v", infos, err)
	}
	return desktop, opened, infos[0], input
}

func TestNativeSignatureExportsMatchOriginalData(t *testing.T) {
	desktop, opened, info, input := desktopWithSignature(t)
	doc, err := reader.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	wantValue, err := doc.SignatureValue(info.Scope, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "signature.dat")
	if saved, err := desktop.saveSignatureValueWithPicker(t.Context(), opened.PagePrefix, info.Scope, info.ID, func(name string) (string, error) {
		if !strings.HasPrefix(name, "signature-") || !strings.HasSuffix(name, ".dat") {
			t.Errorf("签名文件名 = %q", name)
		}
		return output, nil
	}); err != nil || !saved {
		t.Fatalf("签名值保存 = %v, %v", saved, err)
	}
	if data, err := os.ReadFile(output); err != nil || !bytes.Equal(data, wantValue) {
		t.Fatalf("签名值不一致: %v", err)
	}
	found := false
	for _, cert := range info.Certificates {
		if cert.Subject == "" {
			continue
		}
		found = true
		wantDER, err := doc.SignatureCertificate(info.Scope, info.ID, cert.SlotKey)
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"der", "pem"} {
			output := filepath.Join(t.TempDir(), "certificate."+format)
			if saved, err := desktop.saveSignatureCertificateWithPicker(t.Context(), opened.PagePrefix, info.Scope, info.ID, cert.SlotKey, format, func(name string) (string, error) {
				if !strings.HasSuffix(name, "-"+cert.SlotKey+"."+format) {
					t.Errorf("证书文件名 = %q", name)
				}
				return output, nil
			}); err != nil || !saved {
				t.Fatalf("证书 %s 保存 = %v, %v", format, saved, err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if format == "pem" {
				block, rest := pem.Decode(data)
				if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
					t.Fatal("PEM 格式错误")
				}
				data = block.Bytes
			}
			if !bytes.Equal(data, wantDER) {
				t.Fatal("证书 DER 与原始数据不一致")
			}
		}
	}
	if !found {
		t.Fatal("样本没有可导出的证书")
	}
}

func TestNativeSignatureSaveCancellationStaleSessionAndInputLinks(t *testing.T) {
	desktop, opened, info, input := desktopWithSignature(t)
	slot := ""
	for _, cert := range info.Certificates {
		if cert.Subject != "" {
			slot = cert.SlotKey
			break
		}
	}
	if slot == "" {
		t.Fatal("样本没有可导出的证书")
	}
	for _, kind := range []string{"value", "der", "pem"} {
		save := func(ctx context.Context, choose func(string) (string, error)) (bool, error) {
			if kind == "value" {
				return desktop.saveSignatureValueWithPicker(ctx, opened.PagePrefix, info.Scope, info.ID, choose)
			}
			return desktop.saveSignatureCertificateWithPicker(ctx, opened.PagePrefix, info.Scope, info.ID, slot, kind, choose)
		}
		if saved, err := save(t.Context(), func(string) (string, error) { return "", nil }); err != nil || saved {
			t.Fatalf("取消 = %v, %v", saved, err)
		}
		for _, link := range []func(string, string) error{nil, os.Symlink, os.Link} {
			target := input
			if link != nil {
				target = filepath.Join(t.TempDir(), "input-link")
				if err := link(input, target); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := save(t.Context(), func(string) (string, error) { return target, nil }); !errors.Is(err, errOverwriteInput) {
				t.Fatalf("输入保护 = %v", err)
			}
		}
		output := filepath.Join(t.TempDir(), "existing")
		if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		if _, err := save(ctx, func(string) (string, error) { cancel(); return output, nil }); !errors.Is(err, context.Canceled) {
			t.Fatalf("取消请求 = %v", err)
		}
		cancel()
		if data, err := os.ReadFile(output); err != nil || string(data) != "keep" {
			t.Fatalf("取消覆盖目标: %q, %v", data, err)
		}
	}
	output := filepath.Join(t.TempDir(), "stale.dat")
	if _, err := desktop.saveSignatureValueWithPicker(t.Context(), opened.PagePrefix, info.Scope, info.ID, func(string) (string, error) {
		_, err := desktop.document.Open(input, readersession.Options{})
		return output, err
	}); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("过期会话 = %v", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("旧会话写出文件: %v", err)
	}
}

func TestNativeSignatureSaveRejectsPermissionMissingDataAndInvalidOptions(t *testing.T) {
	desktop, opened, _ := desktopWithAttachment(t, false)
	choose := func(string) (string, error) {
		t.Error("非法参数、拒绝或缺失数据不应打开保存对话框")
		return "", nil
	}
	if _, err := desktop.saveSignatureValueWithPicker(t.Context(), opened.PagePrefix, 0, "s1", choose); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("签名值导出权限 = %v", err)
	}
	if _, err := desktop.saveSignatureCertificateWithPicker(t.Context(), opened.PagePrefix, 0, "s1", "outer", "pem", choose); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("证书导出权限 = %v", err)
	}
	for _, options := range []struct {
		scope            int
		id, slot, format string
	}{
		{-1, "s1", "outer", "der"}, {0, "", "outer", "der"}, {0, "s1", "unknown", "der"}, {0, "s1", "outer", "exe"},
	} {
		if _, err := desktop.saveSignatureCertificateWithPicker(t.Context(), opened.PagePrefix, options.scope, options.id, options.slot, options.format, choose); !errors.Is(err, errSignatureSaveOptions) {
			t.Fatalf("非法证书参数 = %v", err)
		}
	}
	desktop, opened, _ = desktopWithAttachment(t, true)
	if _, err := desktop.saveSignatureValueWithPicker(t.Context(), opened.PagePrefix, 0, "missing", choose); err == nil {
		t.Fatal("缺失签名值没有返回错误")
	}
	if _, err := desktop.saveSignatureCertificateWithPicker(t.Context(), opened.PagePrefix, 0, "missing", "outer", "der", choose); err == nil {
		t.Fatal("缺失证书没有返回错误")
	}
}

func TestCertificatePEMLimitAccountsForEncodingExpansion(t *testing.T) {
	der := make([]byte, 48<<20)
	if data, err := signatureCertificateBytes(der, "der"); err != nil || len(data) != len(der) {
		t.Fatalf("DER 原始内容 = %d, %v", len(data), err)
	}
	if data, err := signatureCertificateBytes(der, "pem"); !errors.Is(err, errSignatureSaveSize) || len(data) != 0 {
		t.Fatalf("PEM 膨胀应在编码前拒绝: %d, %v", len(data), err)
	}
}

func TestSignatureSaveSharedFlowRechecksReadAfterPicker(t *testing.T) {
	desktop, opened, _ := desktopWithAttachment(t, true)
	output := filepath.Join(t.TempDir(), "existing.dat")
	if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	reads := 0
	allowed := true
	saved, err := desktop.saveResourceWithPicker(t.Context(), opened.PagePrefix, func() ([]byte, error) {
		reads++
		if !allowed {
			return nil, reader.ErrPermissionDenied
		}
		return []byte("signature"), nil
	}, func() (string, error) { return "signature.dat", nil }, func(string) (string, error) {
		allowed = false
		return output, nil
	})
	if saved || !errors.Is(err, reader.ErrPermissionDenied) || reads != 2 {
		t.Fatalf("对话框后没有复核读取权限: %v, %v, %d", saved, err, reads)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != "keep" {
		t.Fatalf("权限拒绝覆盖目标: %q, %v", data, err)
	}
}
