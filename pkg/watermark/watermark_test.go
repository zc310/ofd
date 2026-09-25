package watermark

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/replace"
)

func testdataPath(names ...string) string {
	parts := append([]string{"..", "..", "test", "testdata"}, names...)
	return filepath.Join(parts...)
}

func readTestdata(t *testing.T, names ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(testdataPath(names...))
	if err != nil {
		t.Skipf("缺少 fixture: %v", err)
	}
	return data
}

func openPackage(t *testing.T, data []byte) *core.Package {
	t.Helper()
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatalf("打开结果失败: %v", err)
	}
	t.Cleanup(func() { _ = pkg.Close() })
	return pkg
}

func readEntry(t *testing.T, pkg *core.Package, name string) []byte {
	t.Helper()
	data, err := pkg.Read(name)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return data
}

func parseXML(t *testing.T, data []byte) *etree.Document {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		t.Fatalf("解析 XML 失败: %v", err)
	}
	return doc
}

func attr(el *etree.Element, name string) string {
	return el.SelectAttrValue(name, "")
}

// findWatermarks 返回页面注解文件中 Type="Watermark" 的 Annot 元素。
func findWatermarks(t *testing.T, data []byte) []*etree.Element {
	t.Helper()
	root := parseXML(t, data).Root()
	if root == nil {
		return nil
	}
	var annots []*etree.Element
	for _, child := range root.ChildElements() {
		if child.Tag == "Annot" && attr(child, "Type") == watermarkType {
			annots = append(annots, child)
		}
	}
	return annots
}

func findAnnotationsElement(root *etree.Element) *etree.Element {
	for _, child := range root.ChildElements() {
		if child.Tag == "Annotations" {
			return child
		}
	}
	return nil
}

func sampleWatermark() Watermark {
	visible := true
	printValue := true
	readOnly := false
	return Watermark{
		Creator:     "watermark-test",
		LastModDate: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
		Visible:     &visible,
		Print:       &printValue,
		NoZoom:      true,
		ReadOnly:    &readOnly,
		Remark:      "测试水印",
		Parameters:  []creator.AnnotationParameter{{Name: "Author", Value: "测试"}},
		Boundary:    &creator.Box{X: 10, Y: 20, Width: 100, Height: 20},
		Appearance:  []byte(`<TextObject ID="900" Boundary="10 20 100 20" Font="4" Size="12" Fill="true"><TextCode X="0" Y="8">保密资料</TextCode></TextObject>`),
	}
}

func TestAddWatermarkToCleanDocument(t *testing.T) {
	var buffer bytes.Buffer
	err := Add(readTestdata(t, "hello.ofd"), Target{}, sampleWatermark(), &buffer, Options{})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())

	page := readEntry(t, result, "Doc_0/Annotations/Page_1.xml")
	annots := findWatermarks(t, page)
	if len(annots) != 1 {
		t.Fatalf("期望 1 条水印, 得到 %d", len(annots))
	}
	annot := annots[0]
	if got := attr(annot, "ID"); got != "6" {
		t.Fatalf("自动 ID 应为 6, 得到 %s", got)
	}
	if got := attr(annot, "Creator"); got != "watermark-test" {
		t.Fatalf("Creator 不符: %s", got)
	}
	if got := attr(annot, "LastModDate"); got != "2026-09-25" {
		t.Fatalf("LastModDate 不符: %s", got)
	}
	if got := attr(annot, "Type"); got != "Watermark" {
		t.Fatalf("Type 不符: %s", got)
	}
	if got := attr(annot, "ReadOnly"); got != "false" {
		t.Fatalf("ReadOnly 应显式为 false: %s", got)
	}
	appearance := annot.FindElement("Appearance")
	if appearance == nil {
		t.Fatal("缺少 Appearance")
	}
	if got := attr(appearance, "Boundary"); got != "10 20 100 20" {
		t.Fatalf("Boundary 不符: %s", got)
	}
	if appearance.FindElement("TextObject") == nil {
		t.Fatal("Appearance 缺少 TextObject")
	}

	index := readEntry(t, result, "Doc_0/Annotations.xml")
	indexRoot := parseXML(t, index).Root()
	if indexRoot.Tag != "Annotations" {
		t.Fatalf("索引根标签不符: %s", indexRoot.Tag)
	}
	pages := indexRoot.ChildElements()
	if len(pages) != 1 || attr(pages[0], "PageID") != "1" {
		t.Fatalf("索引页不符")
	}
	if got := strings.TrimSpace(pages[0].FindElement("FileLoc").Text()); got != "Annotations/Page_1.xml" {
		t.Fatalf("FileLoc 不符: %s", got)
	}

	docRoot := parseXML(t, readEntry(t, result, "Doc_0/Document.xml")).Root()
	annotEl := findAnnotationsElement(docRoot)
	if annotEl == nil {
		t.Fatal("Document.xml 缺少 <Annotations>")
	}
	if got := strings.TrimSpace(annotEl.Text()); got != "Annotations.xml" {
		t.Fatalf("Annotations 文本不符: %s", got)
	}
}

func TestAddWatermarkAutoIDAfterExisting(t *testing.T) {
	var buffer bytes.Buffer
	err := Add(readTestdata(t, "annotations.ofd"), Target{}, Watermark{
		Creator:     "watermark-test",
		LastModDate: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
	}, &buffer, Options{})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())
	annots := findWatermarks(t, readEntry(t, result, "Doc_0/Annotations/Page_1.xml"))
	if len(annots) != 2 {
		t.Fatalf("期望 2 条水印（原有 101 + 新增）, 得到 %d", len(annots))
	}
	newID := ""
	for _, annot := range annots {
		if attr(annot, "ID") == "38" {
			newID = "38"
		}
	}
	if newID != "38" {
		t.Fatalf("自动 ID 应为 MaxUnitID+1=38, 得到 %d", len(annots))
	}
	// MaxUnitID 不应被修改。
	doc := readEntry(t, result, "Doc_0/Document.xml")
	if !strings.Contains(string(doc), "<MaxUnitID>37</MaxUnitID>") {
		t.Fatalf("MaxUnitID 被意外修改: %s", doc)
	}
}

func TestAddExplicitIDAndConflict(t *testing.T) {
	data := readTestdata(t, "annotations.ofd")
	var buffer bytes.Buffer
	err := Add(data, Target{}, Watermark{ID: 200, Creator: "t"}, &buffer, Options{})
	if err != nil {
		t.Fatalf("显式 ID Add 失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())
	annots := findWatermarks(t, readEntry(t, result, "Doc_0/Annotations/Page_1.xml"))
	found := false
	for _, annot := range annots {
		if attr(annot, "ID") == "200" {
			found = true
		}
	}
	if len(annots) != 2 || !found {
		t.Fatalf("显式 ID 不符: %d 条水印", len(annots))
	}

	var conflict bytes.Buffer
	err = Add(data, Target{}, Watermark{ID: 101, Creator: "t"}, &conflict, Options{})
	if err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("冲突 ID 应报错, got %v", err)
	}
}

func TestReplaceWatermarkPreservesIDAndOtherAnnots(t *testing.T) {
	var buffer bytes.Buffer
	err := Replace(readTestdata(t, "annotations.ofd"), Target{}, sampleWatermark(), &buffer, Options{SkipReadOnlyCheck: true})
	if err != nil {
		t.Fatalf("Replace 失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())
	root := parseXML(t, readEntry(t, result, "Doc_0/Annotations/Page_1.xml")).Root()
	var annots, stamps []*etree.Element
	for _, child := range root.ChildElements() {
		switch attr(child, "Type") {
		case "Watermark":
			annots = append(annots, child)
		case "Stamp":
			stamps = append(stamps, child)
		}
	}
	if len(annots) != 1 || len(stamps) != 1 {
		t.Fatalf("期望 1 水印 + 1 印章, 得到 %d/%d", len(annots), len(stamps))
	}
	if got := attr(annots[0], "ID"); got != "101" {
		t.Fatalf("Replace 应保留 ID 101, 得到 %s", got)
	}
	if got := attr(annots[0], "Creator"); got != "watermark-test" {
		t.Fatalf("Replace 后 Creator 不符: %s", got)
	}
	if got := attr(stamps[0], "ID"); got != "102" {
		t.Fatalf("印章注解不应被修改: %s", got)
	}
}

func TestReplaceByMatchIDs(t *testing.T) {
	var buffer bytes.Buffer
	err := Replace(readTestdata(t, "annotations.ofd"), Target{MatchIDs: []uint64{101}}, sampleWatermark(), &buffer, Options{SkipReadOnlyCheck: true})
	if err != nil {
		t.Fatalf("按 ID 替换失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())
	annots := findWatermarks(t, readEntry(t, result, "Doc_0/Annotations/Page_1.xml"))
	if len(annots) != 1 || attr(annots[0], "ID") != "101" {
		t.Fatalf("按 ID 替换结果不符")
	}

	var miss bytes.Buffer
	err = Replace(readTestdata(t, "annotations.ofd"), Target{MatchIDs: []uint64{999}}, sampleWatermark(), &miss, Options{})
	if err == nil || !strings.Contains(err.Error(), "未找到匹配的水印") {
		t.Fatalf("无匹配应报错, got %v", err)
	}
}

func TestRemoveWatermarkKeepsOtherAnnots(t *testing.T) {
	var buffer bytes.Buffer
	err := Remove(readTestdata(t, "annotations.ofd"), Target{}, &buffer, Options{SkipReadOnlyCheck: true})
	if err != nil {
		t.Fatalf("Remove 失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())
	root := parseXML(t, readEntry(t, result, "Doc_0/Annotations/Page_1.xml")).Root()
	var annots, stamps []*etree.Element
	for _, child := range root.ChildElements() {
		switch attr(child, "Type") {
		case "Watermark":
			annots = append(annots, child)
		case "Stamp":
			stamps = append(stamps, child)
		}
	}
	if len(annots) != 0 || len(stamps) != 1 {
		t.Fatalf("期望删除水印保留印章, 得到 %d/%d", len(annots), len(stamps))
	}
	// 剩余注解时索引与 Document.xml 声明应保留。
	if !result.Has("Doc_0/Annotations.xml") {
		t.Fatal("索引不应被删除")
	}
	docRoot := parseXML(t, readEntry(t, result, "Doc_0/Document.xml")).Root()
	if findAnnotationsElement(docRoot) == nil {
		t.Fatal("Document.xml 不应删除 <Annotations>")
	}
}

func TestRemoveReadOnlyBlocked(t *testing.T) {
	var buffer bytes.Buffer
	err := Remove(readTestdata(t, "annotations.ofd"), Target{}, &buffer, Options{})
	if err == nil || !strings.Contains(err.Error(), "只读") {
		t.Fatalf("只读水印应拒绝删除, got %v", err)
	}
	var skipped bytes.Buffer
	if err := Remove(readTestdata(t, "annotations.ofd"), Target{}, &skipped, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("SkipReadOnlyCheck 应跳过只读检查: %v", err)
	}
}

func TestAddRemoveRoundTripCleansUp(t *testing.T) {
	hello := readTestdata(t, "hello.ofd")
	var added bytes.Buffer
	if err := Add(hello, Target{}, sampleWatermark(), &added, Options{}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	intermediate := added.Bytes()

	// Add 产生的 ReadOnly=false 允许默认选项直接删除。
	var removed bytes.Buffer
	if err := Remove(intermediate, Target{}, &removed, Options{}); err != nil {
		t.Fatalf("Remove 失败: %v", err)
	}
	result := openPackage(t, removed.Bytes())
	if result.Has("Doc_0/Annotations.xml") {
		t.Fatal("空索引应被删除")
	}
	if result.Has("Doc_0/Annotations/Page_1.xml") {
		t.Fatal("空页面注解文件应被删除")
	}
	docRoot := parseXML(t, readEntry(t, result, "Doc_0/Document.xml")).Root()
	if findAnnotationsElement(docRoot) != nil {
		t.Fatal("Document.xml 应删除 <Annotations>")
	}
}

func TestPermissionsWatermarkFalseBlocks(t *testing.T) {
	hello := readTestdata(t, "hello.ofd")
	docData := documentWithWatermarkPermission(t, hello, false)

	var blocked bytes.Buffer
	err := Add(docData, Target{}, sampleWatermark(), &blocked, Options{})
	if err == nil || !strings.Contains(err.Error(), "禁止") {
		t.Fatalf("Permissions/Watermark=false 应拒绝, got %v", err)
	}
	var skipped bytes.Buffer
	if err := Add(docData, Target{}, sampleWatermark(), &skipped, Options{SkipPermissionsCheck: true}); err != nil {
		t.Fatalf("SkipPermissionsCheck 应跳过权限检查: %v", err)
	}
}

func TestOfdPrefixPreserved(t *testing.T) {
	// 999.ofd 使用 ofd: 前缀，目标页面 92 尚无注解文件。
	// Appearance 中的原始片段也须套用 ofd 前缀，避免写出空命名空间元素。
	var buffer bytes.Buffer
	err := Add(readTestdata(t, "999.ofd"), Target{Pages: []int{1}}, Watermark{
		Creator:     "watermark-test",
		LastModDate: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
		Appearance:  []byte(`<TextObject ID="900" Boundary="0 0 10 10" Font="20" Size="9"><TextCode X="0" Y="0">保密</TextCode></TextObject>`),
	}, &buffer, Options{})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())

	page := parseXML(t, readEntry(t, result, "Doc_0/Annots/Page_92.xml")).Root()
	if page.Space != "ofd" {
		t.Fatalf("新页面注解文件应保留 ofd 前缀, space=%q", page.Space)
	}
	annots := findWatermarks(t, readEntry(t, result, "Doc_0/Annots/Page_92.xml"))
	if len(annots) != 1 {
		t.Fatalf("期望 1 条水印, 得到 %d", len(annots))
	}
	if got := attr(annots[0], "ID"); got != "790" {
		t.Fatalf("自动 ID 应为 790, 得到 %s", got)
	}
	// 原始外观片段中的无前缀元素必须带上 ofd 前缀。
	raw := readEntry(t, result, "Doc_0/Annots/Page_92.xml")
	if bytes.Contains(raw, []byte("<TextObject")) {
		t.Fatalf("外观片段不应写出无前缀 TextObject:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("<ofd:TextObject")) {
		t.Fatalf("外观片段应使用 ofd 前缀:\n%s", raw)
	}
	indexRoot := parseXML(t, readEntry(t, result, "Doc_0/Annots/Annotations.xml")).Root()
	if indexRoot.Space != "ofd" {
		t.Fatalf("索引应保留 ofd 前缀, space=%q", indexRoot.Space)
	}
	var fileLoc string
	for _, pageEl := range indexRoot.ChildElements() {
		if attr(pageEl, "PageID") == "92" {
			fileLoc = strings.TrimSpace(pageEl.FindElement("FileLoc").Text())
		}
	}
	if fileLoc != "Page_92.xml" {
		t.Fatalf("新页面 FileLoc 不符: %s", fileLoc)
	}
}

func TestBadAppearanceReturnsError(t *testing.T) {
	// 外观片段无法解析时必须报错，而不是静默写成空 <Appearance/>。
	hello := readTestdata(t, "hello.ofd")
	var buffer bytes.Buffer
	err := Add(hello, Target{}, Watermark{
		Creator:    "watermark-test",
		Appearance: []byte(`<TextObject ID="1" Boundary="0 0 1 1"`),
	}, &buffer, Options{})
	if err == nil || !strings.Contains(err.Error(), "外观") {
		t.Fatalf("非法外观应报错, got %v", err)
	}
}

func TestInputTypes(t *testing.T) {
	hello := readTestdata(t, "hello.ofd")

	var fromBytes bytes.Buffer
	if err := Add(hello, Target{}, sampleWatermark(), &fromBytes, Options{}); err != nil {
		t.Fatalf("[]byte 输入失败: %v", err)
	}

	var fromReader bytes.Buffer
	if err := Add(bytes.NewReader(hello), Target{}, sampleWatermark(), &fromReader, Options{}); err != nil {
		t.Fatalf("io.Reader 输入失败: %v", err)
	}

	pkg, err := core.OpenBytes(hello)
	if err != nil {
		t.Fatalf("打开输入失败: %v", err)
	}
	var fromPkg bytes.Buffer
	if err := Add(pkg, Target{}, sampleWatermark(), &fromPkg, Options{}); err != nil {
		t.Fatalf("*core.Package 输入失败: %v", err)
	}
}

func TestTextAppearanceTileLayout(t *testing.T) {
	data, err := TextAppearance(TextOptions{
		Text:     "保密资料",
		Size:     9,
		Boundary: creator.Box{Width: 210, Height: 297},
		Layout:   LayoutTile,
	})
	if err != nil {
		t.Fatalf("TextAppearance 失败: %v", err)
	}
	root := parseXML(t, data).Root()
	if root.Tag != "TextObject" {
		t.Fatalf("根标签不符: %s", root.Tag)
	}
	codes := root.FindElements("TextCode")
	if len(codes) < 4 {
		t.Fatalf("平铺应生成多个 TextCode, 得到 %d:\n%s", len(codes), data)
	}
	var previousY float64
	for i, code := range codes {
		y, _ := strconv.ParseFloat(attr(code, "Y"), 64)
		if y < 0 || y > 297 {
			t.Fatalf("TextCode %d Y 越界: %s", i, attr(code, "Y"))
		}
		if i > 0 && y < previousY {
			t.Fatalf("TextCode 行序错误")
		}
		previousY = y
	}
}

func TestTextAppearanceCenterAndOpacity(t *testing.T) {
	opacity := uint8(127)
	data, err := TextAppearance(TextOptions{
		Text:     "保密资料",
		Size:     9,
		Boundary: creator.Box{Width: 210, Height: 297},
		Layout:   LayoutCenter,
		Opacity:  &opacity,
	})
	if err != nil {
		t.Fatalf("TextAppearance 失败: %v", err)
	}
	root := parseXML(t, data).Root()
	codes := root.FindElements("TextCode")
	if len(codes) != 1 {
		t.Fatalf("居中应生成单个 TextCode, 得到 %d", len(codes))
	}
	if got := attr(codes[0], "X"); got != "87" {
		t.Fatalf("居中 X 应为 87, 得到 %s", got)
	}
	if got := attr(codes[0], "Y"); got != "144" {
		t.Fatalf("居中 Y 应为 144, 得到 %s", got)
	}
	if got := attr(root, "Alpha"); got != "127" {
		t.Fatalf("Alpha 属性不符: %s", got)
	}
}

func TestTextAppearanceRotation(t *testing.T) {
	data, err := TextAppearance(TextOptions{
		Text:     "保密资料",
		Size:     9,
		Boundary: creator.Box{Width: 210, Height: 297},
		Layout:   LayoutCenter,
		Rotation: 45,
	})
	if err != nil {
		t.Fatalf("TextAppearance 失败: %v", err)
	}
	root := parseXML(t, data).Root()
	// +45 为屏幕顺时针（左边往上、右边往下）：渲染器按 CTM 显示，
	// TextCode 起点做等价预转抵消，字形绕文本中心旋转、位置不飘。
	if got := attr(root, "CTM"); got != "0.7071 0.7071 -0.7071 0.7071 0 0" {
		t.Fatalf("旋转 45 度 CTM 不符: %s", got)
	}
	codes := root.FindElements("TextCode")
	if len(codes) != 1 {
		t.Fatalf("居中应生成单个 TextCode, 得到 %d", len(codes))
	}
	if got := attr(codes[0], "X"); got != "155.524" {
		t.Fatalf("旋转后 X 应为 155.524, 得到 %s", got)
	}
	if got := attr(codes[0], "Y"); got != "28.6316" {
		t.Fatalf("旋转后 Y 应为 28.6316, 得到 %s", got)
	}
}

func TestTextAppearanceLocalCoordinates(t *testing.T) {
	data, err := TextAppearance(TextOptions{
		Text:     "保密资料",
		Size:     9,
		Boundary: creator.Box{X: 10, Y: 20, Width: 210, Height: 297},
		Layout:   LayoutCenter,
	})
	if err != nil {
		t.Fatalf("TextAppearance 失败: %v", err)
	}
	root := parseXML(t, data).Root()
	if got := attr(root, "Boundary"); got != "0 0 210 297" {
		t.Fatalf("文字对象边界应为局部坐标, 得到 %s", got)
	}
	codes := root.FindElements("TextCode")
	if got := attr(codes[0], "X"); got != "87" {
		t.Fatalf("外观原点不应进入文字坐标, X=%s", got)
	}
	if got := attr(codes[0], "Y"); got != "144" {
		t.Fatalf("外观原点不应进入文字坐标, Y=%s", got)
	}

	plain, err := TextAppearance(TextOptions{
		Text:     "保密资料",
		Size:     9,
		Boundary: creator.Box{Width: 210, Height: 297},
		Layout:   LayoutTile,
	})
	if err != nil {
		t.Fatalf("平铺外观失败: %v", err)
	}
	plainCodes := parseXML(t, plain).Root().FindElements("TextCode")
	if len(plainCodes) < 4 {
		t.Fatalf("平铺数量不足: %d", len(plainCodes))
	}
	for _, rotation := range []float64{45, -45} {
		rotated, rotErr := TextAppearance(TextOptions{
			Text:     "保密资料",
			Size:     9,
			Boundary: creator.Box{Width: 210, Height: 297},
			Layout:   LayoutTile,
			Rotation: rotation,
		})
		if rotErr != nil {
			t.Fatalf("旋转平铺外观失败: %v", rotErr)
		}
		rotatedCodes := parseXML(t, rotated).Root().FindElements("TextCode")
		if len(rotatedCodes) != len(plainCodes) {
			t.Fatalf("旋转 %g 后平铺数量变化: %d %d", rotation, len(plainCodes), len(rotatedCodes))
		}
		rad := rotation * math.Pi / 180
		cosR, sinR := math.Cos(rad), math.Sin(rad)
		var dx0, dy0 float64
		for i := 0; i < len(plainCodes); i++ {
			px, pxErr := strconv.ParseFloat(attr(plainCodes[i], "X"), 64)
			py, pyErr := strconv.ParseFloat(attr(plainCodes[i], "Y"), 64)
			x, xErr := strconv.ParseFloat(attr(rotatedCodes[i], "X"), 64)
			y, yErr := strconv.ParseFloat(attr(rotatedCodes[i], "Y"), 64)
			if pxErr != nil || pyErr != nil || xErr != nil || yErr != nil {
				t.Fatalf("旋转 %g 平铺坐标无法解析", rotation)
			}
			tx := x*cosR - y*sinR
			ty := x*sinR + y*cosR
			if i == 0 {
				dx0, dy0 = tx-px, ty-py
				continue
			}
			if math.Abs(tx-px-dx0) > 0.05 || math.Abs(ty-py-dy0) > 0.05 {
				t.Fatalf("旋转 %g 后第 %d 个文本离开了平铺网格: %g,%g", rotation, i, tx-px, ty-py)
			}
		}
	}
}

func codeDelta(t *testing.T, rotated, plain *etree.Element, name string) float64 {
	t.Helper()
	left, err := strconv.ParseFloat(attr(rotated, name), 64)
	if err != nil {
		t.Fatalf("解析旋转坐标失败: %v", err)
	}
	right, err := strconv.ParseFloat(attr(plain, name), 64)
	if err != nil {
		t.Fatalf("解析坐标失败: %v", err)
	}
	return left - right
}

func TestImageAppearanceLocalCoordinates(t *testing.T) {
	data, err := ImageAppearance(ImageOptions{
		ImageID:  7,
		Width:    40,
		Height:   20,
		Boundary: creator.Box{X: 30, Y: 40, Width: 210, Height: 297},
		Layout:   LayoutCenter,
	})
	if err != nil {
		t.Fatalf("ImageAppearance 失败: %v", err)
	}
	boxes := parseXML(t, data).ChildElements()
	if len(boxes) != 1 {
		t.Fatalf("图片居中应生成单个 ImageObject, 得到 %d", len(boxes))
	}
	if got := attr(boxes[0], "Boundary"); got != "85 138.5 40 20" {
		t.Fatalf("外观原点不应进入图片坐标, 得到 %s", got)
	}
}

func coordDelta(t *testing.T, rotated, plain *etree.Element, name string) float64 {
	t.Helper()
	left, err := strconv.ParseFloat(attr(rotated, name), 64)
	if err != nil {
		t.Fatalf("解析旋转坐标失败: %v", err)
	}
	right, err := strconv.ParseFloat(attr(plain, name), 64)
	if err != nil {
		t.Fatalf("解析坐标失败: %v", err)
	}
	return left - right
}

func TestTextAppearanceRotationIgnoredWithCTM(t *testing.T) {
	ctm := [6]float64{1, 0, 0, 1, 0, 0}
	data, err := TextAppearance(TextOptions{
		Text:     "保密资料",
		Size:     9,
		Boundary: creator.Box{Width: 210, Height: 297},
		Layout:   LayoutCenter,
		Rotation: 45,
		CTM:      ctm,
	})
	if err != nil {
		t.Fatalf("TextAppearance 失败: %v", err)
	}
	root := parseXML(t, data).Root()
	if got := attr(root, "CTM"); got != "1 0 0 1 0 0" {
		t.Fatalf("显式 CTM 应优先于 Rotation, 得到 %s", got)
	}
	codes := root.FindElements("TextCode")
	if got := attr(codes[0], "X"); got != "87" {
		t.Fatalf("显式 CTM 时位置不应旋转, 得到 %s", got)
	}
}

func TestTextAppearanceManualDefaultPosition(t *testing.T) {
	data, err := TextAppearance(TextOptions{Text: "保密资料", Size: 9})
	if err != nil {
		t.Fatalf("TextAppearance 失败: %v", err)
	}
	root := parseXML(t, data).Root()
	codes := root.FindElements("TextCode")
	if len(codes) != 1 {
		t.Fatalf("手动布局应生成单个 TextCode, 得到 %d", len(codes))
	}
	if got := attr(codes[0], "X"); got != "8" {
		t.Fatalf("默认 X 应为 8, 得到 %s", got)
	}
	if got := attr(codes[0], "Y"); got != "10" {
		t.Fatalf("默认 Y 应为 10, 得到 %s", got)
	}
}

func TestImageAppearance(t *testing.T) {
	tile, err := ImageAppearance(ImageOptions{
		ImageID:  7,
		Width:    40,
		Height:   20,
		Boundary: creator.Box{Width: 210, Height: 297},
		Layout:   LayoutTile,
	})
	if err != nil {
		t.Fatalf("ImageAppearance 失败: %v", err)
	}
	tileDoc := parseXML(t, tile)
	tileBoxes := tileDoc.ChildElements()
	if len(tileBoxes) < 4 {
		t.Fatalf("图片平铺应生成多个 ImageObject, 得到 %d", len(tileBoxes))
	}
	for i, box := range tileBoxes {
		if got := attr(box, "ResourceID"); got != "7" {
			t.Fatalf("ImageObject %d ResourceID 不符: %s", i, got)
		}
	}

	center, err := ImageAppearance(ImageOptions{
		ImageID:  7,
		Width:    40,
		Height:   20,
		Boundary: creator.Box{Width: 210, Height: 297},
		Layout:   LayoutCenter,
	})
	if err != nil {
		t.Fatalf("ImageAppearance center 失败: %v", err)
	}
	centerDoc := parseXML(t, center)
	centerBoxes := centerDoc.ChildElements()
	if len(centerBoxes) != 1 {
		t.Fatalf("图片居中应生成单个 ImageObject, 得到 %d", len(centerBoxes))
	}
	if got := attr(centerBoxes[0], "Boundary"); got != "85 138.5 40 20" {
		t.Fatalf("居中边界不符: %s", got)
	}
}

func TestAddImageWatermark(t *testing.T) {
	opacity := uint8(128)
	image := &Image{
		Data:    testPNG(t, 120, 60),
		Width:   40,
		Layout:  LayoutTile,
		Opacity: &opacity,
	}
	var buffer bytes.Buffer
	err := Add(readTestdata(t, "hello.ofd"), Target{}, Watermark{
		Creator:     "watermark-test",
		LastModDate: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
		Boundary:    &creator.Box{Width: 210, Height: 297},
		Image:       image,
	}, &buffer, Options{})
	if err != nil {
		t.Fatalf("Add 图片水印失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())

	// DocumentRes 中注册了图片媒体并引用了存在的图片条目。
	resData := readEntry(t, result, "Doc_0/DocumentRes.xml")
	res := parseXML(t, resData).Root()
	mediaContainer := findElement(res, "MultiMedias")
	if mediaContainer == nil {
		t.Fatal("DocumentRes 缺少 MultiMedias")
	}
	medias := mediaContainer.ChildElements()
	if len(medias) != 1 || attr(medias[0], "Type") != "Image" || attr(medias[0], "Format") != "PNG" {
		t.Fatalf("图片媒体注册不符: %s", resData)
	}
	mediaFile := strings.TrimSpace(medias[0].FindElement("MediaFile").Text())
	if mediaFile == "" {
		t.Fatal("媒体文件缺失")
	}
	if !result.Has("Doc_0/Res/" + mediaFile) {
		t.Fatalf("缺少图片条目 %s", mediaFile)
	}

	// 页面注解引用了图片资源，且注释与图片 ID 不冲突。
	annots := findWatermarks(t, readEntry(t, result, "Doc_0/Annotations/Page_1.xml"))
	if len(annots) != 1 {
		t.Fatalf("期望 1 条水印, 得到 %d", len(annots))
	}
	appearance := annots[0].FindElement("Appearance")
	imageObjects := appearance.ChildElements()
	if len(imageObjects) < 4 {
		t.Fatalf("平铺应生成多个 ImageObject, 得到 %d", len(imageObjects))
	}
	imageObject := imageObjects[0]
	if got := attr(imageObject, "ResourceID"); got != attr(medias[0], "ID") {
		t.Fatalf("ImageObject 引用 %s 与媒体 ID %s 不符", got, attr(medias[0], "ID"))
	}
	annotID := attr(annots[0], "ID")
	if annotID == attr(medias[0], "ID") {
		t.Fatalf("注解 ID %s 与图片资源 ID 冲突", annotID)
	}
}

func TestBakeImageAlpha(t *testing.T) {
	source := testPNG(t, 4, 4)
	baked, err := bakeImageAlpha(source, 64)
	if err != nil {
		t.Fatalf("bakeImageAlpha 失败: %v", err)
	}
	if bytes.Equal(baked, source) {
		t.Fatal("烘焙后数据不应与原始一致")
	}
	if got, err := bakeImageAlpha(source, 255); err != nil || !bytes.Equal(got, source) {
		t.Fatalf("opacity=255 应原样返回: %v", err)
	}
}

func TestBakeImageAlphaPreservesOpaqueColor(t *testing.T) {
	// 半透明像素烘焙 alpha 只应改动不透明度，不得把非预乘颜色再次预乘而变暗。
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 50, B: 80, A: 128})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, src); err != nil {
		t.Fatal(err)
	}
	baked, err := bakeImageAlpha(buffer.Bytes(), 128)
	if err != nil {
		t.Fatalf("bakeImageAlpha 失败: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(baked))
	if err != nil {
		t.Fatal(err)
	}
	got := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	if got.R < 190 || got.G > 60 || got.B > 90 {
		t.Fatalf("烘焙后颜色被篡改: got %+v, want ~(200,50,80,64)", got)
	}
	if got.A < 58 || got.A > 70 {
		t.Fatalf("烘焙后 alpha 不符: got %d, want ~64", got.A)
	}
}

func TestPrefixDerivedFromDocumentWhenNoIndex(t *testing.T) {
	// 999.ofd 使用 ofd: 前缀但没有既有注解索引，新索引与页面文件应从
	// Document.xml 根元素继承前缀。
	var buffer bytes.Buffer
	err := Add(readTestdata(t, "999.ofd"), Target{Pages: []int{0}}, Watermark{
		Creator:     "watermark-test",
		LastModDate: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
	}, &buffer, Options{})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	result := openPackage(t, buffer.Bytes())
	indexRoot := parseXML(t, readEntry(t, result, "Doc_0/Annots/Annotations.xml")).Root()
	if indexRoot.Space != "ofd" {
		t.Fatalf("无索引时新索引应继承文档体 ofd 前缀, space=%q", indexRoot.Space)
	}
	for _, pageEl := range indexRoot.ChildElements() {
		loc := strings.TrimSpace(pageEl.FindElement("FileLoc").Text())
		pageRoot := parseXML(t, readEntry(t, result, "Doc_0/Annots/"+loc)).Root()
		if pageRoot.Space != "ofd" {
			t.Fatalf("无索引时新页面应继承 ofd 前缀, space=%q", pageRoot.Space)
		}
	}
}

func TestRemoveImageWatermarkReclaimsMedia(t *testing.T) {
	hello := readTestdata(t, "hello.ofd")
	var added bytes.Buffer
	if err := Add(hello, Target{}, Watermark{
		Creator: "watermark-test",
		Image:   &Image{Data: testPNG(t, 40, 20)},
	}, &added, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("Add 图片水印失败: %v", err)
	}
	withImage := openPackage(t, added.Bytes())
	if !withImage.Has("Doc_0/Res/Images/Image_6.png") {
		t.Fatal("图片水印应写入图片条目")
	}
	_ = withImage.Close()

	var removed bytes.Buffer
	if err := Remove(added.Bytes(), Target{}, &removed, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("Remove 失败: %v", err)
	}
	result := openPackage(t, removed.Bytes())
	if result.Has("Doc_0/Res/Images/Image_6.png") {
		t.Fatal("删除水印后应回收图片条目")
	}
	resData := readEntry(t, result, "Doc_0/DocumentRes.xml")
	if bytes.Contains(resData, []byte("Image_6.png")) {
		t.Fatalf("DocumentRes 应移除媒体引用:\n%s", resData)
	}
	if bytes.Contains(resData, []byte("MultiMedias")) {
		t.Fatalf("空 MultiMedias 容器应被移除:\n%s", resData)
	}
}

func TestReplaceImageWatermarkReclaimsOldMedia(t *testing.T) {
	// 用新图片替换图片水印时，旧图片资源应被回收，只保留新图片。
	hello := readTestdata(t, "hello.ofd")
	var added bytes.Buffer
	if err := Add(hello, Target{}, Watermark{
		Creator: "watermark-test",
		Image:   &Image{Data: testPNG(t, 40, 20)},
	}, &added, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("Add 图片水印失败: %v", err)
	}

	var replaced bytes.Buffer
	if err := Replace(added.Bytes(), Target{}, Watermark{
		Creator: "watermark-test",
		Image:   &Image{Data: testPNG(t, 60, 30)},
	}, &replaced, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("Replace 图片水印失败: %v", err)
	}
	afterReplace := openPackage(t, replaced.Bytes())
	medias := findElement(parseXML(t, readEntry(t, afterReplace, "Doc_0/DocumentRes.xml")).Root(), "MultiMedias")
	if medias == nil || len(medias.ChildElements()) != 1 {
		t.Fatalf("替换后应只保留一条媒体: %s", readEntry(t, afterReplace, "Doc_0/DocumentRes.xml"))
	}
	imageCount := 0
	for _, entry := range afterReplace.Entries() {
		if strings.HasPrefix(entry.Name, "Doc_0/Res/Images/") {
			imageCount++
		}
	}
	if imageCount != 1 {
		t.Fatalf("替换后应只保留一个图片条目, 得到 %d", imageCount)
	}
}

func TestRemoveImageWatermarkKeepsSharedMedia(t *testing.T) {
	// 同一图片资源被多页引用时，只删除其中一页不应回收资源。
	data := readTestdata(t, "1000-pages.ofd")
	var added bytes.Buffer
	if err := Add(data, Target{}, Watermark{
		Creator:     "watermark-test",
		LastModDate: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
		Image:       &Image{Data: testPNG(t, 40, 20)},
	}, &added, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("Add 图片水印失败: %v", err)
	}

	// 仅删除第 0 页的水印，其余页面仍引用同一媒体资源。
	var removed bytes.Buffer
	if err := Remove(added.Bytes(), Target{Pages: []int{0}}, &removed, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("按页删除失败: %v", err)
	}
	result := openPackage(t, removed.Bytes())
	doc := parseXML(t, readEntry(t, result, "Doc_0/DocumentRes.xml")).Root()
	medias := findElement(doc, "MultiMedias")
	if medias == nil || len(medias.ChildElements()) != 1 {
		t.Fatalf("仍被引用时不应回收媒体: %s", readEntry(t, result, "Doc_0/DocumentRes.xml"))
	}
	// 其余页的图片水印仍引用同一媒体资源。
	indexRoot := parseXML(t, readEntry(t, result, "Doc_0/Annotations.xml")).Root()
	var otherEntry string
	for _, pageEl := range indexRoot.ChildElements() {
		if attr(pageEl, "PageID") == "1" {
			continue
		}
		otherEntry = strings.TrimSpace(pageEl.FindElement("FileLoc").Text())
		break
	}
	if otherEntry == "" {
		t.Fatal("索引中应仍有其它页面注解")
	}
	other := readEntry(t, result, "Doc_0/"+otherEntry)
	if !bytes.Contains(other, []byte("ImageObject")) {
		t.Fatalf("其余页应保留图片水印:\n%s", other)
	}
}

func TestAnnotIDAvoidsMediaID(t *testing.T) {
	// 图片资源 ID 与注解共享空间：先加水印图片再在同一文档追加文本水印时，
	// 新注解 ID 不得与既有媒体资源 ID 冲突。
	hello := readTestdata(t, "hello.ofd")
	var withImage bytes.Buffer
	if err := Add(hello, Target{}, Watermark{
		Creator: "watermark-test",
		Image:   &Image{Data: testPNG(t, 40, 20)},
	}, &withImage, Options{SkipReadOnlyCheck: true}); err != nil {
		t.Fatalf("Add 图片水印失败: %v", err)
	}

	var withText bytes.Buffer
	if err := Add(withImage.Bytes(), Target{}, Watermark{
		Creator:    "watermark-test",
		Appearance: []byte(`<TextObject ID="1" Boundary="0 0 10 10" Font="4" Size="9"><TextCode X="0" Y="0">T</TextCode></TextObject>`),
	}, &withText, Options{}); err != nil {
		t.Fatalf("追加文本水印失败: %v", err)
	}
	result := openPackage(t, withText.Bytes())
	doc := parseXML(t, readEntry(t, result, "Doc_0/DocumentRes.xml")).Root()
	var mediaID uint64
	for _, media := range findElement(doc, "MultiMedias").ChildElements() {
		if id, err := strconv.ParseUint(attr(media, "ID"), 10, 64); err == nil {
			mediaID = id
		}
	}
	for _, annot := range findWatermarks(t, readEntry(t, result, "Doc_0/Annotations/Page_1.xml")) {
		if id, err := strconv.ParseUint(attr(annot, "ID"), 10, 64); err == nil && id == mediaID {
			t.Fatalf("注解 ID %d 与媒体资源 ID 冲突", id)
		}
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 80, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("编码 PNG 失败: %v", err)
	}
	return buffer.Bytes()
}

func TestErrors(t *testing.T) {
	hello := readTestdata(t, "hello.ofd")

	if err := Add(hello, Target{}, sampleWatermark(), nil, Options{}); err == nil {
		t.Fatal("nil 写入器应报错")
	}
	var buffer bytes.Buffer
	if err := Add(hello, Target{Document: 3}, sampleWatermark(), &buffer, Options{}); err == nil ||
		!strings.Contains(err.Error(), "超出范围") {
		t.Fatalf("文档体越界应报错, got %v", err)
	}
	var pageErr bytes.Buffer
	if err := Add(hello, Target{Pages: []int{9}}, sampleWatermark(), &pageErr, Options{}); err == nil ||
		!strings.Contains(err.Error(), "超出范围") {
		t.Fatalf("页面越界应报错, got %v", err)
	}
}

func documentWithWatermarkPermission(t *testing.T, data []byte, allowed bool) []byte {
	t.Helper()
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	docData, err := pkg.Read("Doc_0/Document.xml")
	if err != nil {
		t.Fatal(err)
	}
	doc := parseXML(t, docData)
	root := doc.Root()
	perm := findElement(root, "Permissions")
	if perm == nil {
		perm = root.CreateElement("Permissions")
	}
	watermarkEl := findElement(perm, "Watermark")
	if watermarkEl == nil {
		watermarkEl = perm.CreateElement("Watermark")
	}
	watermarkEl.SetText(strconv.FormatBool(allowed))
	updated, err := doc.WriteToBytes()
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := replace.Files(data, []replace.Operation{
		{Kind: replace.OpSet, Name: "Doc_0/Document.xml", Data: updated},
	}, &buffer, replace.Options{}); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func findElement(parent *etree.Element, tag string) *etree.Element {
	for _, child := range parent.ChildElements() {
		if child.Tag == tag {
			return child
		}
	}
	return nil
}
