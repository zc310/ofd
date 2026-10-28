package converter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/backends/canvasfont"
)

type lifecycleFontEngine struct {
	render.FontEngine

	document           *parser.Document
	closes             int
	packageClosedEarly bool
}

func (f *lifecycleFontEngine) Close() {
	f.closes++
	reader, err := f.document.FileCache.Open("OFD.xml")
	if err != nil {
		f.packageClosedEarly = true
	} else {
		_ = reader.Close()
	}
	f.FontEngine.Close()
}

func trackLifecycleFontEngines(t *testing.T, onCreate func(int)) *[]*lifecycleFontEngine {
	t.Helper()
	var engines []*lifecycleFontEngine
	render.RegisterFontEngineFactory(func(document *parser.Document) render.FontEngine {
		engine := &lifecycleFontEngine{FontEngine: canvasfont.NewFonts(document), document: document}
		engines = append(engines, engine)
		if onCreate != nil {
			onCreate(len(engines))
		}
		return engine
	})
	t.Cleanup(func() {
		render.RegisterFontEngineFactory(func(document *parser.Document) render.FontEngine {
			return canvasfont.NewFonts(document)
		})
	})
	return &engines
}

func lifecycleOFD(t *testing.T, withPages bool) []byte {
	t.Helper()
	pages := `<Pages/>`
	if withPages {
		pages = `<Pages><Page ID="1" BaseLoc="Page.xml"/></Pages>`
	}
	document := `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 10 10</PhysicalBox></PageArea></CommonData>` + pages + `</Document>`
	entries := map[string]string{
		"OFD.xml":            `<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody><DocBody><DocRoot>Doc_1/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml": document,
		"Doc_1/Document.xml": document,
		"Doc_0/Page.xml":     `<Page xmlns="http://www.ofdspec.org/2016"><Content/></Page>`,
		"Doc_1/Page.xml":     `<Page xmlns="http://www.ofdspec.org/2016"><Content/></Page>`,
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, value := range entries {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestEncodeOFDClosesOwnedDocumentsOnEveryExit(t *testing.T) {
	encodeFailure := errors.New("编码失败")
	for _, test := range []struct {
		name         string
		cancelAfter  int
		withoutPages bool
		encodeError  error
		wantError    error
		wantCreated  int
		wantEncoded  bool
	}{
		{name: "success", wantCreated: 2, wantEncoded: true},
		{name: "encode_failure", encodeError: encodeFailure, wantError: encodeFailure, wantCreated: 2, wantEncoded: true},
		{name: "cancel_during_construction", cancelAfter: 1, wantError: context.Canceled, wantCreated: 1},
		{name: "cancel_after_parsing", cancelAfter: 2, wantError: context.Canceled, wantCreated: 2},
		{name: "without_pages", withoutPages: true, wantCreated: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			engines := trackLifecycleFontEngines(t, func(created int) {
				if created == test.cancelAfter {
					cancel()
				}
			})
			encoded := false
			err := encodeOFD(lifecycleOFD(t, !test.withoutPages), io.Discard, newConverter(ctx), func(documents []*render.Document, _ io.Writer, _ *Converter) error {
				encoded = true
				if len(documents) != 2 {
					t.Fatalf("编码收到 %d 个文档，期望 2 个", len(documents))
				}
				for _, engine := range *engines {
					if engine.closes != 0 {
						t.Error("编码结束前字体引擎已被关闭")
					}
				}
				return test.encodeError
			})
			if test.withoutPages {
				if err == nil {
					t.Fatal("无页面文档应返回错误")
				}
			} else if err != test.wantError {
				t.Fatalf("错误 = %v，期望原样返回 %v", err, test.wantError)
			}
			if encoded != test.wantEncoded || len(*engines) != test.wantCreated {
				t.Fatalf("编码执行 = %v，创建引擎数 = %d；期望 %v、%d", encoded, len(*engines), test.wantEncoded, test.wantCreated)
			}
			for index, engine := range *engines {
				if engine.closes != 1 {
					t.Errorf("字体引擎 %d 关闭了 %d 次，期望 1 次", index, engine.closes)
				}
				if engine.packageClosedEarly {
					t.Errorf("字体引擎 %d 关闭前 ZIP 包已被关闭", index)
				}
				if reader, err := engine.document.FileCache.Open("OFD.xml"); !errors.Is(err, container.ErrPackageClosed) {
					if reader != nil {
						_ = reader.Close()
					}
					t.Errorf("转换结束后 ZIP 包未关闭: %v", err)
				}
			}
		})
	}
}

func TestEncodeDocumentsLeavesCallerResourcesOpen(t *testing.T) {
	engines := trackLifecycleFontEngines(t, nil)
	ofd, err := parser.NewOFD(lifecycleOFD(t, true))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	var documents []*render.Document
	for _, parsed := range ofd.Documents {
		document := render.NewDocument(nil, parsed)
		defer document.Close()
		documents = append(documents, document)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := EncodeDocuments(context.Background(), "text", documents, io.Discard); err != nil {
			t.Fatal(err)
		}
		for _, engine := range *engines {
			if engine.closes != 0 {
				t.Fatal("调用方提供的渲染文档被转换器关闭")
			}
		}
	}
}
