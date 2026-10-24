package xmlcodec

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beevik/etree"
)

func TestParseOFDPackageXMLMatchesExistingReader(t *testing.T) {
	for _, name := range []string{"hello.ofd", "actions.ofd", "annotations.ofd"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "ofd-creator", name)
			pkg, err := zip.OpenReader(path)
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("缺少 OFD 样本 %s", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer pkg.Close()
			count := 0
			for _, entry := range pkg.File {
				if !strings.HasSuffix(entry.Name, ".xml") {
					continue
				}
				count++
				t.Run(entry.Name, func(t *testing.T) {
					reader, err := entry.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, readErr := io.ReadAll(reader)
					closeErr := reader.Close()
					if readErr != nil || closeErr != nil {
						t.Fatalf("读取 XML 失败: %v", errors.Join(readErr, closeErr))
					}
					reference := etree.NewDocument()
					reference.ReadSettings.PreserveCData = true
					if err := reference.ReadFromBytes(referenceXMLInput(data)); err != nil {
						t.Fatal(err)
					}
					doc, err := Parse(data)
					if err != nil {
						t.Fatal(err)
					}
					want, err := Marshal(reference)
					if err != nil {
						t.Fatal(err)
					}
					got, err := Marshal(doc)
					if err != nil || !bytes.Equal(want, got) {
						t.Fatalf("OFD XML 读取结果改变: err=%v\nwant %s\ngot %s", err, want, got)
					}
				})
			}
			if count == 0 {
				t.Fatal("样本不包含 XML")
			}
		})
	}
}
