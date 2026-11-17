package reader

import (
	"encoding/xml"
	"slices"
	"testing"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/pkg/spec"
)

// 直接从公开声明解码混合图元，避免测试依赖内部类型别名。
func specMixedLayer(t *testing.T) *spec.Layer {
	t.Helper()
	const data = `<Layer ID="10">
		<TextObject ID="1" Boundary="0 0 10 10"><TextCode X="0" Y="5">前</TextCode></TextObject>
		<PageBlock ID="20">
			<PathObject ID="2" Boundary="0 0 10 10"><AbbreviatedData>M 0 0 L 1 1</AbbreviatedData></PathObject>
			<ImageObject ID="3" ResourceID="30" Boundary="0 0 10 10"/>
			<PageBlock ID="21">
				<CompositeObject ID="4" ResourceID="40" Boundary="0 0 10 10"/>
			</PageBlock>
		</PageBlock>
		<TextObject ID="5" Boundary="0 0 10 10"><TextCode X="0" Y="5">后</TextCode></TextObject>
	</Layer>`
	var layer spec.Layer
	if err := xml.Unmarshal([]byte(data), &layer); err != nil {
		t.Fatal(err)
	}
	return &layer
}

func TestSpecPageItemsPreserveMixedOrderAndEarlyStop(t *testing.T) {
	layer := specMixedLayer(t)
	for _, tc := range []struct {
		name   string
		stopID string
		want   []string
	}{
		{name: "混合图元按原始顺序展开", want: []string{"1", "2", "3", "4", "5"}},
		{name: "嵌套图元停止后不再访问后续项", stopID: "3", want: []string{"1", "2", "3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ids []string
			walkPageItems(layer.Items, func(item spec.PageItem) bool {
				id := itemID(item)
				ids = append(ids, id)
				if pageItemGraphicUnit(item) == nil {
					t.Fatalf("图元 %s 丢失通用图形属性", id)
				}
				return id != tc.stopID
			})
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("图元顺序=%v，期望 %v", ids, tc.want)
			}
		})
	}
}

func TestSpecLayersPreserveTextOrderAndVisibilityWithoutMutation(t *testing.T) {
	layer := specMixedLayer(t)
	hidden := &spec.Layer{ID: 11}
	layers := []*spec.Layer{nil, layer, hidden}
	r := &Reader{hiddenLayers: map[int]render.PageVisibility{
		0: {HiddenLayers: map[spec.StID]bool{hidden.ID: true}},
	}}
	visible := r.visibleLayers(0, layers)
	if len(visible) != 1 || visible[0] != layer {
		t.Fatalf("图层过滤没有保留公开声明原始指针：%v", visible)
	}
	texts := pageTextObjectsFromLayers(visible)
	if len(texts) != 2 || texts[0] != layer.Items[0].Text || texts[1] != layer.Items[2].Text {
		t.Fatalf("文字提取改变了顺序或复制了声明对象：%v", texts)
	}
	if got := textValue(texts[0]) + textValue(texts[1]); got != "前后" {
		t.Fatalf("文字顺序=%q", got)
	}
	if layers[0] != nil || layers[1] != layer || layers[2] != hidden || len(layer.Items) != 3 {
		t.Fatal("图层过滤或文字提取改写了源声明")
	}
}
