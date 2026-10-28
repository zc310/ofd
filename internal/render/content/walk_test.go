package content

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
)

type testResources struct {
	params     map[models.StID]*models.DrawParam
	composites map[models.StRefID]Composite
	templates  map[models.StID]*models.PageContent
	loads      []models.StID
	err        error
	resolve    func(models.CompositeObject, *models.DrawParam, int) (Composite, bool)
	textClip   *geom.Path
}

func (s *testResources) GetDrawParam(id models.StID) *models.DrawParam { return s.params[id] }
func (s *testResources) ResolveComposite(object models.CompositeObject, dp *models.DrawParam, depth int) (Composite, bool) {
	if s.resolve != nil {
		return s.resolve(object, dp, depth)
	}
	unit, ok := s.composites[object.ResourceID]
	return unit, ok
}
func (s *testResources) TextClip(models.CTGraphicUnit, float64) *geom.Path      { return s.textClip }
func (*testResources) CompositeClip(models.CompositeObject, float64) *geom.Path { return nil }
func (s *testResources) LoadTemplate(id models.StID) (*models.PageContent, error) {
	s.loads = append(s.loads, id)
	return s.templates[id], s.err
}
func (*testResources) TemplateForeground(ref models.Template) bool { return ref.ZOrder == "Foreground" }

func textItem(id models.StID) models.PageItem {
	return models.PageItem{Kind: models.PageItemText, Text: &models.TextObject{ID: id}}
}

func layer(id models.StID, kind string, items ...models.PageItem) *models.Layer {
	return &models.Layer{ID: id, Type: kind, CTPageBlock: models.CTPageBlock{Items: items}}
}

func annotation(kind models.AnnotType, id models.StID) *models.Annot {
	return &models.Annot{Type: kind, Appearance: &models.Appearance{
		Boundary:    &models.StBox{X: 5, Y: 6, Width: 10, Height: 10},
		CTPageBlock: models.CTPageBlock{Items: []models.PageItem{textItem(id)}},
	}}
}

func TestWalkPagePreservesOrderVisibilityAndScope(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(map[bool]string{false: "正文模板", true: "全部来源"}[extra], func(t *testing.T) {
			backgroundDP, bodyDP := &models.DrawParam{}, &models.DrawParam{}
			scope := &testResources{params: map[models.StID]*models.DrawParam{20: backgroundDP, 21: bodyDP}, templates: map[models.StID]*models.PageContent{
				10: {Content: &models.Content{Layer: []*models.Layer{layer(1, "Body", textItem(1))}}},
				11: {Content: &models.Content{Layer: []*models.Layer{layer(1, "Body", textItem(6))}}},
			}}
			layers := []*models.Layer{layer(4, "Body", textItem(4)), nil, layer(3, "Background", textItem(3)), layer(5, "Body", textItem(5))}
			layers[0].DrawParam, layers[2].DrawParam = 21, 20
			page := &models.PageContent{
				Template: []models.Template{{TemplateID: 10}, {TemplateID: 11, ZOrder: "Foreground"}, {TemplateID: 12}},
				Content:  &models.Content{Layer: layers},
			}
			annotations := &models.PageAnnot{Annots: []*models.Annot{nil, annotation(models.AnnotTypeStamp, 7), annotation(models.AnnotTypeWatermark, 2)}}
			childScope := &testResources{}
			var ids []models.StID
			var sources []Source
			seals := 0
			err := WalkPage(Frame[*testResources]{Scope: scope, Matrix: geom.Identity, Source: SourcePage}, page, annotations,
				Visibility{HiddenLayers: map[models.StID]bool{5: true}, HiddenTemplates: map[models.StID]bool{12: true}},
				Options{IncludeAnnotations: extra, IncludeWatermarks: extra, IncludeSeals: extra},
				func(frame Frame[*testResources], visit Visitor[*testResources]) error {
					seals++
					frame.Scope = childScope
					visit(frame.Place(geom.Identity.Translate(30, 40), SourceSeal, nil), textItem(8))
					return nil
				}, func(frame Frame[*testResources], item models.PageItem) {
					id := ItemID(item)
					ids, sources = append(ids, id), append(sources, frame.Source)
					if (id == 3 && frame.DrawParam != backgroundDP) || (id == 4 && frame.DrawParam != bodyDP) || (id == 6 && frame.DrawParam != nil) {
						t.Error("图层绘制参数未继承或污染了后续模板")
					}
					if id == 8 && (frame.Scope != childScope || frame.Matrix.Dot(geom.Point{}) != (geom.Point{X: 30, Y: 40})) {
						t.Error("印章没有保留子文档资源作用域或页面映射")
					}
					if (id == 2 || id == 7) && frame.Matrix.Dot(geom.Point{}) != (geom.Point{X: 5, Y: 6}) {
						t.Error("批注外观没有平移到页面坐标")
					}
				})
			if err != nil {
				t.Fatal(err)
			}
			wantIDs := []models.StID{1, 3, 4, 6}
			wantSources := []Source{SourceTemplate, SourcePage, SourcePage, SourceTemplate}
			if extra {
				wantIDs = []models.StID{1, 2, 3, 4, 6, 8, 7}
				wantSources = []Source{SourceTemplate, SourceWatermark, SourcePage, SourcePage, SourceTemplate, SourceSeal, SourceAnnotation}
			}
			if !reflect.DeepEqual(ids, wantIDs) || !reflect.DeepEqual(sources, wantSources) || (extra && seals != 1) || (!extra && seals != 0) {
				t.Fatalf("遍历顺序或来源错误：ids=%v sources=%v seals=%d", ids, sources, seals)
			}
			if !reflect.DeepEqual(scope.loads, []models.StID{10, 11}) || len(page.Content.Layer) != 4 || page.Content.Layer[0].ID != 4 {
				t.Fatal("加载了隐藏模板或修改了原图层顺序")
			}
		})
	}
}

func TestWalkPagePreservesTemplateAndSealErrors(t *testing.T) {
	want := errors.New("resource unavailable")
	scope := &testResources{err: want}
	frame := Frame[*testResources]{Scope: scope, Matrix: geom.Identity}
	page := &models.PageContent{Template: []models.Template{{TemplateID: 10}}}
	if err := WalkPage(frame, page, nil, Visibility{}, Options{}, nil, func(Frame[*testResources], models.PageItem) {}); !errors.Is(err, want) {
		t.Fatalf("模板加载错误丢失：%v", err)
	}
	if err := WalkPage(frame, &models.PageContent{}, nil, Visibility{}, Options{IncludeSeals: true}, func(Frame[*testResources], Visitor[*testResources]) error {
		return want
	}, func(Frame[*testResources], models.PageItem) {}); !errors.Is(err, want) {
		t.Fatalf("印章加载错误丢失：%v", err)
	}
}

func TestCroppedCompositeFrameMapsTextAndClipsConsistently(t *testing.T) {
	text := textItem(1)
	text.Text.Boundary = models.StBox{X: 10, Y: 20, Width: 5, Height: 4}
	dp := &models.DrawParam{}
	scope := &testResources{composites: map[models.StRefID]Composite{
		100: {Items: []models.PageItem{text}, Box: models.StBox{Width: 100, Height: 100}, Source: models.StBox{X: 10, Y: 20, Width: 20, Height: 10}, DrawParam: dp},
	}}
	clip := geom.Rectangle(20, 12).Translate(35, 47)
	frame := Frame[*testResources]{Scope: scope, Matrix: geom.Identity.Translate(5, 7), Clip: clip, Source: SourcePage}
	object := models.PageItem{Kind: models.PageItemComposite, Composite: &models.CompositeObject{CtComposite: models.CtComposite{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 30, Y: 40, Width: 40, Height: 30}}, ResourceID: 100,
	}}}
	visited := 0
	WalkItems(frame, []models.PageItem{{Kind: models.PageItemText}, object}, func(child Frame[*testResources], item models.PageItem) {
		if item.Kind != models.PageItemText {
			return
		}
		visited++
		if !child.InComposite || child.Scope != scope || child.DrawParam != dp || child.Depth != 1 {
			t.Fatal("复合对象没有保留深度、作用域或绘制参数")
		}
		layout := child.MapLayout(textlayout.Layout{Size: 4, Glyphs: []textlayout.Character{
			{X: 10, Y: 20, Width: 5, Height: 4}, {X: 40, Y: 20, Width: 5, Height: 4},
		}})
		g := layout.Glyphs[0]
		if g.X != 35 || g.Y != 47 || g.Width != 10 || g.Height != 12 || layout.Size != 12 {
			t.Fatalf("文字映射错误：%+v", layout)
		}
		if layout.Glyphs[1].Width != 0 || layout.Glyphs[1].Height != 0 {
			t.Fatal("裁剪外的字符仍有可见区域")
		}
		bounds := geom.Rectangle(5, 4).Translate(10, 20).Transform(child.Matrix).And(child.Clip).Bounds()
		if bounds != (geom.Rect{X0: g.X, Y0: g.Y, X1: g.X + g.Width, Y1: g.Y + g.Height}) {
			t.Fatal("链接区域与文字区域使用了不同的坐标映射")
		}
	})
	if visited != 1 || clip.Bounds() != (geom.Rect{X0: 35, Y0: 47, X1: 55, Y1: 59}) || scope.composites[100].Source.X != 10 {
		t.Fatal("缺失图元被访问，或遍历修改了源区域与裁剪")
	}
}

func TestCyclicCompositesRespectDepthAndExternalBudget(t *testing.T) {
	for _, limit := range []int{3, MaxDepth * 2} {
		scope := &testResources{}
		item := models.PageItem{Kind: models.PageItemComposite, Composite: &models.CompositeObject{CtComposite: models.CtComposite{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}}, ResourceID: 100,
		}}}
		calls := 0
		scope.resolve = func(models.CompositeObject, *models.DrawParam, int) (Composite, bool) {
			calls++
			return Composite{Items: []models.PageItem{item}, Box: models.StBox{Width: 10, Height: 10}, Source: models.StBox{Width: 10, Height: 10}}, calls <= limit
		}
		visited := 0
		WalkItems(Frame[*testResources]{Scope: scope, Matrix: geom.Identity}, []models.PageItem{item}, func(Frame[*testResources], models.PageItem) { visited++ })
		if want := min(limit+1, MaxDepth); calls != want || visited != want {
			t.Fatalf("循环未受深度或外部预算限制：calls=%d visited=%d want=%d", calls, visited, want)
		}
	}
}

func TestFrameConvertsClipsAndPlacesChildScopeWithoutMutatingPaths(t *testing.T) {
	scope := &testResources{}
	frame := Frame[*testResources]{Scope: scope, Box: models.StBox{Height: 100}, Matrix: geom.Identity.Translate(10, 20)}
	path := geom.Rectangle(4, 6).Translate(2, 3)
	before := path.Copy()
	converted := frame.OFDClip(path)
	if converted.Bounds() != (geom.Rect{X0: 12, Y0: 111, X1: 16, Y1: 117}) || !reflect.DeepEqual(path, before) {
		t.Fatal("画布裁剪没有正确翻转到 OFD 坐标，或修改了共享路径")
	}
	frame.Matrix, frame.Clip = geom.Identity, geom.Rectangle(10, 10)
	parentClip := geom.Rectangle(2, 2).Translate(32, 42)
	placed := frame.Place(geom.Identity.Translate(30, 40).Scale(.5, .5), SourceSeal, parentClip)
	if placed.Scope != scope || placed.Clip.Bounds() != parentClip.Bounds() || frame.Clip.Bounds() != geom.RectFromSize(0, 0, 10, 10) {
		t.Fatal("印章放置丢失子资源作用域或修改了原裁剪")
	}
	if got := placed.MapLayout(textlayout.Layout{Font: 42, Size: 4}); got.Font != 0 || got.Source != SourceSeal || got.Size != 2 {
		t.Fatalf("印章布局元数据错误：%+v", got)
	}
}

func TestMapBoxRejectsDegenerateAndOverflowedSource(t *testing.T) {
	for _, source := range []models.StBox{
		{}, {Width: 0, Height: 1}, {Width: math.SmallestNonzeroFloat64, Height: 1}, {X: math.NaN(), Width: 1, Height: 1},
	} {
		if _, ok := MapBox(source, models.StBox{Width: 100, Height: 100}); ok {
			t.Fatalf("异常源区域生成了有效映射：%+v", source)
		}
	}
}
