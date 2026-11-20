package canvasfont

import (
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/font"
	"golang.org/x/image/font/gofont/goregular"
)

func TestFixedWidthFallbackHandlesAndroidMacintoshFontNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fixed bool
	}{
		{"Android Sans", false},
		{"Android Mono", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			family := canvas.NewFontFamily("default")
			if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
				t.Fatal(err)
			}
			face := family.Face(cjkProbeSize, canvas.Black)
			// 复现设备日志中的 platform=1、encoding=25、language=33、name=1。
			// 缺字回退读取名称表时，不能因未实现该名称编码而中断页面加载。
			for i := range face.Font.SFNT.Name.NameRecord {
				record := &face.Font.SFNT.Name.NameRecord[i]
				record.Platform = font.PlatformMacintosh
				record.Encoding = font.EncodingMacintoshChineseSimplified
				record.Language = 33
				record.Value = []byte(tc.name)
			}
			fonts := NewFonts(nil)
			defer fonts.Close()
			if got := fonts.prefersFixedWidthFallback(family, textObjectWith("中文"), FontRegular); got != tc.fixed {
				t.Fatalf("等宽回退分类错误: got=%v want=%v", got, tc.fixed)
			}
		})
	}
}
