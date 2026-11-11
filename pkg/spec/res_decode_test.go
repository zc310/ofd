package spec_test

import (
	"encoding/xml"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestResourceDecodePreservesRelativeZeroOverridesAndCompositeContent(t *testing.T) {
	input := `<Res BaseLoc="../Assets/./"><DrawParams><DrawParam ID="1" LineWidth="2" DashOffset="3" MiterLimit="4"><FillColor Value="15 0 0 0" ColorSpace="7"/></DrawParam><DrawParam ID="2" Relative="1" LineWidth="0" DashOffset="0" MiterLimit="0"/><DrawParam ID="3"/></DrawParams><CompositeGraphicUnits><CompositeGraphicUnit ID="4" Width="10" Height="20"><Content><ImageObject ID="5" Boundary="0 0 10 10" ResourceID="8"/><TextObject ID="6" Boundary="0 0 10 10" Font="9" Size="3"><TextCode>复合正文</TextCode></TextObject></Content></CompositeGraphicUnit></CompositeGraphicUnits></Res>`
	var resource spec.Res
	if err := xml.Unmarshal([]byte(input), &resource); err != nil {
		t.Fatal(err)
	}
	params := resource.DrawParams.DrawParam
	if resource.BaseLoc != "../Assets/./" || params[1].Relative != 1 || !params[1].HasLineWidth() || !params[1].HasDashOffset() || !params[1].HasMiterLimit() || params[2].HasLineWidth() {
		t.Fatal("资源路径、继承引用或显式零值状态丢失")
	}
	merged := *params[0]
	merged.Override(params[1])
	if merged.LineWidth != 0 || merged.DashOffset != 0 || merged.MiterLimit != 0 {
		t.Fatal("显式零值未覆盖父绘制参数")
	}
	var direct spec.DrawParam
	direct.SetLineWidth(0)
	direct.SetDashOffset(0)
	direct.SetMiterLimit(0)
	if !direct.HasLineWidth() || !direct.HasDashOffset() || !direct.HasMiterLimit() {
		t.Fatal("直接构造无法表达显式零值")
	}
	unit := resource.CompositeGraphicUnits.CompositeGraphicUnit[0]
	if unit.ID != 4 || unit.Content.Items[0].Image.ID != 5 || unit.Content.Items[1].Text.TextCode[0].Value != "复合正文" {
		t.Fatal("复合资源内容或图元顺序丢失")
	}
}

func TestComplexColorsDecodeWithoutResolvingResources(t *testing.T) {
	for _, input := range []string{
		`<Color><Pattern Width="10" Height="20" CTM="1 0 0 1 2 3"><CellContent><PathObject ID="1" Boundary="0 0 10 10"><AbbreviatedData>M 0 0 L 1 1</AbbreviatedData></PathObject></CellContent></Pattern></Color>`,
		`<Color><RadialShd StartPoint="0 0" EndPoint="10 10" StartRadius="0" EndRadius="10"><Segment Position="0"><Color Value="#FFFF 0 0"/></Segment></RadialShd></Color>`,
		`<Color><GouraudShd Extend="1"><Point X="1" Y="2" EdgeFlag="0"><Color Value="1 2 3"/></Point></GouraudShd></Color>`,
		`<Color><LaGourandShd VerticesPerRow="2"><Point X="1" Y="2"><Color Value="1 2 3"/></Point></LaGourandShd></Color>`,
		`<Color><LaGouraudShd VerticesPerRow="2"><Point X="1" Y="2"><Color Value="1 2 3"/></Point></LaGouraudShd></Color>`,
	} {
		var color spec.CTColor
		if err := xml.Unmarshal([]byte(input), &color); err != nil {
			t.Fatal(err)
		}
		switch {
		case color.Pattern != nil:
			if color.Pattern.CTM == nil || color.Pattern.CellContent.Items[0].Path.ID != 1 {
				t.Fatal("底纹声明丢失")
			}
		case color.RadialShd != nil:
			values, count, _ := color.RadialShd.Segment[0].Color.Value.Components()
			if count != 3 || values[0] != 65535 {
				t.Fatal("径向渐变丢失原始位深")
			}
		case color.GouraudShd != nil:
			if color.GouraudShd.Point[0].X != 1 {
				t.Fatal("高洛德网格点丢失")
			}
		case color.LaGourandShd != nil, color.LaGouraudShd != nil:
		default:
			t.Fatal("复杂颜色子类型没有解码")
		}
	}
}
