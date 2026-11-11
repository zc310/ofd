package main

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/reader"
)

func TestNativeSealPreviewMatchesSignedDocumentAndRejectsOldSession(t *testing.T) {
	desktop, opened, info, input := desktopWithSignatureFixture(t, "zsbk.ofd")
	doc, err := reader.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	found := false
	for index, stamp := range info.Stamps {
		if !stamp.HasSeal || (stamp.SealType != "png" && stamp.SealType != "jpg" && stamp.SealType != "jpeg") {
			continue
		}
		found = true
		want, _, err := doc.SignatureSeal(info.Scope, info.ID, index)
		if err != nil {
			t.Fatal(err)
		}
		preview, err := desktop.SignatureSealPreview(t.Context(), opened.PagePrefix, info.Scope, info.ID, index)
		if err != nil || !bytes.Equal(preview.Data, want) || (preview.MIME != "image/png" && preview.MIME != "image/jpeg") {
			t.Fatalf("样本印章预览 = %s, %v", preview.MIME, err)
		}
		thumbnail, err := desktop.SignatureSealThumbnail(t.Context(), opened.PagePrefix, info.Scope, info.ID, index)
		if err != nil {
			t.Fatal(err)
		}
		config, err := png.DecodeConfig(bytes.NewReader(thumbnail.Data))
		if err != nil || config.Width > 256 || config.Height > 256 || thumbnail.Width < config.Width || thumbnail.Height < config.Height {
			t.Fatalf("原生印章缩略图 = %v, %v", config, err)
		}
	}
	if !found {
		t.Fatal("签名样本没有 PNG/JPEG 印章")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := desktop.SignatureSealPreview(ctx, opened.PagePrefix, info.Scope, info.ID, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消预览 = %v", err)
	}
	if _, err := desktop.SignatureSealPreview(t.Context(), opened.PagePrefix, info.Scope, info.ID, len(info.Stamps)); err == nil {
		t.Fatal("无效签章索引没有返回错误")
	}
	if _, err := desktop.SignatureSealPreview(t.Context(), opened.PagePrefix, info.Scope, "missing", 0); err == nil {
		t.Fatal("缺失签名没有返回错误")
	}
	if _, err := desktop.openPath(input); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.SignatureSealPreview(t.Context(), opened.PagePrefix, info.Scope, info.ID, 0); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("过期会话预览 = %v", err)
	}
	if _, err := desktop.SignatureSealThumbnail(t.Context(), opened.PagePrefix, info.Scope, info.ID, 0); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧缩略图会话 = %v", err)
	}
}

func TestNativeSealPreviewValidatesOptionsBeforeServiceAccess(t *testing.T) {
	desktop := &Desktop{}
	for _, test := range []struct {
		scope int
		id    string
		index int
	}{{-1, "s1", 0}, {0, " ", 0}, {0, "s1", -1}} {
		if _, err := desktop.SignatureSealPreview(t.Context(), "", test.scope, test.id, test.index); !errors.Is(err, errSealPreviewOptions) {
			t.Fatalf("预览参数 = %v", err)
		}
		if _, err := desktop.SignatureSealThumbnail(t.Context(), "", test.scope, test.id, test.index); !errors.Is(err, errThumbnailOptions) {
			t.Fatalf("缩略图参数 = %v", err)
		}
	}
}
