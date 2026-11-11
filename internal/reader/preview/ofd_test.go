package preview

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image/png"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func TestOFDSealRendersFirstPageWithTransparencyAndBoundedDimensions(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "preview", Pages: []creator.Page{
		{Items: []creator.Item{creator.Path{X: 10, Y: 10, Width: 50, Height: 20, Data: "M 0 0 L 50 0 L 50 20 L 0 20 C", Fill: true, FillColor: &creator.Color{R: 220, G: 10, B: 10}}}},
		{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range []int{256, 2048} {
		output, err := OFDSeal(t.Context(), data, edge)
		if err != nil {
			t.Fatal(err)
		}
		image, err := png.Decode(bytes.NewReader(output))
		if err != nil {
			t.Fatal(err)
		}
		bounds := image.Bounds()
		if max(bounds.Dx(), bounds.Dy()) > edge+1 || max(bounds.Dx(), bounds.Dy()) < edge-1 {
			t.Fatalf("OFD 印章尺寸 = %v, 上限 %d", bounds, edge)
		}
		_, _, _, background := image.At(bounds.Max.X-1, bounds.Max.Y-1).RGBA()
		_, _, _, ink := image.At(bounds.Dx()*20/210, bounds.Dy()*20/297).RGBA()
		if background != 0 || ink == 0 {
			t.Fatalf("背景或第一页绘制错误: 背景 %d，墨迹 %d", background, ink)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if output, err := OFDSeal(ctx, data, 256); !errors.Is(err, context.Canceled) || output != nil {
		t.Fatalf("取消返回输出: %v", err)
	}
	if _, err := OFDSeal(t.Context(), data, 512); !errors.Is(err, ErrSealLimit) {
		t.Fatalf("任意缩放尺寸未拒绝: %v", err)
	}
}

func TestOFDSealRejectsOversizedAndExpandedArchives(t *testing.T) {
	if _, err := OFDSeal(t.Context(), make([]byte, (8<<20)+1), 256); !errors.Is(err, ErrSealLimit) {
		t.Fatalf("输入限制 = %v", err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	entry, err := archive.Create("oversized.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(strings.Repeat("x", (8<<20)+1))); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OFDSeal(t.Context(), output.Bytes(), 256); !errors.Is(err, ErrSealLimit) {
		t.Fatalf("解压条目限制 = %v", err)
	}
	if _, err := OFDSeal(t.Context(), []byte("not an OFD"), 256); err == nil {
		t.Fatal("损坏 OFD 未拒绝")
	}
}
