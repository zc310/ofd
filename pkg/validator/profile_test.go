package validator

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
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
	return injectImageResourceFor(t, spec.DocTypeOFDA, extension)
}

// injectImageResourceFor 在指定 DocType 的包里注入一张被页面引用的栅格图像。
func injectImageResourceFor(t *testing.T, docType, extension string) []byte {
	t.Helper()
	data := profileDocTypePackage(t, docType)
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

// TestProfileMedicalInheritsArchiveRules 保护 OFD-H 至少继承 OFD-A 的全部规则。
// OFD-H 可以覆盖父规则（如 image_format 收窄清单）也可以新增自己的规则
// （如 signature_coverage），但不能丢掉任何一条——那会让电子病历文件绕过
// GB/T 42133 的归档约束。
func TestProfileMedicalInheritsArchiveRules(t *testing.T) {
	archive := profileFor(spec.DocTypeOFDA).resolvedRules()
	medical := profileFor(spec.DocTypeOFDH).resolvedRules()
	if len(medical) < len(archive) {
		t.Fatalf("OFD-H 规则数 %d 少于 OFD-A 的 %d", len(medical), len(archive))
	}
	medicalCodes := make(map[string]bool, len(medical))
	for _, rule := range medical {
		medicalCodes[rule.Code] = true
	}
	for _, rule := range archive {
		if !medicalCodes[rule.Code] {
			t.Errorf("OFD-A 规则 %q 未被 OFD-H 继承", rule.Code)
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

// TestProfileImageFormatDiffersByDocType 锁定 GB/T 42133 与 GB/T 48666 的图像
// 格式清单差异：42133 6.2.6 e) 允许六种（含 JBIG2、JPEG2000），48666 7.2 d)
// 只允许四种。OFD-H 若直接继承父 profile 的清单，判定就会比 48666 宽松，
// 只含 JBIG2 的电子病历文件会被误判为合规。
func TestProfileImageFormatDiffersByDocType(t *testing.T) {
	for _, testCase := range []struct {
		extension string
		archiveOK bool
		medicalOK bool
	}{
		{"png", true, true},
		{"jpg", true, true},
		{"tiff", true, true},
		{"bmp", true, true},
		{"jbig2", true, false}, // 42133 允许，48666 不允许
		{"jp2", true, false},   // JPEG2000 同上
		{"gif", false, false},  // 两者都不允许
	} {
		t.Run(testCase.extension, func(t *testing.T) {
			archiveCodes := profileIssues(t, injectImageResourceFor(t, spec.DocTypeOFDA, testCase.extension))
			medicalCodes := profileIssues(t, injectImageResourceFor(t, spec.DocTypeOFDH, testCase.extension))
			if got := containsCode(archiveCodes, "profile.ofd_a.image_format"); got != !testCase.archiveOK {
				t.Errorf("OFD-A 报出问题=%v，期望=%v（码 %v）", got, !testCase.archiveOK, archiveCodes)
			}
			if got := containsCode(medicalCodes, "profile.ofd_h.image_format"); got != !testCase.medicalOK {
				t.Errorf("OFD-H 报出问题=%v，期望=%v（码 %v）", got, !testCase.medicalOK, medicalCodes)
			}
		})
	}
}

// TestProfileRuleOverrideReplacesInherited 保护同名规则由子 profile 覆盖而非
// 叠加：OFD-H 重新声明 image_format 后，OFD-A 的六种清单不应再出现在 OFD-H 的
// 规则集中，否则同一个码会被执行两次。
func TestProfileRuleOverrideReplacesInherited(t *testing.T) {
	seen := map[string]int{}
	for _, profile := range []*profile{profileFor(spec.DocTypeOFDA), profileFor(spec.DocTypeOFDH)} {
		counts := map[string]int{}
		for _, rule := range profile.resolvedRules() {
			counts[rule.Code]++
		}
		for code, count := range counts {
			if count != 1 {
				t.Errorf("profile %s 的规则 %q 出现 %d 次，应为 1 次", profile.Name, code, count)
			}
		}
		for code := range counts {
			seen[code]++
		}
	}
	// image_format 应在两个 profile 中都存在，且各自只有一份实现。
	if seen["image_format"] != 2 {
		t.Errorf("image_format 规则在两个 profile 中出现 %d 次，期望 2 次", seen["image_format"])
	}
}

// TestProfileInheritsArchiveRuleCount 保护覆盖机制不会把父规则集整体丢掉。
// OFD-H 覆盖 image_format 并新增 signature_coverage，因此规则数应比 OFD-A 多
// 恰好一条（覆盖不减数，新增加一）。
func TestProfileInheritsArchiveRuleCount(t *testing.T) {
	archive := profileFor(spec.DocTypeOFDA).resolvedRules()
	medical := profileFor(spec.DocTypeOFDH).resolvedRules()
	if len(medical) != len(archive)+1 {
		t.Errorf("OFD-H 规则数 = %d，OFD-A = %d；覆盖一条并新增一条后应多 1", len(medical), len(archive))
	}
	archiveCodes := map[string]bool{}
	for _, rule := range archive {
		archiveCodes[rule.Code] = true
	}
	// OFD-H 新增的规则（signature_coverage）不在 OFD-A 中，属预期。
	for _, rule := range medical {
		if !archiveCodes[rule.Code] && rule.Code != "signature_coverage" {
			t.Errorf("OFD-H 出现非预期的额外规则 %q", rule.Code)
		}
	}
}

// TestProfileSignatureCoverage 对应 GB/T 48666 8 c)：签名保护范围应涵盖除
// 注释列表与签名列表外的全部内容。用仓库内带签名的样例验证规则既能报出未覆盖
// 的文件，也不会把签名自身的产物误判为未保护。
func TestProfileSignatureCoverage(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "zsbk.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	// 样例本身声明 OFD-H 之外的基础 profile，改写后才会应用 48666 规则。
	data := rewriteDocType(t, original, spec.DocTypeOFDH)
	codes := profileIssues(t, data)
	if !containsCode(codes, "profile.ofd_h.signature_coverage") {
		t.Fatalf("带签名的 OFD-H 未报出签名覆盖问题，实际码: %v", codes)
	}

	// 补齐签名引用后不应再报：把包内除签名自身之外的文件都登记进 References。
	covered := coverAllPackageFiles(t, data)
	if codes := profileIssues(t, covered); containsCode(codes, "profile.ofd_h.signature_coverage") {
		t.Errorf("全部文件已登记进签名 References，仍报出覆盖问题: %v", codes)
	}

	// OFD-A 不含该规则——GB/T 42133 没有等价条款。
	if codes := profileIssues(t, data); containsCode(codes, "profile.ofd_a.signature_coverage") {
		t.Errorf("OFD-A 不应执行签名覆盖规则: %v", codes)
	}
}

// coverAllPackageFiles 重写每个 Signature.xml 的 References，把包内全部文件
// （签名自身产物除外）登记为已签名。
func coverAllPackageFiles(t *testing.T, data []byte) []byte {
	t.Helper()
	source, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, entry := range source.File {
		content, err := readZipEntry(entry)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(entry.Name, "Signature.xml") {
			content = []byte(withFullReferences(string(content), source))
		}
		target, err := writer.CreateHeader(&zip.FileHeader{Name: entry.Name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := target.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// withFullReferences 用包内全部非签名文件重建 References。样例的 Signature.xml
// 使用 ofd: 前缀，因此按带前缀与不带前缀两种形式匹配闭合标签。
func withFullReferences(signatureXML string, source *zip.Reader) string {
	var references strings.Builder
	for _, entry := range source.File {
		name := entry.Name
		base := path.Base(name)
		// Signature.xml 本身要纳入保护（规则只排除签名列表与签名值），
		// 签名列表与签名值是签名产物，不在保护范围内。
		if base == "Signatures.xml" || strings.HasSuffix(base, "SignedValue.dat") ||
			base == "OFD.xml" {
			continue
		}
		references.WriteString(`<ofd:Reference FileRef="/` + name + `"><ofd:CheckValue>AA==</ofd:CheckValue><ofd:CheckMethod>MD5</ofd:CheckMethod></ofd:Reference>`)
	}
	block := "<ofd:References>" + references.String() + "</ofd:References>"
	if start := strings.Index(signatureXML, "<ofd:References>"); start >= 0 {
		end := strings.Index(signatureXML, "</ofd:References>") + len("</ofd:References>")
		return signatureXML[:start] + block + signatureXML[end:]
	}
	// 没有 References 时插到 SignedValue 之前，保证是 Signature 的最后一个子元素。
	for _, closing := range []string{"</ofd:SignedValue>", "</SignedValue>"} {
		if strings.Contains(signatureXML, closing) {
			return strings.Replace(signatureXML, closing, block+closing, 1)
		}
	}
	return signatureXML + block
}

func readZipEntry(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
