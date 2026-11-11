package main

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

var errSignatureSaveOptions = errors.New("签名导出参数无效：需要有效文档体、签名标识、seal/outer 证书层级及 der/pem 格式")
var errSignatureSaveSize = errors.New("签名或证书输出超过 64 MiB 限制")

// SaveSignatureValue 通过原生对话框保存原始签名值，不自动打开文件。
func (d *Desktop) SaveSignatureValue(ctx context.Context, prefix string, scope int, id string) (bool, error) {
	return d.saveSignatureValueWithPicker(ctx, prefix, scope, id, d.savePicker(ctx, "导出签名或证书"))
}

// SaveSignatureCertificate 通过原生对话框保存指定层级的 DER 或 PEM 证书。
func (d *Desktop) SaveSignatureCertificate(ctx context.Context, prefix string, scope int, id, slot, format string) (bool, error) {
	return d.saveSignatureCertificateWithPicker(ctx, prefix, scope, id, slot, format, d.savePicker(ctx, "导出签名或证书"))
}

func (d *Desktop) saveSignatureValueWithPicker(ctx context.Context, prefix string, scope int, id string, choose func(string) (string, error)) (bool, error) {
	if scope < 0 || strings.TrimSpace(id) == "" {
		return false, errSignatureSaveOptions
	}
	return d.saveResourceWithPicker(ctx, prefix, func() ([]byte, error) {
		return d.document.SignatureValue(ctx, prefix, scope, id)
	}, func() (string, error) {
		return fmt.Sprintf("signature-%d-%s.dat", scope+1, safeAttachmentFilename(id)), nil
	}, choose)
}

func (d *Desktop) saveSignatureCertificateWithPicker(ctx context.Context, prefix string, scope int, id, slot, format string, choose func(string) (string, error)) (bool, error) {
	if scope < 0 || strings.TrimSpace(id) == "" || (slot != "seal" && slot != "outer") || (format != "der" && format != "pem") {
		return false, errSignatureSaveOptions
	}
	return d.saveResourceWithPicker(ctx, prefix, func() ([]byte, error) {
		der, err := d.document.SignatureCertificate(ctx, prefix, scope, id, slot)
		if err != nil {
			return nil, err
		}
		return signatureCertificateBytes(der, format)
	}, func() (string, error) {
		return fmt.Sprintf("certificate-%d-%s-%s.%s", scope+1, safeAttachmentFilename(id), slot, format), nil
	}, choose)
}

func signatureCertificateBytes(der []byte, format string) ([]byte, error) {
	if len(der) > 64<<20 {
		return nil, errSignatureSaveSize
	}
	if format == "der" {
		return der, nil
	}
	// PEM 的 base64 和换行会膨胀，编码前限制最终长度，避免额外大分配。
	encoded := ((len(der) + 2) / 3) * 4
	if encoded+(encoded+63)/64+len("-----BEGIN CERTIFICATE-----\n-----END CERTIFICATE-----\n") > 64<<20 {
		return nil, errSignatureSaveSize
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}
