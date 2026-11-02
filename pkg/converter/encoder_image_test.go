package converter

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func TestParsedImageInputRequiresOutputLikeFileInput(t *testing.T) {
	doc := render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}}, 0)
	defer doc.Close()
	if err := Image(ctxTODO, doc); err == nil {
		t.Fatal("已解析文档不应绕过公共 Image 入口的输出参数检查")
	}
}
