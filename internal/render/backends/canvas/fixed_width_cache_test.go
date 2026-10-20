package canvas

import (
	"path/filepath"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func TestFixedWidthFallbackReusesCachedFontAcrossEngines(t *testing.T) {
	if !systemFontLookupUsable() {
		t.Skip("当前平台不使用系统字体")
	}
	firstFonts, secondFonts := NewFonts(nil), NewFonts(nil)
	defer firstFonts.Close()
	defer secondFonts.Close()
	resource := &models.Font{FixedWidth: true, FamilyName: "monospace"}
	first, ok := firstFonts.loadFixedWidthFont(resource, FontRegular)
	if !ok {
		t.Skip("缺少可用的系统等宽字体")
	}
	second, ok := secondFonts.loadFixedWidthFont(resource, FontRegular)
	if !ok || first != second {
		t.Fatal("同一系统等宽字体在不同引擎中被重复解析")
	}
	if firstFonts.RenderLock(first) != secondFonts.RenderLock(second) {
		t.Fatal("复用的系统等宽字体没有使用共享渲染锁")
	}
}

func TestFixedWidthFallbackClassificationIgnoresFirstCachedAlias(t *testing.T) {
	for _, mono := range []bool{false, true} {
		kind := "proportional"
		data := goregular.TTF
		if mono {
			kind, data = "monospace", gomono.TTF
		}
		for _, aliases := range [][2]string{{"fixed-width", "LogicalAlias"}, {"LogicalAlias", "fixed-width"}} {
			t.Run(kind+"/"+aliases[0], func(t *testing.T) {
				first, second := NewFonts(nil), NewFonts(nil)
				defer first.Close()
				defer second.Close()
				key := systemFontKey{path: t.Name()}
				load := func(family *canvas.FontFamily) error {
					return family.LoadFont(data, 0, canvas.FontRegular)
				}
				a, ok := loadCachedFont(first, key, aliases[0], load)
				if !ok {
					t.Fatal("加载测试字体失败")
				}
				b, ok := loadCachedFont(second, key, aliases[1], load)
				if !ok || a != b || b.Name() != aliases[0] {
					t.Fatal("未复现按文件共享且族名取首次别名的条件")
				}
				for _, fonts := range []*Fonts{first, second} {
					if got := fonts.prefersFixedWidthFallback(b, textObjectWith("中文"), FontRegular); got != mono {
						t.Fatalf("首次别名 %q，等宽分类 = %v，期望 %v", aliases[0], got, mono)
					}
				}
			})
		}
	}
}

func TestFixedWidthFallbackHonorsCurrentResourceDeclaration(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	var resource *models.Font
	doc.ForEachFont(func(_ models.StID, value *models.Font) bool { resource = value; return false })
	if resource == nil {
		t.Fatal("测试文档缺少字体声明")
	}
	// 该测试独有的解析实例，不改变夹具文件；构造明确的等宽声明与比例替代字体。
	resource.FixedWidth, resource.FontName, resource.FamilyName = true, "LogicalAlias", "LogicalAlias"
	fonts := NewFonts(doc)
	defer fonts.Close()
	family := canvas.NewFontFamily("arbitrary-cache-alias")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	object := textObjectWith("中文")
	object.Font = models.StRefID(resource.ID)
	if !fonts.prefersFixedWidthFallback(family, object, FontRegular) {
		t.Fatal("共享族别名或替代字体覆盖了当前资源的明确等宽声明")
	}
	resource.FixedWidth = false
	if fonts.prefersFixedWidthFallback(family, object, FontRegular) {
		t.Fatal("比例字体应使用普通 CJK 回退")
	}
}
