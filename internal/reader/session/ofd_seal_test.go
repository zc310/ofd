package session

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func TestOFDSealPreviewAndThumbnailRecheckParentAccess(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "seal", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := &sealPreviewDocument{data: data, declared: "ofd"}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	preview, err := session.SignatureSealPreview(t.Context(), 2, "s1", 3)
	if err != nil || preview.MIME != "image/png" {
		t.Fatalf("OFD 大图 = %v", err)
	}
	big, err := png.DecodeConfig(bytes.NewReader(preview.Data))
	if err != nil {
		t.Fatal(err)
	}
	thumbnail, err := session.SignatureSealThumbnail(t.Context(), 2, "s1", 3)
	if err != nil {
		t.Fatal(err)
	}
	small, err := png.DecodeConfig(bytes.NewReader(thumbnail.Data))
	if err != nil || max(small.Width, small.Height) > 256 || max(big.Width, big.Height) > 2048 || big.Height <= small.Height || doc.checks != 4 {
		t.Fatalf("OFD 尺寸或权限复核 = %v, %v, %v, %d", big, small, err, doc.checks)
	}
	doc.denyOn = doc.checks + 2
	if thumbnail, err := session.SignatureSealThumbnail(t.Context(), 2, "s1", 3); !errors.Is(err, reader.ErrPermissionDenied) || len(thumbnail.Data) != 0 {
		t.Fatalf("渲染后父文档拒绝 = %v", err)
	}
}

func TestSignedOFDSealFixtureRendersIndependently(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "ofdrw", "999.ofd")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少 OFD 印章样本 %s", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.current(opened.PagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	infos, err := session.doc.Signatures()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, info := range infos {
		for index, stamp := range info.Stamps {
			if !stamp.HasSeal || stamp.SealType != "ofd" {
				continue
			}
			thumbnail, err := service.SignatureSealThumbnail(t.Context(), opened.PagePrefix, info.Scope, info.ID, index)
			if err != nil {
				t.Fatal(err)
			}
			config, err := png.DecodeConfig(bytes.NewReader(thumbnail.Data))
			if err != nil || max(config.Width, config.Height) > 256 {
				t.Fatalf("真实 OFD 印章 = %v, %v", config, err)
			}
			count++
		}
	}
	if count == 0 {
		t.Fatal("样本没有 OFD 印章")
	}
}
