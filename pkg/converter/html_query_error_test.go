package converter

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestHTMLTextLayerPropagatesTemplateLoadError(t *testing.T) {
	var input bytes.Buffer
	w := zip.NewWriter(&input)
	for _, entry := range []struct{ name, xml string }{
		{"OFD.xml", `<OFD><DocBody><DocRoot>Doc.xml</DocRoot></DocBody></OFD>`},
		{"Doc.xml", `<Document><CommonData><PageArea><PhysicalBox>0 0 10 10</PhysicalBox></PageArea><TemplatePage ID="10" BaseLoc="missing.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`},
		{"Page.xml", `<Page><Area><PhysicalBox>0 0 10 10</PhysicalBox></Area><Template TemplateID="10"/><Content/></Page>`},
	} {
		file, err := w.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(file, entry.xml); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		var output bytes.Buffer
		err := HTML(t.Context(), input.Bytes(), &output, HTMLSVG(), WithHTMLTextLayer(enabled))
		if enabled {
			if err == nil || !strings.Contains(err.Error(), "文字布局") {
				t.Fatalf("HTML 文字层静默忽略模板读取错误：%v", err)
			}
		} else if err != nil {
			t.Fatalf("禁用文字层时不应触发文字查询：%v", err)
		}
	}
}
