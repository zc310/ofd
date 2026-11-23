package content

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
	"github.com/zc310/ofd/pkg/spec"
)

type testResources struct {
	params     map[spec.StID]*spec.DrawParam
	composites map[spec.StRefID]Composite
	templates  map[spec.StID]*models.PageContent
	loads      []spec.StID
	err        error
	resolve    func(spec.CompositeObject, *spec.DrawParam, int) (Composite, bool)
	textClip   *geom.Path
}

func (s *testResources) GetDrawParam(id spec.StID) *spec.DrawParam { return s.params[id] }
func (s *testResources) ResolveComposite(object spec.CompositeObject, dp *spec.DrawParam, depth int) (Composite, bool) {
	if s.resolve != nil {
		return s.resolve(object, dp, depth)
	}
	unit, ok := s.composites[object.ResourceID]
	return unit, ok
}
func (s *testResources) TextClip(spec.CTGraphicUnit) *geom.Path      { return s.textClip }
func (*testResources) CompositeClip(spec.CompositeObject) *geom.Path { return nil }
func (s *testResources) LoadTemplate(id spec.StID) (*models.PageContent, error) {
	s.loads = append(s.loads, id)
	return s.templates[id], s.err
}
func (*testResources) TemplateForeground(ref spec.Template) bool { return ref.ZOrder == "Foreground" }

func textItem(id spec.StID) spec.PageItem {
	return spec.PageItem{Kind: spec.PageItemText, Text: &spec.TextObject{ID: id}}
}

func layer(id spec.StID, kind string, items ...spec.PageItem) *spec.Layer {
	return &spec.Layer{ID: id, Type: kind, CTPageBlock: spec.CTPageBlock{Items: items}}
}

func annotation(kind spec.AnnotType, id spec.StID) *spec.Annot {
	return &spec.Annot{Type: kind, Appearance: &spec.Appearance{
		Boundary:    &spec.StBox{X: 5, Y: 6, Width: 10, Height: 10},
		CTPageBlock: spec.CTPageBlock{Items: []spec.PageItem{textItem(id)}},
	}}
}

func TestWalkPagePreservesOrderVisibilityAndScope(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(map[bool]string{false: "正文模板", true: "全部来源"}[extra], func(t *testing.T) {
			backgroundDP, bodyDP := &spec.DrawParam{}, &spec.DrawParam{}
			scope := &testResources{params: map[spec.StID]*spec.DrawParam{20: backgroundDP, 21: bodyDP}, templates: map[spec.StID]*models.PageContent{
				10: {Content: &spec.Content{Layer: []*spec.Layer{layer(1, "Body", textItem(1))}}},
				11: {Content: &spec.Content{Layer: []*spec.Layer{layer(1, "Body", textItem(6))}}},
			}}
			layers := []*spec.Layer{layer(4, "Body", textItem(4)), nil, layer(3, "Background", textItem(3)), layer(5, "Body", textItem(5))}
			layers[0].DrawParam, layers[2].DrawParam = 21, 20
			page := &models.PageContent{
				Template: []spec.Template{{TemplateID: 10}, {TemplateID: 11, ZOrder: "Foreground"}, {TemplateID: 12}},
				Content:  &spec.Content{Layer: layers},
			}
			annotations := &spec.PageAnnot{Annots: []*spec.Annot{nil, annotation(spec.AnnotTypeStamp, 7), annotation(spec.AnnotTypeWatermark, 2)}}
			childScope := &testResources{}
			var ids []spec.StID
			var sources []Source
			seals := 0
			err := WalkPage(Frame[*testResources]{Scope: scope, Matrix: geom.Identity, Source: SourcePage}, page, annotations,
				Visibility{HiddenLayers: map[spec.StID]bool{5: true}, HiddenTemplates: map[spec.StID]bool{12: true}},
				Options{IncludeAnnotations: extra, IncludeWatermarks: extra, IncludeSeals: extra},
				func(frame Frame[*testResources], visit Visitor[*testResources]) error {
					seals++
					frame.Scope = childScope
					visit(frame.Place(geom.Identity.Translate(30, 40), SourceSeal, nil), textItem(8))
					return nil
				}, func(frame Frame[*testResources], item spec.PageItem) {
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
			wantIDs := []spec.StID{1, 3, 4, 6}
			wantSources := []Source{SourceTemplate, SourcePage, SourcePage, SourceTemplate}
			if extra {
				wantIDs = []spec.StID{1, 2, 3, 4, 6, 8, 7}
				wantSources = []Source{SourceTemplate, SourceWatermark, SourcePage, SourcePage, SourceTemplate, SourceSeal, SourceAnnotation}
			}
			if !reflect.DeepEqual(ids, wantIDs) || !reflect.DeepEqual(sources, wantSources) || (extra && seals != 1) || (!extra && seals != 0) {
				t.Fatalf("遍历顺序或来源错误：ids=%v sources=%v seals=%d", ids, sources, seals)
			}
			if !reflect.DeepEqual(scope.loads, []spec.StID{10, 11}) || len(page.Content.Layer) != 4 || page.Content.Layer[0].ID != 4 {
				t.Fatal("加载了隐藏模板或修改了原图层顺序")
			}
		})
	}
}

func TestWalkPagePreservesTemplateAndSealErrors(t *testing.T) {
	want := errors.New("resource unavailable")
	scope := &testResources{err: want}
	frame := Frame[*testResources]{Scope: scope, Matrix: geom.Identity}
	page := &models.PageContent{Template: []spec.Template{{TemplateID: 10}}}
	if err := WalkPage(frame, page, nil, Visibility{}, Options{}, nil, func(Frame[*testResources], spec.PageItem) {}); !errors.Is(err, want) {
		t.Fatalf("模板加载错误丢失：%v", err)
	}
	if err := WalkPage(frame, &models.PageContent{}, nil, Visibility{}, Options{IncludeSeals: true}, func(Frame[*testResources], Visitor[*testResources]) error {
		return want
	}, func(Frame[*testResources], spec.PageItem) {}); !errors.Is(err, want) {
		t.Fatalf("印章加载错误丢失：%v", err)
	}
}

func TestCroppedCompositeFrameMapsTextAndClipsConsistently(t *testing.T) {
	text := textItem(1)
	text.Text.Boundary = spec.StBox{X: 10, Y: 20, Width: 5, Height: 4}
	dp := &spec.DrawParam{}
	scope := &testResources{composites: map[spec.StRefID]Composite{
		100: {Items: []spec.PageItem{text}, Box: spec.StBox{Width: 100, Height: 100}, Source: spec.StBox{X: 10, Y: 20, Width: 20, Height: 10}, DrawParam: dp},
	}}
	clip := geom.Rectangle(20, 12).Translate(35, 47)
	frame := Frame[*testResources]{Scope: scope, Matrix: geom.Identity.Translate(5, 7), Clip: clip, Source: SourcePage}
	object := spec.PageItem{Kind: spec.PageItemComposite, Composite: &spec.CompositeObject{CtComposite: spec.CtComposite{
		CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{X: 30, Y: 40, Width: 40, Height: 30}}, ResourceID: 100,
	}}}
	visited := 0
	WalkItems(frame, []spec.PageItem{{Kind: spec.PageItemText}, object}, func(child Frame[*testResources], item spec.PageItem) {
		if item.Kind != spec.PageItemText {
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
		item := spec.PageItem{Kind: spec.PageItemComposite, Composite: &spec.CompositeObject{CtComposite: spec.CtComposite{
			CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{Width: 10, Height: 10}}, ResourceID: 100,
		}}}
		calls := 0
		scope.resolve = func(spec.CompositeObject, *spec.DrawParam, int) (Composite, bool) {
			calls++
			return Composite{Items: []spec.PageItem{item}, Box: spec.StBox{Width: 10, Height: 10}, Source: spec.StBox{Width: 10, Height: 10}}, calls <= limit
		}
		visited := 0
		WalkItems(Frame[*testResources]{Scope: scope, Matrix: geom.Identity}, []spec.PageItem{item}, func(Frame[*testResources], spec.PageItem) { visited++ })
		if want := min(limit+1, MaxDepth); calls != want || visited != want {
			t.Fatalf("循环未受深度或外部预算限制：calls=%d visited=%d want=%d", calls, visited, want)
		}
	}
}

func TestFrameConvertsClipsAndPlacesChildScopeWithoutMutatingPaths(t *testing.T) {
	scope := &testResources{}
	frame := Frame[*testResources]{Scope: scope, Box: spec.StBox{Height: 100}, Matrix: geom.Identity.Translate(10, 20)}
	path := geom.Rectangle(4, 6).Translate(2, 3)
	before := path.Copy()
	converted := frame.OFDClip(path)
	if converted.Bounds() != (geom.Rect{X0: 12, Y0: 23, X1: 16, Y1: 29}) || !reflect.DeepEqual(path, before) {
		t.Fatal("画布裁剪没有保持 OFD 坐标，或修改了共享路径")
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
	for _, source := range []spec.StBox{
		{}, {Width: 0, Height: 1}, {Width: math.SmallestNonzeroFloat64, Height: 1}, {X: math.NaN(), Width: 1, Height: 1},
	} {
		if _, ok := MapBox(source, spec.StBox{Width: 100, Height: 100}); ok {
			t.Fatalf("异常源区域生成了有效映射：%+v", source)
		}
	}
}
