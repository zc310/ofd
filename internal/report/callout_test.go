package report

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"golang.org/x/image/font/sfnt"
)

// 使用 flow 自带字体，不依赖本地中文字体或系统字体索引。
func calloutTestBuilder(ctx context.Context, t *testing.T) *Builder {
	t.Helper()
	opts := flow.DefaultOptions()
	opts.MarginTop, opts.MarginRight, opts.MarginBottom, opts.MarginLeft = 18, 18, 18, 18
	opts.GenerateOutlines = true
	fonts := make(map[string]Font)
	for _, resource := range opts.Fonts {
		face, err := sfnt.Parse(resource.Data)
		if err != nil {
			t.Fatal(err)
		}
		fonts[resource.Name] = Font{Data: resource.Data, Face: face}
	}
	return &Builder{Document: flow.New(opts), ctx: ctx, fonts: fonts}
}

func TestCalloutPreparesLongTitleAndBodyWithinPaddingAcrossPages(t *testing.T) {
	for _, test := range []struct{ name, title string }{{"无标题", ""}, {"有标题", strings.Repeat("W", 200) + "\x00"}} {
		t.Run(test.name, func(t *testing.T) {
			b := calloutTestBuilder(t.Context(), t)
			b.Callout("warning", test.title, strings.Repeat("W", 6000)+"\x00\uffff")
			if b.err != nil {
				t.Fatal(b.err)
			}
			callout := b.Document.Blocks[0].Callout
			if !callout.ShowIcon || !reflect.DeepEqual(callout.AccentColor, &Orange) || !strings.Contains(callout.Text, `\u0000`) || !strings.Contains(callout.Text, `\uFFFF`) {
				t.Fatal("提示框缺少图标、报告配色或可见转义")
			}
			doc, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Pages) < 2 || len(doc.Outlines) != 0 {
				t.Fatalf("分页或大纲异常: pages=%d outlines=%v", len(doc.Pages), doc.Outlines)
			}
			var content strings.Builder
			for _, page := range doc.Pages {
				for _, item := range page.Items {
					text, ok := item.(creator.Text)
					if !ok {
						continue
					}
					if text.X < 20.99 || text.X+text.Width > 189.01 || text.Y < 18 || text.Y+text.Height > 279.01 {
						t.Fatalf("提示框文字超出内边距或版心: %+v", text)
					}
					for _, code := range text.TextCodes {
						content.WriteString(code.Value)
					}
				}
			}
			if got, want := strings.Count(content.String(), "W"), 6000+strings.Count(test.title, "W"); got != want {
				t.Fatalf("跨页丢失文字: got=%d want=%d", got, want)
			}
		})
	}
}

func TestCalloutPreservesPreparationErrorsAndCancellation(t *testing.T) {
	t.Run("取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		b := calloutTestBuilder(ctx, t)
		cancel()
		b.Callout("info", "Title", "Body")
		if _, err := b.Build(); !errors.Is(err, context.Canceled) {
			t.Fatalf("丢失取消错误: %v", err)
		}
	})
	t.Run("字体未注册", func(t *testing.T) {
		b := calloutTestBuilder(t.Context(), t)
		delete(b.fonts, b.Document.Options.BoldFont)
		b.Callout("info", "Title", "Body")
		original := b.err
		b.Callout("success", "Other", "Body")
		if _, err := b.Build(); original == nil || !errors.Is(err, original) || len(b.Document.Blocks) != 0 {
			t.Fatalf("未保留首次文字准备错误: %v", err)
		}
	})
}
