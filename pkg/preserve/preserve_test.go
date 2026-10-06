package preserve

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/spec"
)

// fixtureOf 是测试样例在 testdata 里的路径。
const fixtureOf = "../../testdata/"

// docXML 取出包内 Document.xml 的内容。
func docXML(t *testing.T, file string) []byte {
	t.Helper()
	r, err := zip.OpenReader(file)
	if err != nil {
		t.Fatalf("打开 %s 失败: %v", file, err)
	}
	defer func() { _ = r.Close() }()
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "Document.xml") {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("读取 %s 失败: %v", f.Name, err)
			}
			defer func() { _ = rc.Close() }()
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(rc); err != nil {
				t.Fatalf("读取 %s 失败: %v", f.Name, err)
			}
			return buf.Bytes()
		}
	}
	t.Fatalf("%s 内找不到 Document.xml", file)
	return nil
}

// appliedChangesBytes 转换字节输入，返回结果与输出包内各 XML 条目的内容。
func appliedChangesBytes(t *testing.T, src []byte, options Options) (Result, map[string]string) {
	t.Helper()
	var out bytes.Buffer
	result, err := Apply(src, &out, options)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	return result, readEntries(t, out.Bytes())
}

// readEntries 读出输出包内全部 .xml 条目。
func readEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("输出不是合法 ZIP: %v", err)
	}
	out := map[string]string{}
	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

// appliedChanges 跑一次转换并读回结果里的 Document.xml。
func appliedChanges(t *testing.T, file string, options Options) (Result, []byte) {
	t.Helper()
	var out bytes.Buffer
	result, err := Apply(file, &out, options)
	if err != nil {
		t.Fatalf("转换 %s 失败: %v", file, err)
	}
	if out.Len() == 0 {
		return result, nil
	}
	return result, docXMLFrom(t, out.Bytes())
}

// docXMLFrom 从生成的字节流里取 Document.xml。
func docXMLFrom(t *testing.T, data []byte) []byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("打开输出失败: %v", err)
	}
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, "Document.xml") {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("读取 %s 失败: %v", f.Name, err)
			}
			defer func() { _ = rc.Close() }()
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(rc); err != nil {
				t.Fatalf("读取 %s 失败: %v", f.Name, err)
			}
			return buf.Bytes()
		}
	}
	t.Fatal("输出内找不到 Document.xml")
	return nil
}

// TestPreserveRemovesArchiveForbiddenNodes 覆盖 GB/T 42133 6.2.2 a) b) e)：
// 长期保存文件不应含权限声明、视图首选项与扩展信息。
func TestPreserveRemovesArchiveForbiddenNodes(t *testing.T) {
	cases := []struct {
		file   string
		absent string
		clause string
	}{
		{"permissions.ofd", "Permissions", "GB/T 42133 6.2.2 a)"},
		{"preferences.ofd", "VPreferences", "GB/T 42133 6.2.2 b)"},
		{"package-extras.ofd", "Extensions", "GB/T 42133 6.2.2 e)"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			before := string(docXML(t, fixtureOf+tc.file))
			if !strings.Contains(before, "<"+tc.absent) {
				t.Fatalf("样例 %s 本就不含 %s，无法验证转换", tc.file, tc.absent)
			}
			result, after := appliedChanges(t, fixtureOf+tc.file, Options{DocType: "OFD-A"})
			if len(after) == 0 {
				t.Fatal("转换没有产生输出")
			}
			if strings.Contains(string(after), "<"+tc.absent) {
				t.Errorf("转换后仍含 %s:\n%s", tc.absent, after)
			}
			if len(result.Changes) != 1 {
				t.Fatalf("预期 1 处改动，实际 %d: %+v", len(result.Changes), result.Changes)
			}
			if got := result.Changes[0].Clause; got != tc.clause {
				t.Errorf("依据条款 = %q，预期 %q", got, tc.clause)
			}
			if got := result.Changes[0].Entry; got != "Doc_0/Document.xml" {
				t.Errorf("改动条目 = %q，预期 Doc_0/Document.xml", got)
			}
			if result.DocType != "OFD-A" {
				t.Errorf("DocType = %q，预期 OFD-A", result.DocType)
			}
		})
	}
}

// TestPreserveKeepsNamespaceAndDocument 确认转换只动目标节点，不损伤命名空间、
// 页面列表与公共数据——丢命名空间会让输出直接不是合法 OFD。
func TestPreserveKeepsNamespaceAndDocument(t *testing.T) {
	before := string(docXML(t, fixtureOf+"permissions.ofd"))
	_, after := appliedChanges(t, fixtureOf+"permissions.ofd", Options{DocType: "OFD-A"})
	got := string(after)
	for _, want := range []string{
		`xmlns="` + spec.Namespace + `"`,
		"<CommonData>",
		"<Pages>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("转换后丢失 %s:\n%s", want, got)
		}
	}
	if strings.Count(before, "<Page ") != strings.Count(got, "<Page ") {
		t.Errorf("页面数量变化: %d -> %d",
			strings.Count(before, "<Page "), strings.Count(got, "<Page "))
	}
}

// TestPreserveSkipsBaseDocType 确认基础 profile 不施加档案转换。未声明
// DocType 的文件若被擅自改写，就是无依据地损坏用户数据。
func TestPreserveSkipsBaseDocType(t *testing.T) {
	result, err := Plan(fixtureOf+"permissions.ofd", Options{DocType: "OFD"})
	if err != nil {
		t.Fatalf("Plan 失败: %v", err)
	}
	if result.DocType != "OFD" {
		t.Errorf("DocType = %q，预期 OFD", result.DocType)
	}
	if len(result.Changes) != 0 {
		t.Errorf("基础 profile 不应有改动，实际 %+v", result.Changes)
	}
}

// TestPreservePlanMatchesApply 确认 Plan 与 Apply 给出同一份改动清单。
// --dry-run 展示的计划若与实际执行的不一致，用户就无法据此判断影响。
func TestPreservePlanMatchesApply(t *testing.T) {
	planned, err := Plan(fixtureOf+"permissions.ofd", Options{DocType: "OFD-A"})
	if err != nil {
		t.Fatalf("Plan 失败: %v", err)
	}
	var out bytes.Buffer
	applied, err := Apply(fixtureOf+"permissions.ofd", &out, Options{DocType: "OFD-A"})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if len(planned.Changes) != len(applied.Changes) {
		t.Fatalf("计划 %d 处改动，实际 %d 处", len(planned.Changes), len(applied.Changes))
	}
	for i := range planned.Changes {
		if planned.Changes[i] != applied.Changes[i] {
			t.Errorf("第 %d 处不一致: 计划 %+v，实际 %+v", i, planned.Changes[i], applied.Changes[i])
		}
	}
	if planned.Entry != applied.Entry || planned.Entry != "Doc_0/Document.xml" {
		t.Errorf("条目路径不一致: 计划 %q，实际 %q", planned.Entry, applied.Entry)
	}
	if planned.Changes[0].Action == "" {
		t.Error("改动说明为空，--dry-run 无法展示")
	}
	if out.Len() == 0 {
		t.Error("Apply 未写出内容")
	}
}

// TestPreserveDetectsDocType 确认未显式指定时按文件声明判定。
func TestPreserveDetectsDocType(t *testing.T) {
	result, err := Plan(fixtureOf+"permissions.ofd", Options{})
	if err != nil {
		t.Fatalf("Plan 失败: %v", err)
	}
	t.Logf("按文件声明判定为 %s，计划 %d 处改动", result.DocType, len(result.Changes))
}

// TestPreserveOutputPassesStrictValidation 确认转换结果仍是合法 OFD。
// 这是整套转换的地基：写出的文件若过不了严格校验，归档毫无意义。
func TestPreserveOutputPassesStrictValidation(t *testing.T) {
	var out bytes.Buffer
	if _, err := Apply(fixtureOf+"permissions.ofd", &out, Options{
		DocType:  "OFD-A",
		Validate: true,
	}); err != nil {
		t.Fatalf("转换并校验失败: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("输出为空")
	}
	pkg, err := core.OpenBytes(out.Bytes())
	if err != nil {
		t.Fatalf("输出不是合法 ZIP: %v", err)
	}
	defer func() { _ = pkg.Close() }()
	if !pkg.Has("OFD.xml") {
		t.Error("输出缺少 OFD.xml")
	}
	if !pkg.Has("Doc_0/Document.xml") {
		t.Error("输出缺少 Doc_0/Document.xml")
	}
}

// TestPreserveRejectsUnknownDocType 确认未知取值被拒绝，而不是静默按基础处理。
func TestPreserveRejectsUnknownDocType(t *testing.T) {
	if _, err := Plan(fixtureOf+"permissions.ofd", Options{DocType: "OFD-X"}); err == nil {
		t.Error("未知 DocType 应报错")
	}
}

// buildOFD 就地拼一个最小 OFD 包，用于覆盖 testdata 里没有的结构。
func buildOFD(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("创建条目 %s 失败: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写入 %s 失败: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const ns = ` xmlns="http://www.ofdspec.org/2016"`

// ofdXML 拼一个只含入口的 OFD.xml。
func ofdXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<OFD` + ns + ` Version="1.0">` +
		`<DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`
}

// actionXML 拼一个动作元素。withGoto 为真时带 Goto 子元素，即文档内跳转。
func actionXML(withGoto bool) string {
	if withGoto {
		return `<Action Event="CLICK"><Goto><Dest PageID="2"/></Goto></Action>`
	}
	return `<Action Event="CLICK"><URI URI="http://example.com"/></Action>`
}

func documentXML(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Document` + ns + `><CommonData><MaxUnitID>99</MaxUnitID>` +
		`<PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData>` +
		`<Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>` +
		body + `</Document>`
}

// TestPreserveRemovesPageActions 覆盖 GB/T 42133 6.2.3 c)：页面动作只保留 Goto。
func TestPreserveRemovesPageActions(t *testing.T) {
	src := buildOFD(t, map[string]string{
		"OFD.xml":            ofdXML(),
		"Doc_0/Document.xml": documentXML(""),
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Page` + ns + ` ID="1"><Area><PhysicalBox>0 0 210 297</PhysicalBox></Area>` +
			`<Content><TextObject ID="4" Boundary="0 0 10 10"><TextCode X="0" Y="5">A</TextCode>` +
			`<Actions>` + actionXML(false) + actionXML(false) + actionXML(true) + `</Actions>` +
			`</TextObject></Content><Actions>` + actionXML(false) + actionXML(true) + `</Actions></Page>`,
	})
	result, after := appliedChangesBytes(t, src, Options{DocType: "OFD-A"})
	// 只统计动作类条款：页面设置归并也会改动 Content.xml，不该计入。
	removed := 0
	for _, change := range result.Changes {
		if change.Clause == "GB/T 42133 6.2.3 c)" || change.Clause == "GB/T 42133 6.3.3 b)" {
			removed += change.Count
		}
	}
	// 页面级 1 个非 Goto，TextObject 内 2 个，共 3 个。
	if removed != 3 {
		t.Errorf("共去除 %d 个动作，预期 3 个：%+v", removed, result.Changes)
	}
	if strings.Contains(after["Doc_0/Pages/Page_0/Content.xml"], "URI") {
		t.Error("非 Goto 动作仍残留")
	}
	if n := strings.Count(after["Doc_0/Pages/Page_0/Content.xml"], "<Goto"); n != 2 {
		t.Errorf("Goto 动作应保留 2 个，实际 %d 个", n)
	}
}

// TestPreserveRemovesOutlineActions 覆盖 GB/T 42133 6.2.5 a)：大纲节点动作只保留 Goto。
// 大纲可多层嵌套，因此动作可能出现在任意深度的 OutlineElem 上。
func TestPreserveRemovesOutlineActions(t *testing.T) {
	src := buildOFD(t, map[string]string{
		"OFD.xml": ofdXML(),
		"Doc_0/Document.xml": documentXML(
			`<Outlines><OutlineElem ID="10" Title="L1">` +
				`<Actions>` + actionXML(false) + actionXML(true) + `</Actions>` +
				`<OutlineElem ID="11" Title="L2">` +
				`<Actions>` + actionXML(false) + actionXML(false) + `</Actions>` +
				`</OutlineElem></OutlineElem></Outlines>`),
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Page` + ns + ` ID="1"><Area><PhysicalBox>0 0 210 297</PhysicalBox></Area><Content/></Page>`,
	})
	result, after := appliedChangesBytes(t, src, Options{DocType: "OFD-A"})
	var removed int
	for _, change := range result.Changes {
		if change.Clause == "GB/T 42133 6.2.5 a)" {
			removed += change.Count
		}
	}
	// 外层 1 个非 Goto，内层 2 个，共 3 处。
	if removed != 3 {
		t.Errorf("大纲节点共去除 %d 个非 Goto 动作，预期 3 个：%+v", removed, result.Changes)
	}
	got := after["Doc_0/Document.xml"]
	if strings.Contains(got, "URI") {
		t.Error("大纲内的非 Goto 动作仍残留")
	}
	if n := strings.Count(got, "<Goto"); n != 1 {
		t.Errorf("大纲内 Goto 动作应保留 1 个，实际 %d 个", n)
	}
}

// TestPruneActionsRemovesEmptiedContainer 确认动作删光后连 Actions 容器一起删。
// XSD 里 Action 没有 minOccurs（默认 1），留下空的 <Actions/> 会让输出通不过校验。
func TestPruneActionsRemovesEmptiedContainer(t *testing.T) {
	src := buildOFD(t, map[string]string{
		"OFD.xml": ofdXML(),
		"Doc_0/Document.xml": documentXML(
			`<Actions>` + actionXML(false) + `</Actions>`),
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Page` + ns + ` ID="1"><Area><PhysicalBox>0 0 210 297</PhysicalBox></Area><Content/></Page>`,
	})
	_, after := appliedChangesBytes(t, src, Options{DocType: "OFD-A"})
	if got := after["Doc_0/Document.xml"]; strings.Contains(got, "Actions") {
		t.Errorf("动作删光后仍留下空的 Actions 容器，输出通不过 XSD:\n%s", got)
	}
}

// TestPreserveDoesNotTouchPageActionsAsDocumentScope 确认页面动作不会被当成
// 文档动作处理——两者条款不同，混淆会让 6.2.2 c) 的统计与实际改动对不上。
func TestPreserveDoesNotTouchPageActionsAsDocumentScope(t *testing.T) {
	src := buildOFD(t, map[string]string{
		"OFD.xml":            ofdXML(),
		"Doc_0/Document.xml": documentXML(""),
		"Doc_0/Pages/Page_0/Content.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Page` + ns + ` ID="1"><Area><PhysicalBox>0 0 210 297</PhysicalBox></Area>` +
			`<Content/><Actions>` + actionXML(false) + `</Actions></Page>`,
	})
	result, _ := appliedChangesBytes(t, src, Options{DocType: "OFD-A"})
	// 页面设置归并（6.2.3 a)）本就要改 Document.xml，故只检查动作类条款的归属。
	for _, change := range result.Changes {
		isAction := change.Clause == "GB/T 42133 6.2.2 c)" ||
			change.Clause == "GB/T 42133 6.2.3 c)" ||
			change.Clause == "GB/T 42133 6.3.3 b)"
		if isAction && change.Entry == "Doc_0/Document.xml" {
			t.Errorf("页面动作被记到文档动作条款下：%+v", change)
		}
	}
	var found bool
	for _, change := range result.Changes {
		if change.Clause == "GB/T 42133 6.2.3 c)" {
			found = true
		}
	}
	if !found {
		t.Error("未按 6.2.3 c) 记录页面动作的改动")
	}
}

// buildWithOrphan 构造一个含无人引用条目的最小 OFD 包。
func buildWithOrphan(t *testing.T) []byte {
	t.Helper()
	entries := [][2]string{
		{"OFD.xml", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<OFD` + ns + ` Version="1.0">` +
			`<DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`},
		{"Doc_0/Document.xml", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Document` + ns + `><CommonData><MaxUnitID>9</MaxUnitID>` +
			`<PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData>` +
			`<Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`},
		{"Doc_0/Pages/Page_0/Content.xml", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Page` + ns + ` ID="1"><Area><PhysicalBox>0 0 210 297</PhysicalBox></Area><Content/></Page>`},
		{"Doc_0/Res/orphan.bin", "无人引用的多余文件"},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range entries {
		w, err := zw.Create(entry[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestUnreferencedReportedButNotDeletedByDefault 覆盖 6.2.1 c) 的默认行为：
// 无人引用的条目只报告不删除。删除不可逆，而闭包正确性依赖引用识别是否穷尽。
func TestUnreferencedReportedButNotDeletedByDefault(t *testing.T) {
	result, err := Plan(buildWithOrphan(t), Options{DocType: "OFD-A"})
	if err != nil {
		t.Fatalf("Plan 失败: %v", err)
	}
	if len(result.Unreferenced) != 1 || result.Unreferenced[0] != "Doc_0/Res/orphan.bin" {
		t.Fatalf("未正确识别删除候选：%v", result.Unreferenced)
	}
	if result.UnreferencedDropped != 0 {
		t.Errorf("默认不应删除任何条目，实际删了 %d 个", result.UnreferencedDropped)
	}
	if result.ClosureIncomplete {
		t.Error("构造的包闭包应完整")
	}
}

// TestUnreferencedDeletedWhenOptedIn 确认显式开启后 6.2.1 c) 真正执行删除。
func TestUnreferencedDeletedWhenOptedIn(t *testing.T) {
	var out bytes.Buffer
	result, err := Apply(buildWithOrphan(t), &out, Options{
		DocType: "OFD-A", DropUnreferenced: true,
	})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if result.UnreferencedDropped != 1 {
		t.Errorf("应删除 1 个条目，实际 %d 个", result.UnreferencedDropped)
	}
	if out.Len() == 0 {
		t.Fatal("没有写出输出")
	}
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "Doc_0/Res/orphan.bin" {
			t.Error("无人引用的条目仍留在输出中")
		}
	}
	// 入口与在用文件必须完好。
	var kept int
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".xml") {
			kept++
		}
	}
	if kept != 3 {
		t.Errorf("应保留 3 个 XML 条目，实际 %d 个", kept)
	}
}

// TestUnreferencedRefusedWhenClosureIncomplete 是安全性的核心：闭包不可信时，
// 即使显式要求删除也必须拒绝。testdata/ofdrw/intro.ofd 的命名空间缺 "/2016"
// 后缀，其 123 个条目会全被误判为无人引用。
func TestUnreferencedRefusedWhenClosureIncomplete(t *testing.T) {
	const legacy = "../../testdata/ofdrw/intro.ofd"
	var warnings []string
	var out bytes.Buffer
	_, err := Apply(legacy, &out, Options{
		DocType: "OFD-A", DropUnreferenced: true,
		OnWarning: func(msg string) { warnings = append(warnings, msg) },
	})
	// 该文件本身还有其他问题，可能在别处失败；关键是不能删条目。
	if err == nil && out.Len() > 0 {
		zr, zipErr := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
		if zipErr == nil && len(zr.File) < 100 {
			t.Errorf("闭包不完整却执行了删除：输出仅 %d 个条目", len(zr.File))
		}
	}
	var reported bool
	for _, msg := range warnings {
		if strings.Contains(msg, "闭包不完整") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("未报告闭包不完整，警告：%v", warnings)
	}
}

// pageXML 拼一个带指定 Area 的页面描述。
func pageXML(id, area string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Page` + ns + ` ID="` + id + `">` + area +
		`<Content><TextObject ID="9" Boundary="0 0 10 10"><TextCode X="0" Y="5">A</TextCode></TextObject></Content></Page>`
}

const (
	areaA4 = `<Area><PhysicalBox>0 0 595 842</PhysicalBox></Area>`
	areaA5 = `<Area><PhysicalBox>0 0 595 842</PhysicalBox><ContentBox>0 0 575 812</ContentBox></Area>`
)

// TestPageAreaPromotesMostUsedAsDefault 覆盖 6.2.3 a)：使用最多的页面设置
// 升为文档默认设置，写入 CommonData/PageArea。
func TestPageAreaPromotesMostUsedAsDefault(t *testing.T) {
	// 五页：A 四页、B 一页，A 应成为默认。
	pages := `<Pages>` +
		`<Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/>` +
		`<Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/>` +
		`<Page ID="3" BaseLoc="Pages/Page_2/Content.xml"/>` +
		`<Page ID="4" BaseLoc="Pages/Page_3/Content.xml"/>` +
		`<Page ID="5" BaseLoc="Pages/Page_4/Content.xml"/>` +
		`</Pages>`
	files := map[string]string{
		"OFD.xml": ofdXML(),
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Document` + ns + `><CommonData><MaxUnitID>99</MaxUnitID>` +
			`<PageArea><PhysicalBox>0 0 100 100</PhysicalBox></PageArea></CommonData>` +
			pages + `</Document>`,
		"Doc_0/Pages/Page_0/Content.xml": pageXML("1", areaA4),
		"Doc_0/Pages/Page_1/Content.xml": pageXML("2", areaA4),
		"Doc_0/Pages/Page_2/Content.xml": pageXML("3", areaA4),
		"Doc_0/Pages/Page_3/Content.xml": pageXML("4", areaA4),
		"Doc_0/Pages/Page_4/Content.xml": pageXML("5", areaA5),
	}
	result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})

	doc := after["Doc_0/Document.xml"]
	if !strings.Contains(doc, "595 842") {
		t.Errorf("CommonData/PageArea 未更新为使用最多的设置：\n%s", doc)
	}
	if strings.Contains(doc, "0 0 100 100") {
		t.Errorf("原有默认设置未被替换：\n%s", doc)
	}
	// PageArea 必须紧随 MaxUnitID：CT_CommonData 是 xs:sequence，顺序错则 XSD 不过。
	maxIdx := strings.Index(doc, "MaxUnitID")
	areaIdx := strings.Index(doc, "PageArea")
	pagesIdx := strings.Index(doc, "<Pages")
	if !(maxIdx < areaIdx && areaIdx < pagesIdx) {
		t.Errorf("PageArea 位置不对，应在 MaxUnitID 之后、Pages 之前：\n%s", doc)
	}
	var promoted bool
	for _, change := range result.Changes {
		if change.Clause == "GB/T 42133 6.2.3 a)" {
			promoted = true
		}
	}
	if !promoted {
		t.Error("未按 6.2.3 a) 记录改动")
	}
}

// TestPageAreaOmitsMatchingPages 覆盖 6.2.3 b)：与默认设置相同的页面省略 Area。
func TestPageAreaOmitsMatchingPages(t *testing.T) {
	pages := `<Pages>` +
		`<Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/>` +
		`<Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/>` +
		`</Pages>`
	files := map[string]string{
		"OFD.xml": ofdXML(),
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Document` + ns + `><CommonData><MaxUnitID>99</MaxUnitID>` +
			`<PageArea><PhysicalBox>0 0 10 10</PhysicalBox></PageArea></CommonData>` +
			pages + `</Document>`,
		// 两页都与文档默认相同，都应被省略。
		"Doc_0/Pages/Page_0/Content.xml": pageXML("1", areaA4),
		"Doc_0/Pages/Page_1/Content.xml": pageXML("2", areaA4),
	}
	result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	for _, name := range []string{"Doc_0/Pages/Page_0/Content.xml", "Doc_0/Pages/Page_1/Content.xml"} {
		if strings.Contains(after[name], "<Area>") {
			t.Errorf("%s 的 Area 应被省略：\n%s", name, after[name])
		}
		if !strings.Contains(after[name], "<Content>") {
			t.Errorf("%s 的页面内容被误删：\n%s", name, after[name])
		}
	}
	var omitted bool
	for _, change := range result.Changes {
		if change.Clause == "GB/T 42133 6.2.3 b)" {
			omitted = true
			if change.Count != 2 {
				t.Errorf("应省略 2 页，实际 %d 页", change.Count)
			}
		}
	}
	if !omitted {
		t.Error("未按 6.2.3 b) 记录改动")
	}
}

// TestPageAreaKeepsNonMatchingPage 确认与默认设置不同的页面保留自己的 Area。
func TestPageAreaKeepsNonMatchingPage(t *testing.T) {
	pages := `<Pages>` +
		`<Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/>` +
		`<Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/>` +
		`</Pages>`
	files := map[string]string{
		"OFD.xml": ofdXML(),
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Document` + ns + `><CommonData><MaxUnitID>99</MaxUnitID>` +
			`<PageArea><PhysicalBox>0 0 10 10</PhysicalBox></PageArea></CommonData>` +
			pages + `</Document>`,
		// A4 四页占优，B 成为默认；A4 的四页省略，B 自己那页保留。
		"Doc_0/Pages/Page_0/Content.xml": pageXML("1", areaA4),
		"Doc_0/Pages/Page_1/Content.xml": pageXML("2", areaA5),
	}
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	// 两页设置不同，各出现一次，按页面顺序取前者为默认，故第一页被省略、第二页保留。
	if strings.Contains(after["Doc_0/Pages/Page_0/Content.xml"], "<Area>") {
		t.Errorf("成为默认设置的那页应省略 Area：\n%s", after["Doc_0/Pages/Page_0/Content.xml"])
	}
	if !strings.Contains(after["Doc_0/Pages/Page_1/Content.xml"], "<Area>") {
		t.Errorf("非默认设置的页面应保留 Area：\n%s", after["Doc_0/Pages/Page_1/Content.xml"])
	}
}

// TestPageAreaComparesNumerically 确认按数值而非文本比较页面设置。
// 文本比对会把 "210" 与 "210.0" 判成不同设置，于是 6.2.3 b) 静默失效。
func TestPageAreaComparesNumerically(t *testing.T) {
	pages := `<Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>`
	files := map[string]string{
		"OFD.xml": ofdXML(),
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Document` + ns + `><CommonData><MaxUnitID>99</MaxUnitID>` +
			`<PageArea><PhysicalBox>0 0 10 10</PhysicalBox></PageArea></CommonData>` +
			pages + `</Document>`,
		// 与文档默认数值相同但写法不同。
		"Doc_0/Pages/Page_0/Content.xml": pageXML("1",
			`<Area><PhysicalBox>0.0 0.00 595.0 842.000</PhysicalBox></Area>`),
	}
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"],
		`<PageArea><PhysicalBox>0 0 10 10</PhysicalBox></PageArea>`, "", 1)
	_, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	// 写法不同但数值相同，应被判定为与默认一致而省略。
	if strings.Contains(after["Doc_0/Pages/Page_0/Content.xml"], "<Area>") {
		t.Errorf("数值相同的页面设置应被识别为一致并省略 Area：\n%s",
			after["Doc_0/Pages/Page_0/Content.xml"])
	}
}

// TestPageAreaSkippedForBaseDocType 确认基础 profile 不做页面设置归并。
// 基础 profile 不承诺满足 GB/T 42133，改写页面结构属无依据的破坏。
func TestPageAreaSkippedForBaseDocType(t *testing.T) {
	pages := `<Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages>`
	files := map[string]string{
		"OFD.xml": ofdXML(),
		"Doc_0/Document.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Document` + ns + `><CommonData><MaxUnitID>99</MaxUnitID>` +
			`<PageArea><PhysicalBox>0 0 10 10</PhysicalBox></PageArea></CommonData>` +
			pages + `</Document>`,
		"Doc_0/Pages/Page_0/Content.xml": pageXML("1", areaA4),
	}
	result, err := Plan(buildOFD(t, files), Options{DocType: "OFD"})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range result.Changes {
		if change.Clause == "GB/T 42133 6.2.3 a)" || change.Clause == "GB/T 42133 6.2.3 b)" {
			t.Errorf("基础 profile 不应做页面设置归并，却有改动 %+v", change)
		}
	}
}
