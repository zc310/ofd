package flow_test

import (
	"math"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func TestLeadingIconScalesWithTextAndStaysBeforeAlignedFirstLine(t *testing.T) {
	for _, align := range []string{"left", "center", "right"} {
		for _, size := range []float64{12, 24} {
			opts := flow.DefaultOptions()
			opts.Theme.Body.Align, opts.Theme.Body.Size = align, size
			d := flow.New(opts)
			icon := flow.Icon{Width: 16, Height: 16, Data: "M 0 0 L 16 0 L 8 16 C"}
			d.Blocks = []flow.Block{{Text: "First\nSecond", LeadingIcon: &icon}}
			model, err := d.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var path creator.Path
			count, first := 0, false
			for _, item := range model.Pages[0].Items {
				switch value := item.(type) {
				case creator.Path:
					path = value
					count++
				case creator.Text:
					if textValue(value) == "First" {
						first = true
						if path.CTM == nil || value.X <= path.X+path.Width || math.Abs(value.Y+value.Height/2-(path.Y+path.Height/2)) > size*25.4/72 {
							t.Fatalf("图标与 %s 对齐文字重叠或错位: %+v / %+v", align, path, value)
						}
					}
				}
			}
			if !first || count != 1 || math.Abs(path.Height-size*25.4/72) > 0.000001 || math.Abs(path.Width-path.Height) > 0.000001 {
				t.Fatalf("图标未按字号缩放或重复绘制: %+v", path)
			}
			if icon.Width != 16 || icon.Height != 16 {
				t.Fatal("排版修改了调用方图标")
			}
		}
	}
}

func TestLeadingIconRejectsInvalidViewportPathAndBlockType(t *testing.T) {
	valid := flow.Icon{Width: 16, Height: 16, Data: "M 0 0 L 16 0 L 8 16 C"}
	for _, change := range []string{"width", "height", "nan", "inf", "overflow", "path", "empty", "rule", "kind", "text"} {
		t.Run(change, func(t *testing.T) {
			icon := valid
			block := flow.Block{Text: "Label", LeadingIcon: &icon}
			switch change {
			case "width":
				icon.Width = 0
			case "height":
				icon.Height = -1
			case "nan":
				icon.Width = math.NaN()
			case "inf":
				icon.Height = math.Inf(1)
			case "overflow":
				icon.Width = math.MaxFloat64
			case "path":
				icon.Data = "M 0 0 B 1 1"
			case "empty":
				icon.Data = ""
			case "rule":
				icon.Rule = "invalid"
			case "kind":
				block.Kind = "code"
			case "text":
				block.Text = ""
			}
			d := flow.New()
			d.Blocks = []flow.Block{block}
			if _, err := d.Build(t.Context()); err == nil {
				t.Fatalf("无效图标未报错: %+v", icon)
			}
		})
	}
}
