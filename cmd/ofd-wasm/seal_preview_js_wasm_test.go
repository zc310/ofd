//go:build js && wasm

package main

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"syscall/js"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestOFDSealPreviewWorkerReturnsPNGWithoutChangingRawSeal(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "ofdrw", "999.ofd")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少 OFD 印章样本 %s", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	doc, err := reader.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	app := &wasmApp{reader: doc}
	t.Cleanup(func() { _ = app.closeCurrentReader() })
	infos, err := doc.Signatures()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		for index, stamp := range info.Stamps {
			if !stamp.HasSeal || stamp.SealType != "ofd" {
				continue
			}
			for requestID, thumbnail := range []bool{true, false} {
				value, rejected := awaitRenderPromise(t, app.signatureSealPreview(js.Undefined(), []js.Value{js.ValueOf(info.Scope), js.ValueOf(info.ID), js.ValueOf(index), js.ValueOf(thumbnail), js.ValueOf(requestID + 1)}))
				if rejected {
					t.Fatalf("印章渲染失败: %v", value)
				}
				output := make([]byte, value.Length())
				js.CopyBytesToGo(output, value)
				config, err := png.DecodeConfig(bytes.NewReader(output))
				edge := 2048
				if thumbnail {
					edge = 256
				}
				if err != nil || max(config.Width, config.Height) > edge {
					t.Fatalf("Worker OFD 印章 = %v, %v", config, err)
				}
			}
			raw, kind, err := doc.SignatureSeal(info.Scope, info.ID, index)
			if err != nil || kind != "ofd" || !bytes.HasPrefix(raw, []byte("PK")) {
				t.Fatalf("原始印章 API 被改变: %s, %v", kind, err)
			}
			return
		}
	}
	t.Fatal("样本没有 OFD 印章")
}
