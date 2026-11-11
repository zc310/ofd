package integrity_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/integrity"
	"github.com/zc310/ofd/pkg/spec"
)

// 给真实创建产物附加声明清单，验证原始公开模型与检查器报告的路径语义分离。
// 本用例不提供签名或时间戳，检查结果必须保持 incomplete。
func TestIntegrityInspectReusesPublicManifestAndKeepsReportSemantics(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "spec-integrity", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, rootName := range []string{"OFDEntries", "DocEntries"} {
		t.Run(rootName, func(t *testing.T) {
			var physical bytes.Buffer
			writer := zip.NewWriter(&physical)
			var content strings.Builder
			fmt.Fprintf(&content, `<%s xmlns="%s" ID="manifest1" CreatorName="测试" CreationDate="2026-01-02"><FileList>`, rootName, spec.Namespace)
			for index, file := range archive.File {
				if err := writer.Copy(file); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&content, `<File ID="file%d" FileLoc="/`, index+1)
				if err := xml.EscapeText(&content, []byte(file.Name)); err != nil {
					t.Fatal(err)
				}
				content.WriteString(`"/>`)
			}
			fmt.Fprintf(&content, `</FileList></%s>`, rootName)
			entry, err := writer.Create(spec.IntegrityEntryPoint)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte(content.String())); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			var raw spec.IntegrityManifest
			if err := xml.Unmarshal([]byte(content.String()), &raw); err != nil {
				t.Fatal(err)
			}
			report, err := integrity.Inspect(t.Context(), physical.Bytes(), integrity.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if report == nil || report.Manifest == nil || len(report.Manifest.Files) == 0 {
				t.Fatalf("检查器未返回完整性清单: %+v", report)
			}
			manifest := report.Manifest
			firstFile := manifest.Files[0]
			if !report.Present || report.Status != "incomplete" || len(report.Unlisted)+len(report.Missing) != 0 || len(manifest.Files) != len(raw.Files) {
				t.Fatalf("声明迁移改变了完整性报告结论: %+v", report)
			}
			if !strings.HasPrefix(raw.Files[0].Location, "/") || firstFile.Location != strings.TrimPrefix(raw.Files[0].Location, "/") || manifest.XMLName.Local != rootName {
				t.Fatal("原始声明应保留词法路径，检查器报告仍使用规范 ZIP 路径")
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte("XMLName")) || !bytes.Contains(encoded, []byte(`"manifest":{"id":"manifest1"`)) || !bytes.Contains(encoded, []byte(`"location":`)) {
				t.Fatalf("已发布报告的 JSON 字段改变: %s", encoded)
			}
		})
	}
}
