package reader

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/backends/canvasfont"
)

type openTrackingFontEngine struct {
	render.FontEngine
	closes int
}

func (f *openTrackingFontEngine) Close() {
	f.closes++
	f.FontEngine.Close()
}

func emptyDocBodiesPackage(t *testing.T, hasPage bool) []byte {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entries := map[string]string{
		"OFD.xml": `<OFD Version="1.0"><DocBody><DocRoot>A/Document.xml</DocRoot></DocBody><DocBody><DocRoot>B/Document.xml</DocRoot></DocBody><DocBody><DocRoot>C/Document.xml</DocRoot></DocBody></OFD>`,
	}
	for _, dir := range []string{"A", "B", "C"} {
		pages := ""
		if dir == "B" && hasPage {
			pages = `<Page ID="1" BaseLoc="Page.xml"/>`
			entries[dir+"/Page.xml"] = `<Page><Content><Layer ID="2"/></Content></Page>`
		}
		entries[dir+"/Document.xml"] = fmt.Sprintf(`<Document><CommonData><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea></CommonData><Pages>%s</Pages></Document>`, pages)
	}
	for name, content := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestEmptyDocBodiesDoNotLeaveUnclosedRenderDocuments(t *testing.T) {
	// 本包不并行运行测试；结束时恢复由 canvasfont 注册的默认工厂。
	t.Cleanup(func() {
		render.RegisterFontEngineFactory(func(doc *parser.Document) render.FontEngine {
			return canvasfont.NewFonts(doc)
		})
	})
	for _, hasPage := range []bool{false, true} {
		t.Run(fmt.Sprintf("hasPage=%v", hasPage), func(t *testing.T) {
			var engines []*openTrackingFontEngine
			render.RegisterFontEngineFactory(func(doc *parser.Document) render.FontEngine {
				engine := &openTrackingFontEngine{FontEngine: canvasfont.NewFonts(doc)}
				engines = append(engines, engine)
				return engine
			})
			t.Cleanup(func() {
				for _, engine := range engines {
					if engine.closes == 0 {
						engine.FontEngine.Close()
					}
				}
			})
			r, err := Open(emptyDocBodiesPackage(t, hasPage))
			if !hasPage {
				if r != nil || err == nil || !strings.Contains(err.Error(), "没有页面") {
					t.Fatalf("全空文档体应返回无页错误：reader=%v err=%v", r, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				if r.PageCount() != 1 || r.pages[0].fontScope != 1 {
					t.Fatalf("跳过空文档体后页数或原作用域错误：%+v", r.pages)
				}
				if err := r.SelectVersion(1, ""); err != nil {
					t.Fatal(err)
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for i, engine := range engines {
				if engine.closes != 1 {
					t.Errorf("字体引擎 %d 关闭次数=%d，期望 1", i, engine.closes)
				}
			}
		})
	}
}
