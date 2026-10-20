package merge

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeRejectsNestedDocumentDirectoriesInEitherOrder(t *testing.T) {
	for _, dirs := range [][2]string{{"Outer", "Outer/Inner"}, {"Outer/Inner", "Outer"}} {
		t.Run(dirs[0], func(t *testing.T) {
			input := writeDocumentDirectories(t, dirs[:])
			err := Files([]string{input}, &bytes.Buffer{}, Options{})
			if err == nil || !strings.Contains(err.Error(), "重叠") {
				t.Fatalf("应拒绝重叠目录，实际 %v", err)
			}
		})
	}
}

func TestMergeKeepsSiblingDocumentDirectories(t *testing.T) {
	input := writeDocumentDirectories(t, []string{"Outer/One", "Outer/OneMore"})
	var out bytes.Buffer
	if err := Files([]string{input}, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	pkg := openMerged(t, out.Bytes())
	for _, name := range []string{"Doc_0/Document.xml", "Doc_1/Document.xml"} {
		if !pkg.Has(name) {
			t.Errorf("合并结果缺少 %s", name)
		}
	}
}

func writeDocumentDirectories(t *testing.T, dirs []string) string {
	t.Helper()
	input := filepath.Join(t.TempDir(), "directories.ofd")
	var root strings.Builder
	root.WriteString(`<OFD xmlns="http://www.ofdspec.org/2016">`)
	entries := []ofdEntry{}
	for _, dir := range dirs {
		root.WriteString("<DocBody><DocRoot>" + dir + "/Document.xml</DocRoot></DocBody>")
		entries = append(entries, ofdEntry{dir + "/Document.xml", testDocumentXML})
	}
	root.WriteString("</OFD>")
	entries = append(entries, ofdEntry{"OFD.xml", root.String()})
	writeTestOFD(t, input, entries)
	return input
}
