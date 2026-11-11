package session

import (
	"context"
	"errors"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

type signatureDataDocument struct {
	blockingDocument

	read          func() ([]byte, error)
	permissionErr error
	checks        int
}

func (d *signatureDataDocument) CheckDocumentPermission(action reader.PermissionAction, scope int) error {
	if action != reader.PermissionExport || scope != 2 {
		return errors.New("导出权限或作用域错误")
	}
	d.checks++
	return d.permissionErr
}

func (d *signatureDataDocument) SignatureValue(scope int, id string) ([]byte, error) {
	if scope != 2 || id != "s1" {
		return nil, errors.New("签名标识错误")
	}
	return d.read()
}

func (d *signatureDataDocument) SignatureCertificate(scope int, id, slot string) ([]byte, error) {
	if slot != "outer" {
		return nil, errors.New("证书层级错误")
	}
	return d.SignatureValue(scope, id)
}

func TestSignatureExportChecksScopePermissionBeforeReading(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		doc := &signatureDataDocument{permissionErr: reader.ErrPermissionDenied, read: func() ([]byte, error) { t.Error("拒绝导出时仍读取数据"); return nil, nil }}
		session := newSession(doc, 1)
		t.Cleanup(func() { _ = session.Close() })
		read := func(ctx context.Context) ([]byte, error) {
			if certificate {
				return session.SignatureCertificate(ctx, 2, "s1", "outer")
			}
			return session.SignatureValue(ctx, 2, "s1")
		}
		if data, err := read(t.Context()); !errors.Is(err, reader.ErrPermissionDenied) || len(data) != 0 {
			t.Fatalf("权限拒绝 = %q, %v", data, err)
		}
		doc.permissionErr = nil
		doc.read = func() ([]byte, error) { return []byte("raw"), nil }
		if data, err := read(t.Context()); err != nil || string(data) != "raw" {
			t.Fatalf("读取 = %q, %v", data, err)
		}
		if doc.checks != 2 {
			t.Fatalf("权限检查次数 = %d", doc.checks)
		}
		ctx, cancel := context.WithCancel(t.Context())
		doc.read = func() ([]byte, error) { cancel(); return []byte("late"), nil }
		if data, err := read(ctx); !errors.Is(err, context.Canceled) || len(data) != 0 {
			t.Fatalf("取消后泄漏结果: %q, %v", data, err)
		}
		cancel()
		doc.read = func() ([]byte, error) { return nil, nil }
		if _, err := read(t.Context()); !errors.Is(err, errSignatureExportSize) {
			t.Fatalf("空数据 = %v", err)
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := read(t.Context()); !errors.Is(err, ErrClosed) {
			t.Fatalf("关闭后读取 = %v", err)
		}
	}
}

func TestSignatureExportRejectsOversizeData(t *testing.T) {
	doc := &signatureDataDocument{read: func() ([]byte, error) { return make([]byte, (64<<20)+1), nil }}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	if data, err := session.SignatureValue(t.Context(), 2, "s1"); !errors.Is(err, errSignatureExportSize) || len(data) != 0 {
		t.Fatalf("超限数据 = %d, %v", len(data), err)
	}
}

func TestSignatureExportCloseDiscardsInFlightData(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	doc := &signatureDataDocument{read: func() ([]byte, error) { close(started); <-release; return []byte("late"), nil }}
	session := newSession(doc, 1)
	readDone := make(chan error, 1)
	go func() {
		data, err := session.SignatureValue(t.Context(), 2, "s1")
		if len(data) != 0 {
			readDone <- errors.New("关闭后返回了签名数据")
			return
		}
		readDone <- err
	}()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()
	<-session.ctx.Done()
	close(release)
	if err := <-readDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭期间读取 = %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}
