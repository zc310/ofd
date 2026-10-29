package preserve

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator/replace"
)

func TestPreserveChecksInputLimitsBeforeParsingXML(t *testing.T) {
	files := pageAreaFiles(areaA4)
	files["OFD.xml"] = "<broken"
	for _, tc := range []struct {
		name    string
		limits  replace.Limits
		message string
	}{
		{"entry bytes", replace.Limits{MaxEntryBytes: 1}, "上限"},
		{"total bytes", replace.Limits{MaxTotalBytes: 1}, "上限"},
		{"entry count", replace.Limits{MaxEntries: 1}, "上限"},
		{"negative", replace.Limits{MaxEntryBytes: -1}, "负数"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := buildOFD(t, files)
			options := Options{DocType: "OFD-A", Limits: tc.limits}
			if _, err := Plan(source, options); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("Plan 应在解析 XML 前拒绝输入: %v", err)
			}
			var out bytes.Buffer
			if _, err := Apply(source, &out, options); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("Apply 应在解析 XML 前拒绝输入: %v", err)
			}
			if out.Len() != 0 {
				t.Fatal("拒绝输入后不应写出文件")
			}
		})
	}
}

func TestPreserveLimitsIncludeUnreferencedBinaryEntries(t *testing.T) {
	files := pageAreaFiles("")
	files["unused.bin"] = strings.Repeat("x", 2048)
	if _, err := Plan(buildOFD(t, files), Options{DocType: "OFD-A", Limits: replace.Limits{MaxEntryBytes: 1024}}); err == nil || !strings.Contains(err.Error(), "unused.bin") {
		t.Fatalf("未引用二进制条目也应受输入限制: %v", err)
	}
}

func TestPreserveInputTotalBudgetDoesNotOverflow(t *testing.T) {
	var source bytes.Buffer
	zw := zip.NewWriter(&source)
	for _, header := range []*zip.FileHeader{{Name: "large.bin", UncompressedSize64: 1<<63 - 1}, {Name: "extra.bin", UncompressedSize64: 1}} {
		if _, err := zw.CreateRaw(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := Plan(source.Bytes(), Options{DocType: "OFD-A", Limits: replace.Limits{MaxEntryBytes: 1<<63 - 1, MaxTotalBytes: 1<<63 - 1}})
	if err == nil || !strings.Contains(err.Error(), "解压总大小超过上限") {
		t.Fatalf("元数据总大小溢出时应在解压前拒绝: %v", err)
	}
}
