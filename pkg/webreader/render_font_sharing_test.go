package webreader

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestPageViewsReuseEmbeddedFontsAcrossDPIAndCacheEviction(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofdrw", "intro.ofd"))
	if err != nil {
		t.Skipf("缺少内嵌字体样例：%v", err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	const page = 8
	ref := reader.pages[page]
	if _, err := reader.RenderPage(t.Context(), page, RenderOptions{DPI: 96, PNGCompressionLevel: 1}); err != nil {
		t.Fatal(err)
	}
	var loaded []models.StRefID
	ref.document.ForEachFont(func(id models.StID, font *models.Font) bool {
		if ref.document.HasLoadedEmbeddedFont(models.StRefID(id)) {
			loaded = append(loaded, models.StRefID(id))
		}
		return true
	})
	if len(loaded) == 0 {
		t.Fatal("正文渲染后基础文档没有共享的内嵌字体")
	}
	// 创建视图而不绘制，应立即看到同一份已解析字体；改变 DPI 触发视图淘汰。
	for i := 0; i < maxRenderDocs+2; i++ {
		view, err := reader.pageDocument(ref, color.Transparent, geom.DPI(float64(30+i)))
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range loaded {
			if !view.HasLoadedEmbeddedFont(id) || !ref.document.HasLoadedEmbeddedFont(id) {
				t.Fatalf("DPI 视图创建/淘汰后字体 %d 未被复用", id)
			}
		}
		reader.releasePageDocument(view)
	}
	if _, err := reader.Text(page); err != nil {
		t.Fatalf("视图淘汰后文字测量失败：%v", err)
	}
}
