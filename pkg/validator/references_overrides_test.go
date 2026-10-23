package validator

import (
	"context"
	"strings"
	"testing"
)

func TestReferenceOverridesEnforcePathsAndBudgets(t *testing.T) {
	const root = `<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocRoot>Doc.xml</DocRoot></DocBody></OFD>`
	const doc = `<Document xmlns="http://www.ofdspec.org/2016"/>`
	source := makeArchive(t, map[string]string{"OFD.xml": root, "Doc.xml": doc})
	for _, tc := range []struct {
		name      string
		overrides map[string][]byte
		options   []Option
		message   string
	}{
		{"missing", map[string][]byte{"missing.xml": []byte(doc)}, nil, "不存在"},
		{"absolute", map[string][]byte{"/Doc.xml": []byte(doc)}, nil, "路径"},
		{"traversal", map[string][]byte{"../Doc.xml": []byte(doc)}, nil, "路径"},
		{"file limit", map[string][]byte{"Doc.xml": []byte(doc + strings.Repeat(" ", 200))}, []Option{WithMaxFileSize(200)}, "单条大小"},
		{"total limit", map[string][]byte{"Doc.xml": []byte(doc + strings.Repeat(" ", 200))}, []Option{WithMaxFileSize(1024), WithMaxTotalSize(300)}, "总大小"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := PackageReferencesWithOverrides(context.Background(), source, tc.overrides, tc.options...); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("应拒绝非法替换或超限内容: %v", err)
			}
		})
	}
}

func TestMalformedReferenceOverrideMakesClosureIncomplete(t *testing.T) {
	source := makeArchive(t, map[string]string{
		"OFD.xml": `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>closure-test</DocID></DocInfo><DocRoot>Doc.xml</DocRoot></DocBody></OFD>`,
		"Doc.xml": `<Document xmlns="http://www.ofdspec.org/2016"/>`,
	})
	index, err := PackageReferencesWithOverrides(context.Background(), source, map[string][]byte{"Doc.xml": []byte("<broken")})
	if err != nil {
		t.Fatal(err)
	}
	if index.Complete() || len(index.Unparsed) != 1 || index.Unparsed[0] != "Doc.xml" {
		t.Fatalf("替换 XML 无法解析时不能报告可信闭包: %+v", index)
	}
}

func TestReferenceClosureRequiresKnownStructureEvenWhenXSDOptionIsSkipped(t *testing.T) {
	page := []byte(`<Page xmlns="http://www.ofdspec.org/2016"><Unknown><FileLoc>uninterpreted.bin</FileLoc></Unknown></Page>`)
	index, err := PackageReferencesWithOverrides(t.Context(), buildOFDWithOrphan(t), map[string][]byte{"Doc_0/Pages/Page_0/Content.xml": page}, WithSkipXSD(true))
	if err != nil {
		t.Fatal(err)
	}
	if index.Complete() || len(index.Unparsed) != 1 || index.Unparsed[0] != "Doc_0/Pages/Page_0/Content.xml" || !index.Reachable["Doc_0/Pages/Page_0/Content.xml"] {
		t.Fatalf("未知结构不能作完整闭包: %+v", index)
	}
}
