package content

import (
	"encoding/xml"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/navigation"
	"github.com/zc310/ofd/internal/render/pathbuild"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/pkg/spec"
)

// 同一份公开声明同时供遍历、路径、导航和诊断消费，不能改写源图元。
func TestSpecLayerSharedConsumersPreserveOrderAndDeclaration(t *testing.T) {
	const data = `<Layer ID="1">
		<TextObject ID="2" Boundary="0 0 10 5"><TextCode X="0" Y="4">前</TextCode></TextObject>
		<PageBlock ID="3">
			<PathObject ID="4" Boundary="10 20 10 6">
				<Actions><Action Event="CLICK"><URI URI="guide" Base="https://example.test/"/></Action></Actions>
				<AbbreviatedData>M 0 0 L 10 0 L 10 6 L 0 6 C</AbbreviatedData>
			</PathObject>
			<ImageObject ID="5" ResourceID="50" Boundary="0 0 1 1"/>
		</PageBlock>
		<TextObject ID="6" Boundary="0 0 10 5"><TextCode X="0" Y="4">后</TextCode></TextObject>
	</Layer>`
	var layer, original spec.Layer
	for _, target := range []*spec.Layer{&layer, &original} {
		if err := xml.Unmarshal([]byte(data), target); err != nil {
			t.Fatal(err)
		}
	}
	frame := Frame[*testResources]{Scope: &testResources{}, Box: spec.StBox{Width: 80, Height: 50}, Matrix: geom.Identity}
	var ids []spec.StID
	var text strings.Builder
	report := &renderstate.ReportBuilder{}
	budget := renderstate.Budget{Report: report}
	WalkLayers(frame, []*spec.Layer{&layer}, func(current Frame[*testResources], item spec.PageItem) {
		ids = append(ids, ItemID(item))
		budget.ObjectID = ItemID(item)
		switch item.Kind {
		case spec.PageItemText:
			for _, code := range item.Text.TextCode {
				text.WriteString(code.Value)
			}
		case spec.PageItemPath:
			path := pathbuild.Object(*item.Path, current.Box.Height, nil)
			if path.Bounds() != (geom.Rect{X0: 10, Y0: 24, X1: 20, Y1: 30}) {
				t.Fatalf("路径坐标变换错误：%v", path.Bounds())
			}
			budget.Diagnose("test", "记录图元位置", 50)
		case spec.PageItemImage, spec.PageItemComposite, spec.PageItemBlock:
		}
	})
	if !slices.Equal(ids, []spec.StID{2, 4, 5, 6}) || text.String() != "前后" {
		t.Fatalf("图元或文字顺序改变：ids=%v text=%q", ids, text.String())
	}
	links := navigation.Links(layer.Items, nil)
	if len(links) != 1 || links[0].URI != "https://example.test/guide" ||
		links[0].X != 10 || links[0].Y != 20 || links[0].Width != 10 || links[0].Height != 6 {
		t.Fatalf("导航没有复用图元动作与边界：%+v", links)
	}
	diagnostics := report.Snapshot().Diagnostics
	if len(diagnostics) != 1 || diagnostics[0].ObjectID != 4 || diagnostics[0].ResourceID != 50 {
		t.Fatalf("诊断丢失标准标识：%+v", diagnostics)
	}
	if !reflect.DeepEqual(layer, original) {
		t.Fatal("共享消费改写了公开声明")
	}
}
