package spec_test

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestActionVariantsRoundTripWithoutApplyingExecutionDefaults(t *testing.T) {
	for _, body := range []string{
		`<Goto><Dest Type="XYZ" PageID="2" Left="0" Top="0" Right="0" Bottom="0" Zoom="0"/></Goto>`,
		`<Goto><Bookmark Name="chapter"/></Goto>`,
		`<URI URI="next" Base="https://example.test/" Target=""/>`,
		`<GotoA AttachID="note"/>`,
		`<GotoA AttachID="note" NewWindow="false"/>`,
		`<GotoA AttachID="note" NewWindow="true"/>`,
		`<Sound ResourceID="10"/>`,
		`<Sound ResourceID="10" Volume="0" Repeat="false" Synchronous="false"/>`,
		`<Movie ResourceID="11"/>`,
		`<Movie ResourceID="11" Operator="Pause"/>`,
	} {
		var original spec.Action
		if err := xml.Unmarshal([]byte(`<Action Event="CLICK">`+body+`</Action>`), &original); err != nil {
			t.Fatal(err)
		}
		data, err := xml.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var decoded spec.Action
		if err := xml.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded, original) {
			t.Fatalf("动作往返改变了声明: %s", data)
		}
		for _, name := range []string{"NewWindow", "Volume", "Repeat", "Synchronous", "Operator", "Target", "Left", "Zoom"} {
			if strings.Contains(string(data), name+`="`) != strings.Contains(body, name+`="`) {
				t.Fatalf("动作往返改变了 %s 的省略状态: %s", name, data)
			}
		}
	}
}

func TestActionRegionPreservesXMLPathNamesOrderAndArcParameters(t *testing.T) {
	input := `<Action Event="CLICK"><Region><Area Start="1 2"><Move Point1="3 4"/><Line Point1="5 6"/><QuadraticBezier Point1="7 8" Point2="9 10"/><CubicBezier Point1="11 12" Point2="13 14" Point3="15 16"/><Arc SweepDirection="false" LargeArc="true" RotationAngle="0" EllipseSize="5 6" EndPoint="17 18"/><Close/></Area><Area Start="0 0"><Line Point1="1 1"/><Close/></Area></Region><Goto><Dest Type="Fit" PageID="2"/></Goto></Action>`
	var original spec.Action
	if err := xml.Unmarshal([]byte(input), &original); err != nil {
		t.Fatal(err)
	}
	data, err := xml.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded spec.Action
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, original) || len(decoded.Region.Areas) != 2 {
		t.Fatalf("动作区域的线段、区域顺序或圆弧参数改变: %s", data)
	}
	names := make([]string, 0, len(decoded.Region.Areas[0].Paths))
	for _, path := range decoded.Region.Areas[0].Paths {
		names = append(names, path.XMLName.Local)
	}
	if !reflect.DeepEqual(names, []string{"Move", "Line", "QuadraticBezier", "CubicBezier", "Arc", "Close"}) {
		t.Fatalf("区域线段名或顺序错误: %v", names)
	}
}
