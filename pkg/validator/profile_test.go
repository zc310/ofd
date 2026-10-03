package validator

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/pkg/creator"
)

// archiveProfileEntry 是构造 profile 测试包时使用的条目。
type archiveProfileEntry struct {
	name    string
	content string
}

// profileDocTypePackage 在合法包的基础上改写 OFD.xml 的 DocType，用于触发
// profile 规则的自动判定。
func profileDocTypePackage(t *testing.T, docType string) []byte {
	t.Helper()
	data, err := creator.MarshalWithOptions(creator.Document{
		ID:       "profile-check",
		Title:    "profile 校验",
		PageSize: creator.A4,
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{X: 20, Y: 30, Width: 100, Height: 10, Value: "内容", Font: "SimSun"},
		}}},
	}, creator.CreateOptions{Compression: creator.CompressionAuto, DocType: docType})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// replaceArchiveEntry 替换或新增包内条目，其余条目原样保留。
func replaceArchiveEntry(t *testing.T, data []byte, entries ...archiveProfileEntry) []byte {
	t.Helper()
	source, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	replacements := make(map[string]string, len(entries))
	for _, entry := range entries {
		replacements[entry.name] = entry.content
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	seen := make(map[string]bool, len(entries))
	for _, entry := range source.File {
		target, err := writer.CreateHeader(&zip.FileHeader{Name: entry.Name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if replacement, ok := replacements[entry.Name]; ok {
			if _, err := target.Write([]byte(replacement)); err != nil {
				t.Fatal(err)
			}
			seen[entry.Name] = true
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := target.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range entries {
		if seen[entry.name] {
			continue
		}
		if _, err := writer.Create(entry.name); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// profileIssues 执行校验并返回 profile 阶段的问题码。
func profileIssues(t *testing.T, data []byte, options ...Option) []string {
	t.Helper()
	instance, err := New(options...)
	if err != nil {
		t.Fatal(err)
	}
	report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "profile.ofd")
	var codes []string
	for _, issue := range report.Issues {
		if issue.Stage == StageProfile {
			codes = append(codes, issue.Code)
		}
	}
	return codes
}

// TestProfileRulesSkippedForBaseDocType 保护基础 profile 不做 profile 校验：
// 42133 与电子病历要求只约束自称档案或病历的文件，普通 OFD 不该被这些规则
// 拦下，否则会给存量文件凭空增加错误。
func TestProfileRulesSkippedForBaseDocType(t *testing.T) {
	data := profileDocTypePackage(t, spec.DocTypeOFD)
	if codes := profileIssues(t, data); len(codes) != 0 {
		t.Fatalf("基础 profile 出现 profile 问题: %v", codes)
	}
	instance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "base.ofd")
	if report.Profile != "" {
		t.Errorf("基础 profile 的报告不应记录 profile，got %q", report.Profile)
	}
}

// TestProfileDetectedFromDocType 保护按 DocType 自动识别 profile，并把实际
// 应用的 profile 记进报告。
func TestProfileDetectedFromDocType(t *testing.T) {
	for _, docType := range []string{spec.DocTypeOFDA, spec.DocTypeOFDH} {
		data := profileDocTypePackage(t, docType)
		instance, err := New()
		if err != nil {
			t.Fatal(err)
		}
		report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "p.ofd")
		if report.Profile != docType {
			t.Errorf("DocType=%q 时报告记录的 profile = %q", docType, report.Profile)
		}
		if got := checkStatus(report, "profile"); got != "passed" {
			t.Errorf("DocType=%q 时 profile 检查项状态 = %q, want passed", docType, got)
		}
	}
}

// TestProfileOptionOverridesDocType 保护显式 --doctype 可在不改写 DocType 的
// 前提下预检。
func TestProfileOptionOverridesDocType(t *testing.T) {
	data := profileDocTypePackage(t, spec.DocTypeOFD)
	instance, err := New(WithDocType(spec.DocTypeOFDA))
	if err != nil {
		t.Fatal(err)
	}
	report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "p.ofd")
	if report.Profile != spec.DocTypeOFDA {
		t.Errorf("报告记录的 profile = %q, want %q", report.Profile, spec.DocTypeOFDA)
	}
	// 合规的基础文件不应被 OFD-A 规则判为违规，但检查项必须已执行。
	if got := checkStatus(report, "profile"); got != "passed" {
		t.Errorf("profile 检查项状态 = %q, want passed", got)
	}
}

// TestProfileRejectsUnknownProfile 保护未知 DocType 在构造校验器时就报错。
// 取值区分大小写，因此 ofd-a 这类拼写错误不会被静默接受。
func TestProfileRejectsUnknownProfile(t *testing.T) {
	if _, err := New(WithDocType("ofd-a")); err == nil {
		t.Fatal("未知 profile 未被拒绝")
	}
}

// TestProfileRuleSingleDocument 对应 GB/T 42133 6.2.1 c)。
func TestProfileRuleSingleDocument(t *testing.T) {
	data := profileDocTypePackage(t, spec.DocTypeOFDA)
	data = replaceArchiveEntry(t, data, archiveProfileEntry{name: "OFD.xml", content: `<?xml version="1.0" encoding="UTF-8"?>
<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD-A"><DocBody><DocInfo><DocID>a</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody><DocBody><DocInfo><DocID>b</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`})
	assertProfileCode(t, data, "profile.ofd_a.single_document")
}

// TestProfileRuleForbiddenDocumentNodes 对应 GB/T 42133 6.2.2 a) b) e)。
func TestProfileRuleForbiddenDocumentNodes(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		element  string
		wantCode string
	}{
		{"权限声明", "Permissions", "profile.ofd_a.permissions_present"},
		{"视图首选项", "VPreferences", "profile.ofd_a.vpreferences_present"},
		{"扩展信息", "Extensions", "profile.ofd_a.extensions_present"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			data := profileDocTypePackage(t, spec.DocTypeOFDA)
			data = injectDocumentChild(t, data, "<"+testCase.element+"><MaxUnitID>0</MaxUnitID></"+testCase.element+">")
			assertProfileCode(t, data, testCase.wantCode)
		})
	}
}

// TestProfileRuleEncryption 对应 GB/T 42133 6.16。
func TestProfileRuleEncryption(t *testing.T) {
	data := profileDocTypePackage(t, spec.DocTypeOFDA)
	data = replaceArchiveEntry(t, data, archiveProfileEntry{name: "Encryptions.xml", content: `<?xml version="1.0" encoding="UTF-8"?>
<Encryptions xmlns:ofd="http://www.ofdspec.org/2016"/>`})
	assertProfileCode(t, data, "profile.ofd_a.encrypted")
}

// TestProfileRulePageBlockDepth 对应 GB/T 42133 6.2.3 e)。
func TestProfileRulePageBlockDepth(t *testing.T) {
	data := profileDocTypePackage(t, spec.DocTypeOFDA)
	// 四层嵌套超过 3 层上限。
	const (
		open  = "<PageBlock ID=\"b1\"><PageBlock ID=\"b2\"><PageBlock ID=\"b3\"><PageBlock ID=\"b4\">"
		close = "</PageBlock></PageBlock></PageBlock></PageBlock>"
	)
	data = injectPageContent(t, data, open+close)
	assertProfileCode(t, data, "profile.ofd_a.pageblock_depth")

	// 三层嵌套是允许的上限，不应报错。
	const (
		okOpen  = "<PageBlock ID=\"c1\"><PageBlock ID=\"c2\"><PageBlock ID=\"c3\">"
		okClose = "</PageBlock></PageBlock></PageBlock>"
	)
	okData := injectPageContent(t, profileDocTypePackage(t, spec.DocTypeOFDA), okOpen+okClose)
	for _, code := range profileIssues(t, okData) {
		if code == "profile.ofd_a.pageblock_depth" {
			t.Fatal("3 层嵌套被误报为超限")
		}
	}
}

// assertProfileCode 断言指定问题码出现。
func assertProfileCode(t *testing.T, data []byte, want string) {
	t.Helper()
	codes := profileIssues(t, data)
	for _, code := range codes {
		if code == want {
			return
		}
	}
	t.Errorf("未出现问题码 %q，实际: %v", want, codes)
}

// injectDocumentChild 在 Doc_0/Document.xml 的根节点末尾插入一个元素。
func injectDocumentChild(t *testing.T, data []byte, snippet string) []byte {
	t.Helper()
	const name = "Doc_0/Document.xml"
	original := readArchiveEntry(t, data, name)
	if !strings.HasSuffix(strings.TrimSpace(original), "</Document>") {
		t.Fatalf("%s 结构异常，无法注入", name)
	}
	updated := strings.Replace(original, "</Document>", snippet+"</Document>", 1)
	return replaceArchiveEntry(t, data, archiveProfileEntry{name: name, content: updated})
}

// injectPageRootChild 在首个页面 Content.xml 的根节点末尾插入片段。Page 的
// 元素顺序是 Template?、PageRes*、Area?、Content?、Actions?，因此页面级动作
// 必须挂在 Content 之后，直接插到根节点末尾即可满足顺序约束。
func injectPageRootChild(t *testing.T, data []byte, snippet string) []byte {
	t.Helper()
	name := findArchiveEntry(t, data, "Content.xml")
	if name == "" {
		t.Fatal("包内没有 Content.xml")
	}
	original := readArchiveEntry(t, data, name)
	updated := strings.Replace(original, "</Page>", snippet+"</Page>", 1)
	if updated == original {
		t.Fatalf("%s 结构异常，无法注入", name)
	}
	return replaceArchiveEntry(t, data, archiveProfileEntry{name: name, content: updated})
}

// injectPageContent 在首个页面 Content.xml 的 Layer 内插入片段。
func injectPageContent(t *testing.T, data []byte, snippet string) []byte {
	t.Helper()
	name := findArchiveEntry(t, data, "Content.xml")
	if name == "" {
		t.Fatal("包内没有 Content.xml")
	}
	original := readArchiveEntry(t, data, name)
	updated := strings.Replace(original, "</Layer>", snippet+"</Layer>", 1)
	if updated == original {
		t.Fatalf("%s 结构异常，无法注入", name)
	}
	return replaceArchiveEntry(t, data, archiveProfileEntry{name: name, content: updated})
}

func readArchiveEntry(t *testing.T, data []byte, name string) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if entry.Name != name {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}
	t.Fatalf("包内缺少 %s", name)
	return ""
}

// findArchiveEntry 返回包内第一个以 suffix 结尾的条目名。
func findArchiveEntry(t *testing.T, data []byte, suffix string) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(archive.File))
	for _, entry := range archive.File {
		if strings.HasSuffix(entry.Name, suffix) {
			names = append(names, entry.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[0]
}

// checkStatus 返回指定校验项的状态。
func checkStatus(report Report, name string) string {
	for _, check := range report.Checks {
		if check.Name == name {
			return check.Status
		}
	}
	return ""
}

// TestProfileRuleColorSpaceType 对应 GB/T 42133 6.3.1 b)。creator 不生成颜色
// 空间，这里直接改写 PublicRes.xml 注入。
func TestProfileRuleColorSpaceType(t *testing.T) {
	const name = "Doc_0/DocumentRes.xml"
	t.Run("允许 RGB", func(t *testing.T) {
		data := replaceArchiveEntry(t, profileDocTypePackage(t, spec.DocTypeOFDA),
			archiveProfileEntry{name: name, content: colorSpaceXML("RGB")})
		for _, code := range profileIssues(t, data) {
			if code == "profile.ofd_a.colorspace_type" {
				t.Fatal("RGB 被误报为非法颜色空间")
			}
		}
	})
	t.Run("拒绝 Lab", func(t *testing.T) {
		data := replaceArchiveEntry(t, profileDocTypePackage(t, spec.DocTypeOFDA),
			archiveProfileEntry{name: name, content: colorSpaceXML("Lab")})
		assertProfileCode(t, data, "profile.ofd_a.colorspace_type")
	})
}

func colorSpaceXML(colorType string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<Res xmlns="http://www.ofdspec.org/2016"><ColorSpaces><ColorSpace ID="1" Type="` + colorType + `"/></ColorSpaces></Res>`
}

// TestProfileRuleImageFormat 对应 GB/T 42133 6.2.6 e)。
func TestProfileRuleImageFormat(t *testing.T) {
	for _, testCase := range []struct {
		extension string
		rejected  bool
	}{
		{"png", false},
		{"jpg", false},
		{"tiff", false},
		{"jbig2", false},
		{"gif", true},
		{"webp", true},
		{"svg", true},
	} {
		t.Run(testCase.extension, func(t *testing.T) {
			data := injectImageResource(t, testCase.extension)
			codes := profileIssues(t, data)
			got := containsCode(codes, "profile.ofd_a.image_format")
			if got != testCase.rejected {
				t.Errorf("格式 .%s 报出问题=%v，期望 rejected=%v（实际码 %v）",
					testCase.extension, got, testCase.rejected, codes)
			}
		})
	}
}

// injectImageResource 注入一张被页面引用的栅格图像，extension 只给扩展名。
func injectImageResource(t *testing.T, extension string) []byte {
	t.Helper()
	data := profileDocTypePackage(t, spec.DocTypeOFDA)
	data = replaceArchiveEntry(t, data,
		archiveProfileEntry{name: "Doc_0/DocumentRes.xml", content: `<?xml version="1.0" encoding="UTF-8"?>
<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Res"><MultiMedias><MultiMedia ID="9" Type="Image"><MediaFile>Image_0.` + extension + `</MediaFile></MultiMedia></MultiMedias></Res>`},
		archiveProfileEntry{name: "Doc_0/Res/Image_0." + extension, content: "fake-image-bytes"},
	)
	return injectPageContent(t, data,
		`<ImageObject ID="7" ResourceID="9" Boundary="0 0 10 10"/>`)
}

// TestProfileRuleActionOnlyGoto 对应 GB/T 42133 6.2.2 c) 与 6.2.3 c)。
func TestProfileRuleActionOnlyGoto(t *testing.T) {
	t.Run("Goto 动作被放行", func(t *testing.T) {
		data := injectPageRootChild(t, profileDocTypePackage(t, spec.DocTypeOFDA),
			`<Actions><Action Event="DO"><Goto><Dest Page="0"/></Goto></Action></Actions>`)
		for _, code := range profileIssues(t, data) {
			if code == "profile.ofd_a.page_action_not_goto" {
				t.Fatal("Goto 动作被误报为非文档内跳转")
			}
		}
	})
	t.Run("URI 动作被拒", func(t *testing.T) {
		data := injectPageRootChild(t, profileDocTypePackage(t, spec.DocTypeOFDA),
			`<Actions><Action Event="DO"><URI URI="https://example.invalid"/></Action></Actions>`)
		assertProfileCode(t, data, "profile.ofd_a.page_action_not_goto")
	})
	t.Run("Sound 动作被拒", func(t *testing.T) {
		data := injectPageRootChild(t, profileDocTypePackage(t, spec.DocTypeOFDA),
			`<Actions><Action Event="DO"><Sound Value="Doc_0/Res/a.wav"/></Action></Actions>`)
		assertProfileCode(t, data, "profile.ofd_a.page_action_not_goto")
	})
}

// TestProfileRuleDocumentActionOnlyGoto 对应 GB/T 42133 6.2.2 c)：文档级动作。
func TestProfileRuleDocumentActionOnlyGoto(t *testing.T) {
	data := injectDocumentChild(t, profileDocTypePackage(t, spec.DocTypeOFDA),
		`<Actions><Action Event="DO"><URI URI="https://example.invalid"/></Action></Actions>`)
	assertProfileCode(t, data, "profile.ofd_a.document_action_not_goto")
}

// TestProfileRuleOutlineActionOnlyGoto 对应 GB/T 42133 6.2.5 a)：大纲节点动作。
func TestProfileRuleOutlineActionOnlyGoto(t *testing.T) {
	data := injectDocumentChild(t, profileDocTypePackage(t, spec.DocTypeOFDA),
		`<Outlines><OutlineElem Title="外部链接"><Actions><Action Event="DO"><URI URI="https://example.invalid"/></Action></Actions></OutlineElem></Outlines>`)
	assertProfileCode(t, data, "profile.ofd_a.outline_action_not_goto")
}

// TestProfileMedicalInheritsArchiveRules 保护 OFD-H 继承 OFD-A 的全部规则：
// 电子病历标准声明数据内容与组织应符合 GB/T 42133。
func TestProfileMedicalInheritsArchiveRules(t *testing.T) {
	archive := profileFor(spec.DocTypeOFDA).resolvedRules()
	medical := profileFor(spec.DocTypeOFDH).resolvedRules()
	if len(medical) < len(archive) {
		t.Fatalf("OFD-H 规则数 %d 少于 OFD-A 的 %d", len(medical), len(archive))
	}
	archiveCodes := make(map[string]bool, len(archive))
	for _, rule := range archive {
		archiveCodes[rule.Code] = true
	}
	for _, rule := range medical {
		if !archiveCodes[rule.Code] {
			t.Errorf("OFD-H 规则 %q 不在 OFD-A 规则集中", rule.Code)
		}
	}
	// 同一条违规在两个 profile 下都应被报出，只是问题码前缀不同。
	data := replaceArchiveEntry(t, profileDocTypePackage(t, spec.DocTypeOFDH),
		archiveProfileEntry{name: "Encryptions.xml", content: `<?xml version="1.0" encoding="UTF-8"?>
<Encryptions xmlns="http://www.ofdspec.org/2016"/>`})
	assertProfileCode(t, data, "profile.ofd_h.encrypted")
}

func containsCode(codes []string, want string) bool {
	return slices.Contains(codes, want)
}
