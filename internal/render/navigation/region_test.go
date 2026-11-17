package navigation

import (
	"encoding/xml"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

func TestActionRegionPreservesComplexShapeAndObjectTransform(t *testing.T) {
	var action spec.Action
	if err := xml.Unmarshal([]byte(`<Action Event="CLICK"><Region><Area Start="1 2"><Line Point1="5 2"/><Line Point1="1 6"/><Close/></Area></Region><URI URI="guide"/></Action>`), &action); err != nil {
		t.Fatal(err)
	}
	ctm := spec.CTM{2, 0, 0, 2, 0, 0}
	path := ActionRegion(action, spec.CTGraphicUnit{Boundary: spec.StBox{X: 10, Y: 20, Width: 20, Height: 20}, CTM: &ctm})
	if box := path.Bounds(); box.X0 != 12 || box.Y0 != 24 || box.W() != 8 || box.H() != 8 {
		t.Fatalf("启动区域变换=%v", box)
	}
	if !path.ContainsPoint(13, 25, geom.NonZero) || path.ContainsPoint(19, 31, geom.NonZero) {
		t.Fatal("三角形被扩大为矩形")
	}
	normalized := geom.MustParseSVGPath(NormalizedRegion(path))
	if !normalized.ContainsPoint(.1, .1, geom.NonZero) || normalized.ContainsPoint(.9, .9, geom.NonZero) {
		t.Fatal("归一化后改变了启动区域")
	}
	if Region(nil) != nil || !Region(&spec.Region{}).Empty() {
		t.Fatal("没有区分缺省与显式空区域")
	}
}

func TestRegionSupportsDisjointAreasCurvesAndArcs(t *testing.T) {
	var region spec.Region
	data := `<Region><Area Start="0 0"><Line Point1="2 0"/><Line Point1="2 2"/><Line Point1="0 2"/><Close/></Area><Area Start="10 0"><QuadraticBezier Point1="11 -2" Point2="12 0"/><CubicBezier Point1="12 1" Point2="12 2" Point3="11 2"/><Arc EllipseSize="1 1" EndPoint="10 0" SweepDirection="true" LargeArc="false" RotationAngle="0"/><Close/></Area></Region>`
	if err := xml.Unmarshal([]byte(data), &region); err != nil {
		t.Fatal(err)
	}
	path := Region(&region)
	if !path.ContainsPoint(1, 1, geom.NonZero) || path.ContainsPoint(6, 1, geom.NonZero) || path.Bounds().X1 < 11 {
		t.Fatal("不相连区域、曲线或圆弧丢失")
	}
}

func TestRegionBudgetDisablesHotspotInsteadOfExpandingBoundary(t *testing.T) {
	region := &spec.Region{Areas: make([]spec.RegionArea, 129)}
	if path := ActionRegion(spec.Action{Region: region}, spec.CTGraphicUnit{Boundary: spec.StBox{Width: 20, Height: 20}}); !path.Empty() {
		t.Fatal("超限 Region 被扩大为对象边界")
	}
	region.Areas = []spec.RegionArea{{Paths: make([]spec.RegionPath, 4097)}}
	if !Region(region).Empty() {
		t.Fatal("命令超限后仍生成热区")
	}
}
