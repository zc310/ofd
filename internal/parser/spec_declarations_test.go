package parser

import (
	"encoding/xml"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/spec"
)

// 先比较原始声明，再经过页面加载策略，防止 A4 兜底混入公开解码模型。
func TestReadPageContentPreservesSpecDeclarationsBeforePagePolicies(t *testing.T) {
	for _, tc := range []struct {
		name string
		area string
	}{
		{name: "缺省页面区域"},
		{name: "显式零尺寸", area: `<Area><PhysicalBox>0 0 0 0</PhysicalBox></Area>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := `<Page xmlns="http://www.ofdspec.org/2016">` + tc.area + `
				<Template TemplateID="10"/>
				<PageRes>./Res/../Res.xml</PageRes>
				<Actions><Action Event="PO"><GotoA AttachID="file" NewWindow="false"/></Action></Actions>
				<Content><Layer ID="1">
					<TextObject ID="2" Boundary="0 0 10 10" Visible="false" Fill="false">
						<TextCode X="0" Y="5">前</TextCode>
					</TextObject>
					<PageBlock ID="3">
						<ImageObject ID="4" ResourceID="40" Boundary="0 0 10 10"/>
						<PathObject ID="5" Boundary="0 0 10 10"><AbbreviatedData>M 0 0 L 1 1</AbbreviatedData></PathObject>
					</PageBlock>
					<TextObject ID="6" Boundary="0 0 10 10"><TextCode X="0" Y="5">后</TextCode></TextObject>
				</Layer></Content>
			</Page>`
			var declaration spec.PageContent
			if err := xml.Unmarshal([]byte(data), &declaration); err != nil {
				t.Fatal(err)
			}
			pkg, err := container.OpenBytes(parserTestArchive(t, map[string]string{"Page.xml": data}))
			if err != nil {
				t.Fatal(err)
			}
			defer pkg.Close()
			content, err := readPageContentXML(pkg, "Page.xml", maxPageXMLBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual((*spec.PageContent)(content), &declaration) {
				t.Fatalf("快速读取与公开声明不一致：\n读取=%+v\n声明=%+v", content, declaration)
			}
			if len(content.PageRes) != 1 || content.PageRes[0] != "./Res/../Res.xml" {
				t.Fatalf("读取时改写了资源词法路径：%v", content.PageRes)
			}
			if content.Area != nil && content.Area.PhysicalBox != (spec.StBox{}) {
				t.Fatalf("读取声明时提前填入了页面尺寸：%+v", content.Area)
			}
			box, err := NewPage(*content).PhysicalBox()
			if err != nil || box != (spec.StBox{Width: 210, Height: 297}) {
				t.Fatalf("内部页面没有保留 A4 兼容策略：%+v，%v", box, err)
			}
			if declaration.Area != nil && declaration.Area.PhysicalBox != (spec.StBox{}) {
				t.Fatal("内部页面策略污染了独立解码的公开声明")
			}
		})
	}
}
