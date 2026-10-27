package svg

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
)

func TestSVGPreparesIndependentImageAtSelectedPageSize(t *testing.T) {
	source := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><text x="10" y="30">SVG text</text></svg>`)
	conv := &converter.Converter{}
	converter.WithPaperSize("A3")(conv)
	converter.WithLandscape(true)(conv)
	output, err := prepareHTML(append([]byte{0xEF, 0xBB, 0xBF}, source...), conv)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"width:420mm;height:297mm", "object-fit:contain", "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(source)} {
		if !bytes.Contains(output, []byte(want)) {
			t.Fatalf("页面或 SVG 内容缺少 %q", want)
		}
	}
}

func TestSVGRejectsMalformedWrongRootAndExcessiveNesting(t *testing.T) {
	for _, input := range []string{
		"", "plain text", "<html/>", "<svg/>", `<svg xmlns="http://www.w3.org/2000/svg">`,
		`<svg xmlns="http://www.w3.org/2000/svg"/><svg xmlns="http://www.w3.org/2000/svg"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg"/>trailing`,
		`<svg xmlns="http://www.w3.org/2000/svg">` + strings.Repeat("<g>", 130) + strings.Repeat("</g>", 130) + "</svg>",
	} {
		if _, err := prepareHTML([]byte(input), nil); err == nil {
			t.Fatalf("无效 SVG 未报错: %q", input)
		}
	}
}
