package session

import (
	"context"
	"errors"

	"github.com/zc310/ofd/pkg/reader"
)

var errSignatureExportSize = errors.New("签名或证书数据为空或超过 64 MiB 导出限制")

// SignatureValue 为宿主保存读取签名值，额外检查所属文档体的导出权限。
func (s *Session) SignatureValue(ctx context.Context, scope int, id string) ([]byte, error) {
	return s.signatureData(ctx, scope, func() ([]byte, error) { return s.doc.SignatureValue(scope, id) })
}

// SignatureCertificate 为宿主保存读取证书 DER，额外检查所属文档体的导出权限。
func (s *Session) SignatureCertificate(ctx context.Context, scope int, id, slot string) ([]byte, error) {
	return s.signatureData(ctx, scope, func() ([]byte, error) { return s.doc.SignatureCertificate(scope, id, slot) })
}

func (s *Session) signatureData(ctx context.Context, scope int, read func() ([]byte, error)) ([]byte, error) {
	return limited(ctx, s, s.pageMetadata, func(_ context.Context) ([]byte, error) {
		if err := s.doc.CheckDocumentPermission(reader.PermissionExport, scope); err != nil {
			return nil, err
		}
		data, err := read()
		if err != nil {
			return nil, err
		}
		if len(data) == 0 || len(data) > 64<<20 {
			return nil, errSignatureExportSize
		}
		return data, nil
	})
}

// SignatureValue 读取指定会话中的签名值，不将二进制数据交给网页。
func (s *Service) SignatureValue(ctx context.Context, prefix string, scope int, id string) ([]byte, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.SignatureValue(ctx, scope, id)
}

// SignatureCertificate 读取指定会话中的证书 DER，不将二进制数据交给网页。
func (s *Service) SignatureCertificate(ctx context.Context, prefix string, scope int, id, slot string) ([]byte, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.SignatureCertificate(ctx, scope, id, slot)
}
