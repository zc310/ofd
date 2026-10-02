package docx

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

// TestElementFieldsAreNotScalars 挡住一整类错误：OOXML 里 ST_String 之类的简单
// 类型必须写成 <w:xxx w:val="…"/>，而 Go 的 encoding/xml 把裸 string/int/bool
// 字段序列化成 <w:xxx>文本</w:xxx>。实测踩过：ParagraphProperties.Style 声明成
// *string 时产出的是 <w:pStyle>Heading2</w:pStyle>，Word 与 LibreOffice 都会把
// 样式当成缺失，标题静默退化成正文。
//
// 所以凡是要作为元素出现的字段，只能是结构体、指向结构体的指针、指向 Val 的
// 指针，或它们的切片；标量只能出现在 ,attr 属性里。
func TestElementFieldsAreNotScalars(t *testing.T) {
	types := []any{
		ParagraphProperties{}, RunProperties{}, NumberingProps{}, RunFonts{},
		TableCellProperties{}, TableRowProperties{}, TableProperties{}, TableBorders{},
		TableGrid{}, GridColumn{},
		SectionProperties{}, PageSize{}, PageMargin{}, Columns{}, Measure{},
		VerticalMerge{}, Style{}, Styles{}, DocDefaults{}, Border{},
		Level{}, AbstractNumbering{}, NumberInstance{}, NumberingRoot{},
		Paragraph{}, Run{}, Text{}, Table{}, TableRow{}, TableCell{},
	}
	scalar := reflect.TypeFor[xml.Name]() // 占位，实际判断用下面的 kind 列表
	_ = scalar
	for _, value := range types {
		assertNoScalarElements(t, reflect.TypeOf(value))
	}
}

func assertNoScalarElements(t *testing.T, typ reflect.Type) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			continue
		}
		name := elementName(field)
		if name == "" {
			continue
		}
		target := field.Type
		if target.Kind() == reflect.Slice {
			target = target.Elem()
		}
		if target.Kind() == reflect.Ptr {
			target = target.Elem()
		}
		switch target.Kind() {
		case reflect.Struct:
			// Val 是允许的标量包装类型。
			if target != reflect.TypeFor[Val]() && target != reflect.TypeFor[TypeVal]() &&
				target != reflect.TypeFor[Text]() {
				continue
			}
		default:
			t.Errorf("%s.%s（%s）声明成 %s，会被序列化成元素文本而不是 w:val 属性；"+
				"请改用 Val 并配合 StringVal/IntVal/BoolVal",
				typ.Name(), field.Name, name, field.Type)
			continue
		}
		if target == reflect.TypeFor[Val]() || target == reflect.TypeFor[TypeVal]() {
			continue
		}
		// 结构体元素若含 chardata，也说明内容没走 w:val。
		if value, ok := reflect.New(target).Interface().(interface{ hasChardata() bool }); ok && value.hasChardata() {
			continue
		}
	}
}

// TestSchemaElementsAreNotAttributes 是第三个方向的断言：ECMA-376 里
// w:tblLayout、w:tblLook 这类是**子元素**（<w:tblLayout w:type="fixed"/>），
// 与 w:tblStyle、w:orient 这类**属性**同为 w: 前缀，形状相似，很容易写反。
// 实测踩过：w:tblLayout 声明成属性后产出 <w:tblPr w:tblLayout="fixed">，
// LibreOffice 解析表格时连带把后面的分页符一起丢掉，页数从 2 变 1。
//
// 判定依据是：若某个 tag 的名字出现在该类型的 schema 元素顺序表里，它就必须是
// 元素，不能带 ,attr。反过来带 ,attr 的名字不应出现在元素顺序表里。
func TestSchemaElementsAreNotAttributes(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		schema []string
	}{
		{"ParagraphProperties", ParagraphProperties{}, paragraphPropertiesOrder},
		{"RunProperties", RunProperties{}, runPropertiesOrder},
		{"TableCellProperties", TableCellProperties{}, tableCellPropertiesOrder},
		{"TableRowProperties", TableRowProperties{}, tableRowPropertiesOrder},
		{"TableProperties", TableProperties{}, tablePropertiesOrder},
		{"TableBorders", TableBorders{}, tableBordersOrder},
		{"SectionProperties", SectionProperties{}, sectionPropertiesOrder},
		{"Style", Style{}, styleOrder},
		{"Level", Level{}, numberingLevelOrder},
		{"AbstractNumbering", AbstractNumbering{}, abstractNumberingOrder},
		{"NumberingProps", NumberingProps{}, numberingPropertiesOrder},
		{"Paragraph", Paragraph{}, paragraphOrder},
		{"Run", Run{}, runOrder},
		{"Table", Table{}, tableOrder},
		{"TableRow", TableRow{}, tableRowOrder},
		{"TableCell", TableCell{}, tableCellOrder},
		{"Styles", Styles{}, stylesOrder},
		{"NumberingRoot", NumberingRoot{}, numberingOrder},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			known := make(map[string]bool, len(tc.schema))
			for _, name := range tc.schema {
				known[name] = true
			}
			typ := reflect.TypeOf(tc.value)
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				tag := field.Tag.Get("xml")
				name, options, _ := strings.Cut(tag, ",")
				if name == "" || name == "-" {
					continue
				}
				isAttr := false
				for _, option := range strings.Split(options, ",") {
					if option == "attr" {
						isAttr = true
					}
				}
				if isAttr && known[name] {
					t.Errorf("字段 %q 在 %s 的 schema 里是子元素，不该声明成属性："+
						"OOXML 会忽略它并输出非法的 <%s> 属性", name, tc.name, name)
				}
			}
		})
	}
}

// TestStructFieldOrderFollowsSchema 是本包最核心的断言：encoding/xml 按结构体字段
// 声明顺序输出，而 OOXML 的复杂类型是 xsd:sequence，顺序写错 Word 与 WPS 会判定
// 文档损坏。本地没有 OOXML XSD，LibreOffice 对顺序错误也毫无反应（实测），所以
// 顺序正确性只能靠这个测试保证。
//
// 断言两条：
//  1. 结构体声明的元素是 schema 顺序表的子集（没有拼错或不存在的元素）；
//  2. 声明顺序是 schema 顺序表的子序列（相对顺序合法，且没有重复）。
//
// 只检查相对顺序而不要求元素齐全，是为了让结构体可以按需逐步补齐；补字段时
// 字段必须插到 schema 规定的位置，插错位置会被这里抓住。
func TestStructFieldOrderFollowsSchema(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		schema []string
	}{
		{"ParagraphProperties", ParagraphProperties{}, paragraphPropertiesOrder},
		{"RunProperties", RunProperties{}, runPropertiesOrder},
		{"NumberingProps", NumberingProps{}, numberingPropertiesOrder},
		{"TableCellProperties", TableCellProperties{}, tableCellPropertiesOrder},
		{"TableRowProperties", TableRowProperties{}, tableRowPropertiesOrder},
		{"TableProperties", TableProperties{}, tablePropertiesOrder},
		{"TableBorders", TableBorders{}, tableBordersOrder},
		{"SectionProperties", SectionProperties{}, sectionPropertiesOrder},
		{"Style", Style{}, styleOrder},
		{"NumberingRoot", NumberingRoot{}, numberingOrder},
		{"AbstractNumbering", AbstractNumbering{}, abstractNumberingOrder},
		{"Level", Level{}, numberingLevelOrder},

		{"Paragraph", Paragraph{}, paragraphOrder},
		{"Run", Run{}, runOrder},
		{"Table", Table{}, tableOrder},
		{"TableRow", TableRow{}, tableRowOrder},
		{"TableCell", TableCell{}, tableCellOrder},
		{"Styles", Styles{}, stylesOrder},

		{"Inline", Inline{}, inlineOrder},
		{"Picture", Picture{}, pictureOrder},
		{"PictureNonVisual", PictureNonVisual{}, pictureNonVisualOrder},
		{"PictureBlipFill", PictureBlipFill{}, pictureBlipFillOrder},
		{"ShapeProperties", ShapeProperties{}, shapePropertiesOrder},
		{"Transform", Transform{}, transformOrder},
		{"PresetShape", PresetShape{}, presetShapeOrder},
		{"GraphicData", GraphicData{}, graphicDataOrder},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSchemaOrder(t, tc.value, tc.schema)
		})
	}
}

// assertSchemaOrder 校验 value 的元素字段是 schema 的子集且相对顺序合法。
func assertSchemaOrder(t *testing.T, value any, schema []string) {
	t.Helper()

	declared := declaredElements(reflect.TypeOf(value))
	if len(declared) == 0 {
		return
	}

	position := make(map[string]int, len(schema))
	for i, name := range schema {
		if _, dup := position[name]; dup {
			t.Fatalf("schema 顺序表里有重复元素 %q", name)
		}
		position[name] = i
	}

	seen := make(map[string]bool, len(declared))
	previous := -1
	for _, name := range declared {
		index, ok := position[name]
		if !ok {
			t.Errorf("字段 %q 不在 schema 顺序表里，可能是拼错或使用了该类型不允许的元素", name)
			continue
		}
		if seen[name] {
			t.Errorf("字段 %q 声明了多次", name)
			continue
		}
		seen[name] = true
		if index <= previous {
			t.Errorf("字段 %q 位置不对：schema 要求它排在 %q 之后（schema 第 %d 项，当前是第 %d 项）",
				name, schema[previous], index+1, previous+1)
		}
		previous = index
	}
}

// declaredElements 返回结构体声明的子元素名，顺序即字段声明顺序。
// 属性、匿名内嵌路径的父元素、以及跳过的字段不计入。
func declaredElements(t reflect.Type) []string {
	if t.Kind() != reflect.Struct {
		return nil
	}
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous {
			continue
		}
		name := elementName(field)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// elementName 从结构体 tag 里取出直接子元素名。"w:tblGrid>w:gridCol" 返回
// "w:tblGrid"，因为对父元素而言 tblGrid 才是它声明的那个子元素。
//
// 返回空串表示该字段不计入子元素顺序：xml.Name 类型的 XMLName 字段描述的是
// 类型自身的元素名而不是它声明的子元素，attr/chardata/any 选项同理。
func elementName(field reflect.StructField) string {
	if field.Type == reflect.TypeFor[xml.Name]() {
		return ""
	}
	tag := field.Tag.Get("xml")
	if tag == "" || tag == "-" {
		return ""
	}
	name, options, _ := strings.Cut(tag, ",")
	if name == "" {
		return ""
	}
	for _, option := range strings.Split(options, ",") {
		if option == "attr" || option == "chardata" || option == "any" {
			return ""
		}
	}
	if index := strings.Index(name, ">"); index >= 0 {
		name = name[:index]
	}
	return name
}

// TestSchemaOrderTablesHaveNoGap 校验顺序常量表本身覆盖了 ECMA-376 里的完整序列。
// TestStructFieldOrderFollowsSchema 只检查子集顺序，常量表被误删元素时它发现不了，
// 所以这里对每张表断言确切长度——两张最长的表在补齐 w:rPrChange 与 w:sectPrChange
// 之后才达到下界，这个测试此前正是靠它发现了遗漏。
func TestSchemaOrderTablesHaveNoGap(t *testing.T) {
	exact := []struct {
		name  string
		table []string
		want  int
	}{
		// EG_PPrBase(32) + w:rPr + w:sectPr + w:pPrChange
		{"paragraphPropertiesOrder", paragraphPropertiesOrder, 36},
		// EG_RPrBase(39) + w:rPrChange
		{"runPropertiesOrder", runPropertiesOrder, 40},
		// EG_SectPrContents(19) + 页眉页脚引用(2) + w:sectPrChange
		{"sectionPropertiesOrder", sectionPropertiesOrder, 22},
		// CT_Style 的 22 个元素，w:tblStylePr 可重复但只计一次
		{"styleOrder", styleOrder, 22},
		// CT_TcPrBase(13) + w:tcPrChange
		{"tableCellPropertiesOrder", tableCellPropertiesOrder, 14},
		// CT_TblPrBase(17) + w:tblPrChange
		// CT_TblPrBase 的 16 个子元素 + w:tblPrChange；w:tblStyle 是属性不计
		{"tablePropertiesOrder", tablePropertiesOrder, 17},
	}
	for _, item := range exact {
		if got := len(item.table); got != item.want {
			t.Errorf("%s 有 %d 项，ECMA-376 的 CT 定义有 %d 项，顺序表可能被误删或误增",
				item.name, got, item.want)
		}
	}
}
