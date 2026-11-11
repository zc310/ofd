package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/zc310/ofd/internal/reader/preview"
	"github.com/zc310/ofd/pkg/reader"
)

type sealPreviewDocument struct {
	blockingDocument

	data                  []byte
	err                   error
	declared              string
	checks, reads, denyOn int
	afterRead             func()
}

func (d *sealPreviewDocument) CheckDocumentPermission(action reader.PermissionAction, scope int) error {
	if action != reader.PermissionAccess || scope != 2 {
		return errors.New("预览必须检查所属文档体访问权限，而非导出权限")
	}
	d.checks++
	if d.denyOn > 0 && d.checks >= d.denyOn {
		return reader.ErrPermissionDenied
	}
	return nil
}

func (d *sealPreviewDocument) SignatureSeal(scope int, id string, index int) ([]byte, string, error) {
	if scope != 2 || id != "s1" || index != 3 {
		return nil, "", errors.New("印章标识或索引错误")
	}
	d.reads++
	if d.afterRead != nil {
		d.afterRead()
	}
	return d.data, d.declared, d.err
}

func sealPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestSealPreviewUsesActualImageContentAndRechecksAccess(t *testing.T) {
	doc := &sealPreviewDocument{data: sealPNG(t), declared: "svg"}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	preview, err := session.SignatureSealPreview(t.Context(), 2, "s1", 3)
	if err != nil || preview.MIME != "image/png" || !bytes.Equal(preview.Data, doc.data) || doc.checks != 2 {
		t.Fatalf("PNG 预览 = %s, %v, 检查 %d 次", preview.MIME, err, doc.checks)
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	doc.data, doc.declared = jpg.Bytes(), "png"
	preview, err = session.SignatureSealPreview(t.Context(), 2, "s1", 3)
	if err != nil || preview.MIME != "image/jpeg" {
		t.Fatalf("JPEG 预览 = %s, %v", preview.MIME, err)
	}
	doc.denyOn = doc.checks + 1
	reads := doc.reads
	if preview, err := session.SignatureSealPreview(t.Context(), 2, "s1", 3); !errors.Is(err, reader.ErrPermissionDenied) || len(preview.Data) != 0 || doc.reads != reads {
		t.Fatalf("读取前拒绝 = %v", err)
	}
	doc.denyOn = doc.checks + 2
	if preview, err := session.SignatureSealPreview(t.Context(), 2, "s1", 3); !errors.Is(err, reader.ErrPermissionDenied) || len(preview.Data) != 0 {
		t.Fatalf("读取后权限过期 = %v", err)
	}
}

func TestSealPreviewRejectsUnsafeOversizeAndMissingImages(t *testing.T) {
	large := sealPNG(t)
	binary.BigEndian.PutUint32(large[16:20], 5001)
	binary.BigEndian.PutUint32(large[20:24], 5000)
	binary.BigEndian.PutUint32(large[29:33], crc32.ChecksumIEEE(large[12:29]))
	missing := errors.New("印章不存在")
	for _, test := range []struct {
		name          string
		data          []byte
		readErr, want error
	}{
		{"SVG 伪装 PNG", []byte(`<svg onload="alert(1)"/>`), nil, preview.ErrImageFormat},
		{"OFD", []byte("PK\x03\x04"), nil, preview.ErrImageFormat},
		{"空数据", nil, nil, preview.ErrImageFormat},
		{"损坏图片", []byte("\x89PNG\r\n\x1a\n"), nil, preview.ErrImageFormat},
		{"文件过大", make([]byte, (8<<20)+1), nil, preview.ErrImageSize},
		{"像素过多", large, nil, preview.ErrImageDimensions},
		{"数据缺失", nil, missing, missing},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := &sealPreviewDocument{data: test.data, err: test.readErr, declared: "png"}
			session := newSession(doc, 1)
			t.Cleanup(func() { _ = session.Close() })
			if preview, err := session.SignatureSealPreview(t.Context(), 2, "s1", 3); !errors.Is(err, test.want) || len(preview.Data) != 0 {
				t.Fatalf("无效印章预览 = %+v, %v", preview, err)
			}
		})
	}
}

func TestSealPreviewDiscardsCancellationAndClosedSessionResults(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	doc := &sealPreviewDocument{data: sealPNG(t), afterRead: cancel}
	session := newSession(doc, 1)
	if preview, err := session.SignatureSealPreview(ctx, 2, "s1", 3); !errors.Is(err, context.Canceled) || len(preview.Data) != 0 {
		t.Fatalf("取消后预览 = %+v, %v", preview, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.SignatureSealPreview(t.Context(), 2, "s1", 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后读取 = %v", err)
	}
}

func TestSealPreviewCloseWaitsForNativeReadAndDiscardsResult(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	doc := &sealPreviewDocument{data: sealPNG(t), afterRead: func() { close(started); <-release }}
	session := newSession(doc, 1)
	readDone := make(chan error, 1)
	go func() {
		preview, err := session.SignatureSealPreview(t.Context(), 2, "s1", 3)
		if len(preview.Data) != 0 {
			readDone <- errors.New("关闭后泄漏印章图片")
			return
		}
		readDone <- err
	}()
	<-started
	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close() }()
	<-session.ctx.Done()
	select {
	case err := <-closeDone:
		t.Fatalf("底层读取未退出就完成关闭: %v", err)
	default:
	}
	close(release)
	if err := <-readDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭期间预览 = %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
}
