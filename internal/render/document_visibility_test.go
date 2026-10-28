package render

import (
	"image/color"
	"reflect"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

// 同一页的画面、搜索文字、字符布局和链接必须使用一致的可见性设置，
// 且单次过滤不能修改缓存中的页面或影响其他页面的模板引用。
func TestPageVisibilityKeepsRenderingTextAndLinksConsistent(t *testing.T) {
	items := func(x float64, text string, fill creator.Color) []creator.Item {
		return []creator.Item{
			creator.Path{X: x, Width: 10, Height: 10, Data: "M 0 0 L 10 0 L 10 10 L 0 10 C", Fill: true, FillColor: &fill},
			creator.PageBlock{Items: []creator.Item{creator.Text{
				X: x, Width: 10, Height: 4, Size: 3, Value: text,
				Actions: []creator.Action{{Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://example.com/" + text}}},
			}}},
		}
	}
	const backgroundID, foregroundID = 1000, 1001
	pageInput := creator.Page{
		Templates: []creator.TemplateRef{{ID: backgroundID}, {ID: foregroundID}},
		Layers: []creator.Layer{
			{Items: items(20, "body", creator.Color{B: 255})},
			{Items: items(30, "last", creator.Color{R: 255, B: 255})},
		},
	}
	data, err := creator.Marshal(creator.Document{
		ID: "visibility", PageSize: creator.PageSize{Width: 40, Height: 10},
		Templates: []creator.TemplatePage{
			{ID: backgroundID, ZOrder: "Background", Items: items(0, "background", creator.Color{R: 255})},
			{ID: foregroundID, ZOrder: "Foreground", Items: items(10, "foreground", creator.Color{G: 255})},
		},
		Pages: []creator.Page{pageInput, pageInput},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{
			ID: 2000, Type: "Link", Creator: "visibility-test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Boundary: &creator.Box{X: 1, Y: 1, Width: 2, Height: 2},
			Items: []creator.Item{creator.Path{Width: 2, Height: 2, Data: "M 0 0 L 2 0", Actions: []creator.Action{{
				Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://example.com/annotation"},
			}}}},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	// 固定字体度量，避免测试依赖运行机器上的系统字体。
	doc := &Document{Document: ofd.Documents[0], background: color.White, fonts: metricsFontEngine{face: metricsFontFace{ascent: 3}}}
	t.Cleanup(doc.Close)
	page := doc.Pages[0]
	var bodyID, lastID models.StID
	if err := page.WithPageContent(func(content *models.PageContent) error {
		bodyID, lastID = content.Content.Layer[0].ID, content.Content.Layer[1].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		visibility PageVisibility
		values     []string
		links      []string
		visible    [4]bool
	}{
		{"all", PageVisibility{}, []string{"background", "body", "last", "foreground"}, []string{"body", "last", "background", "foreground", "annotation"}, [4]bool{true, true, true, true}},
		{"hide body layer", PageVisibility{HiddenLayers: map[models.StID]bool{bodyID: true}}, []string{"background", "last", "foreground"}, []string{"last", "background", "foreground", "annotation"}, [4]bool{true, true, false, true}},
		{"hide background", PageVisibility{HiddenTemplates: map[models.StID]bool{backgroundID: true}}, []string{"body", "last", "foreground"}, []string{"body", "last", "foreground", "annotation"}, [4]bool{false, true, true, true}},
		{"hide foreground", PageVisibility{HiddenTemplates: map[models.StID]bool{foregroundID: true}}, []string{"background", "body", "last"}, []string{"body", "last", "background", "annotation"}, [4]bool{true, false, true, true}},
		{"false and unrelated IDs", PageVisibility{HiddenLayers: map[models.StID]bool{bodyID: false, backgroundID: true}, HiddenTemplates: map[models.StID]bool{foregroundID: false, lastID: true}}, []string{"background", "body", "last", "foreground"}, []string{"body", "last", "background", "foreground", "annotation"}, [4]bool{true, true, true, true}},
		{"hide all content", PageVisibility{HiddenLayers: map[models.StID]bool{bodyID: true, lastID: true}, HiddenTemplates: map[models.StID]bool{backgroundID: true, foregroundID: true}}, []string{}, []string{"annotation"}, [4]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := doc.TextValuesWithVisibility(page, tc.visibility); !reflect.DeepEqual(got, tc.values) {
				t.Fatalf("TextValues = %v, want %v", got, tc.values)
			}
			layouts := doc.TextLayoutsWithVisibility(page, tc.visibility)
			texts := make([]string, len(layouts))
			for i, layout := range layouts {
				texts[i] = layout.Text
			}
			if !reflect.DeepEqual(texts, tc.values) {
				t.Fatalf("TextLayouts = %v, want %v", texts, tc.values)
			}
			if len(layouts) > 0 {
				// 请求顺序、重复和非法索引不影响输出顺序；索引以过滤后的文字为准。
				last := len(layouts) - 1
				got := doc.TextLayoutsForRunsWithVisibility(page, []int{last, -1, 0, last, 99}, tc.visibility)
				want := []TextLayout{layouts[0]}
				if last != 0 {
					want = append(want, layouts[last])
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("selected layouts = %+v, want %+v", got, want)
				}
			}
			links := doc.PageLinksWithVisibility(page, nil, tc.visibility)
			uris := make([]string, len(links))
			for i, link := range links {
				uris[i] = link.URI
			}
			wantURIs := make([]string, len(tc.links))
			for i, name := range tc.links {
				wantURIs[i] = "https://example.com/" + name
			}
			if !reflect.DeepEqual(uris, wantURIs) {
				t.Fatalf("links = %v, want %v", uris, wantURIs)
			}
			surface, err := doc.PageWithVisibility(page, tc.visibility)
			if err != nil {
				t.Fatal(err)
			}
			img := surface.Rasterize(geom.DPI(25.4)) // 1 px/mm，采样点远离文字和边缘。
			colors := []color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, B: 255, A: 255}}
			for i, visible := range tc.visible {
				want := colors[i]
				if !visible {
					want = color.RGBA{R: 255, G: 255, B: 255, A: 255}
				}
				if got := img.RGBAAt(i*10+5, 7); got != want {
					t.Errorf("region %d pixel = %v, want %v", i, got, want)
				}
			}
		})
	}
	wantValues := []string{"background", "body", "last", "foreground"}
	for _, page := range doc.Pages {
		if got := doc.TextValues(page); !reflect.DeepEqual(got, wantValues) {
			t.Errorf("filter changed shared content: TextValues = %v", got)
		}
	}
}
