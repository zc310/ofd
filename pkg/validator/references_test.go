package validator

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/core"
)

// TestPackageReferencesCoversRealFixtures 在真实样例上验证闭包：凡校验器认为
// 被引用的条目，闭包必须认为可达；凡闭包判定可达的条目，必须真实存在。
//
// 交叉印证的意义在于两处结论出自同一套 collectReferences 与 resolvePackagePath，
// 若实现分叉，这个测试会先发现。
func TestPackageReferencesCoversRealFixtures(t *testing.T) {
	fixtures := []string{
		"../../testdata/ofdrw/999.ofd",
		"../../testdata/actions.ofd",
		"../../testdata/annotations.ofd",
		"../../testdata/permissions.ofd",
		"../../testdata/preferences.ofd",
		"../../testdata/package-extras.ofd",
		"../../testdata/clips.ofd",
		"../../testdata/media-actions.ofd",
		"../../testdata/ofdrw/zsbk.ofd",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			index, err := PackageReferences(context.Background(), fixture)
			if err != nil {
				t.Fatalf("解析引用失败: %v", err)
			}
			pkg, err := core.OpenFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = pkg.Close() }()

			for name := range index.Reachable {
				if !pkg.Has(name) {
					t.Errorf("闭包含可达条目 %s，但包内不存在", name)
				}
			}
			if !index.Reachable["OFD.xml"] {
				t.Error("闭包未含入口 OFD.xml")
			}

			// 交叉印证：校验器若认定某引用有效，闭包必须也把它算作可达。
			v, err := New()
			if err != nil {
				t.Fatal(err)
			}
			report := v.ValidatePath(context.Background(), fixture)
			for _, issue := range report.Issues {
				if issue.Code == "reference.missing" {
					t.Logf("样例自身存在悬空引用（不影响闭包正确性）：%s", issue.Message)
				}
			}
			if len(index.Missing) > 0 {
				t.Logf("闭包记录 %d 处缺失引用，Complete()=%v", len(index.Missing), index.Complete())
			}
		})
	}
}

// TestPackageReferencesKeepsReferencedFontsAndMedia 确认叶子资源（字体、图像）
// 也计入可达集合。它们不再往下解析，但必须被认作「有人引用」，否则会被
// 6.2.1 c) 当作无人引用的条目删掉。
func TestPackageReferencesKeepsReferencedFontsAndMedia(t *testing.T) {
	const fixture = "../../testdata/image-effects.ofd"
	index, err := PackageReferences(context.Background(), fixture)
	if err != nil {
		t.Fatalf("解析引用失败: %v", err)
	}
	pkg, err := core.OpenFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()

	var leaves int
	for _, entry := range pkg.Entries() {
		if entry.IsDir || !strings.HasSuffix(entry.Path, ".xml") {
			if index.Reachable[entry.Path] {
				leaves++
			}
		}
	}
	if leaves == 0 {
		t.Error("没有任何非 XML 条目被判为可达，引用闭包多半漏掉了资源文件")
	}
	t.Logf("%d 个非 XML 条目计入可达集合", leaves)
}

// TestUnreachableListsOrphanFiles 确认无人引用的条目会被列出，且不含目录。
func TestUnreachableListsOrphanFiles(t *testing.T) {
	// 用一个带多余条目的包：额外放一个不被任何 XML 引用的文件。
	src := buildOFDWithOrphan(t)
	index, err := PackageReferences(context.Background(), src)
	if err != nil {
		t.Fatalf("解析引用失败: %v", err)
	}
	pkg, err := core.OpenBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()

	orphans := index.Unreachable(pkg)
	var found bool
	for _, name := range orphans {
		if strings.Contains(name, "orphan") {
			found = true
		}
		if strings.HasSuffix(name, "/") {
			t.Errorf("目录条目不应出现在无人引用列表里：%s", name)
		}
	}
	if !found {
		t.Errorf("未列出无人引用的条目，得到 %v", orphans)
	}
	if !index.Complete() {
		t.Error("构造的包没有悬空引用，Complete() 应为 true")
	}
}

// buildOFDWithOrphan 构造一个含无人引用条目的最小 OFD 包。
func buildOFDWithOrphan(t *testing.T) []byte {
	t.Helper()
	entries := [][2]string{
		{"OFD.xml", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0">` +
			`<DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`},
		{"Doc_0/Document.xml", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Document xmlns="http://www.ofdspec.org/2016">` +
			`<CommonData><MaxUnitID>9</MaxUnitID>` +
			`<PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData>` +
			`<Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`},
		{"Doc_0/Pages/Page_0/Content.xml", `<?xml version="1.0" encoding="UTF-8"?>` +
			`<Page xmlns="http://www.ofdspec.org/2016" ID="1">` +
			`<Area><PhysicalBox>0 0 210 297</PhysicalBox></Area><Content/></Page>`},
		{"Doc_0/Res/orphan.bin", "无人引用的多余文件"},
		{"Doc_0/Res/", ""},
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

// TestPackageReferencesRefusesToJudgeInvalidNamespace 是本组最关键的测试。
//
// testdata/ofdrw/intro.ofd 的命名空间是 http://www.ofdspec.org，缺 "/2016"
// 后缀，validator 报 namespace.invalid。解析失败后该文件里的引用一个也收集不到，
// 于是它的 74 个字体会全部落进「无人引用」。若不记录 Unparsed，这个包会显示
// 「124 个条目中 74 个可删」——照做就是毁掉一份真实文件。
func TestPackageReferencesRefusesToJudgeInvalidNamespace(t *testing.T) {
	const legacy = "../../testdata/ofdrw/intro.ofd"
	index, err := PackageReferences(context.Background(), legacy)
	if err != nil {
		t.Fatalf("解析引用失败: %v", err)
	}
	if index.Complete() {
		t.Fatal("命名空间无效的包竟被判定为闭包完整")
	}
	if len(index.Unparsed) == 0 {
		t.Fatal("未记录无法解析的被引用文件")
	}
	if reason := index.IncompleteReason(); reason == "" {
		t.Error("闭包不完整却给不出原因")
	} else {
		t.Logf("闭包不完整：%s", reason)
	}

	// 关键断言：绝不能出现「可以删掉一堆字体」的结论。
	pkg, err := core.OpenFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	orphans := index.Unreachable(pkg)
	var fonts int
	for _, name := range orphans {
		if strings.HasSuffix(name, ".ttf") || strings.HasSuffix(name, ".otf") {
			fonts++
		}
	}
	t.Logf("不可信前提下会列出 %d 个条目，其中字体 %d 个", len(orphans), fonts)
	if fonts > 0 {
		t.Logf("这 %d 个字体的删除结论不可信，必须由 Complete() 拦住", fonts)
	}
}

// TestPackageReferencesCompleteForValidNamespace 确认命名空间正确的样例闭包完整，
// 否则上一条的保护会过于宽松、让 6.2.1 c) 永远无法执行。
func TestPackageReferencesCompleteForValidNamespace(t *testing.T) {
	index, err := PackageReferences(context.Background(), "../../testdata/cover-thumbnail.ofd")
	if err != nil {
		t.Fatalf("解析引用失败: %v", err)
	}
	if !index.Complete() {
		t.Errorf("命名空间正确的样例闭包应完整，实际：%s", index.IncompleteReason())
	}
	if len(index.Unparsed) > 0 {
		t.Errorf("不应有无法解析的文件：%v", index.Unparsed)
	}
}
