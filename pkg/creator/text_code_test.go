package creator

import (
	"bytes"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

// 显式码位可共用同一 Y 指针；边界校正不能改动输入或重复平移共享坐标。
func TestCreateTextBoundaryPreservesInputAndRepeatedOutput(t *testing.T) {
	for _, tc := range []struct {
		name           string
		completeDeltas bool
	}{
		{"保留显式码位", false},
		{"补全文字增量", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y := 0.0, 0.0
			codes := []TextCode{{Value: "AB", X: &x, Y: &y}, {Value: "CD", X: &x, Y: &y}}
			document := Document{
				ID:    "text-boundary-input",
				Fonts: []Font{{Name: "Boundary", Data: goregular.TTF}},
				Pages: []Page{{Items: []Item{
					Text{X: 10, Y: 20, Width: 100, Height: 1, Size: 18, Font: "Boundary", TextCodes: codes},
				}}},
			}
			options := CreateOptions{Deterministic: true, PreserveEmbeddedFonts: true, CompleteTextCodeDeltas: tc.completeDeltas}
			first, err := MarshalWithOptions(document, options)
			if err != nil {
				t.Fatal(err)
			}
			if y != 0 {
				t.Errorf("边界校正修改了调用方 Y: 0 -> %g", y)
			}
			for index, code := range codes {
				if code.Y != &y || len(code.DeltaX) != 0 || len(code.DeltaY) != 0 {
					t.Errorf("边界校正或增量补全修改了输入码位 %d: %+v", index, code)
				}
			}
			second, err := MarshalWithOptions(document, options)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Error("重复构建同一文档的确定性输出不同")
			}
			for _, data := range [][]byte{first, second} {
				parsed := newTestOFD(t, data)
				defer parsed.Close()
				text := firstItemOfKind(parsed.Documents[0].Pages[0].Content().Layer[0].Items, models.PageItemText).Text
				if text.Boundary.Y >= 20 {
					t.Errorf("边界上沿未按字体度量上移: %g", text.Boundary.Y)
				}
				for index, code := range text.TextCode {
					if baseline := text.Boundary.Y + code.Y; math.Abs(baseline-20) > 1e-4 {
						t.Errorf("码位 %d 的基线变化: 20 -> %g", index, baseline)
					}
				}
			}
		})
	}
}
