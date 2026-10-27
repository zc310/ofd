package canvas

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
)

// 对象未声明斜体时也应保留原始字形，不能用反向剪切把原生斜体拉直。
func TestTextFacePreservesNativeItalicWithoutMutatingFont(t *testing.T) {
	for _, registeredStyle := range []canvas.FontStyle{canvas.FontRegular, canvas.FontItalic} {
		t.Run(registeredStyle.String(), func(t *testing.T) {
			family := canvas.NewFontFamily("native-italic")
			if err := family.LoadFont(goitalic.TTF, 0, registeredStyle); err != nil {
				t.Fatal(err)
			}
			native := family.Face(12, canvas.Black, registeredStyle)
			angle := native.Font.Post.ItalicAngle
			if angle == 0 {
				t.Fatal("测试字体必须声明非零倾斜角")
			}
			face := buildTextFace(family, models.TextObject{CtText: models.CtText{Size: 12 / 2.83465}}, nil)
			if face.FauxItalic != 0 {
				t.Fatalf("原生斜体被额外剪切: %g（原角度 %g°）", face.FauxItalic, angle)
			}
			if face.Font != native.Font || native.Font.Post.ItalicAngle != angle || native.FauxItalic != 0 {
				t.Fatal("样式合成修改了原生斜体或共享字体")
			}
			probe := probeFamilyFace(family, 12, FontRegular)
			if probe.FauxItalic != 0 {
				t.Fatal("字体探测改变了原生斜体")
			}
			// 用原生斜体轮廓做独立几何预期，涵盖实际文字路径和 PDF 使用的整形文字行。
			wantPath, _ := native.ToPath("OFD")
			got, _ := face.ToPath("OFD")
			if got == nil || got.Empty() || got.String() != wantPath.String() {
				t.Fatal("对象未声明斜体时改变了原生字形")
			}
			matched := buildTextFace(family, models.TextObject{CtText: models.CtText{Size: 12 / 2.83465, Italic: true}}, nil)
			if matched.FauxItalic != 0 {
				t.Fatal("原生斜体被重复倾斜")
			}
			matchedPath, _ := matched.ToPath("OFD")
			if matchedPath == nil || matchedPath.String() != wantPath.String() {
				t.Fatal("显式请求斜体时改变了原生字形")
			}
		})
	}
	// 正体字体仍按既有策略合成斜体。
	regular := canvas.NewFontFamily("native-regular")
	if err := regular.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	synthetic := buildTextFace(regular, models.TextObject{CtText: models.CtText{Size: 4, Italic: true}}, nil)
	if synthetic.FauxItalic != 0.3 {
		t.Fatalf("零角度正体的合成斜体策略被改变: %g", synthetic.FauxItalic)
	}
}

func TestThousandPageEmbeddedObliquePreservesNativeGlyphGeometry(t *testing.T) {
	data, err := os.ReadFile("../../../../testdata/ofd-creator/1000-pages.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 1000 页样本")
	}
	if err != nil {
		t.Fatal(err)
	}
	document, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer document.Close()
	fonts := NewFonts(document.Documents[0])
	defer fonts.Close()
	// 样本的正文和模板均引用同一份内嵌 SmileySans-Oblique；不依赖本机安装字体。
	resource := document.Documents[0].GetFont(2)
	if resource == nil || resource.FontFile == "" || resource.FontName != "SmileySans-Oblique" {
		t.Fatal("样本的斜体资源发生改变")
	}
	family, err := fonts.LoadFont(2)
	if err != nil {
		t.Fatal(err)
	}
	// 直接读取未修复、未合成的内嵌字型作为几何参照，不能只断言轮廓尺寸合理。
	raw, err := fonts.FileCache.Read(string(resource.FontFile))
	if err != nil {
		t.Fatal(err)
	}
	native := canvas.NewFontFamily("original-oblique")
	if err := native.LoadFont(raw, 0, canvas.FontItalic); err != nil {
		t.Fatal(err)
	}
	for _, weight := range []int{400, 700} {
		for _, italic := range []bool{false, true} {
			t.Run(fmt.Sprintf("weight=%d/italic=%t", weight, italic), func(t *testing.T) {
				face := fonts.FaceObject(family, models.TextObject{CtText: models.CtText{Font: 2, Size: 15, Weight: weight, Italic: italic}}, nil).(canvasFontFace).face
				if face.FauxItalic != 0 {
					t.Fatalf("字重 %d 的倾斜变换异常: %g", weight, face.FauxItalic)
				}
				reference := native.Face(15*2.83465, canvas.Black, canvasStyle(textFontStyle(weight, true)))
				if reference.Font.Post.ItalicAngle != -8 || reference.FauxItalic != 0 {
					t.Fatal("原生字体的倾斜参照异常")
				}
				path, _ := face.ToPath("OFD 开源文档工具链")
				if path == nil || path.Empty() {
					t.Fatal("标题轮廓为空")
				}
				bounds := path.Bounds()
				if bounds.W() > 180 || bounds.H() < 8 || bounds.H() > 25 {
					t.Fatalf("标题被拉伸: %v", bounds)
				}
				wantPath, _ := reference.ToPath("OFD 开源文档工具链")
				if wantPath == nil || path.String() != wantPath.String() {
					t.Fatal("渲染字形与内嵌字体的原生倾斜轮廓不一致")
				}
			})
		}
	}
}
