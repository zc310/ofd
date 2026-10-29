package merge

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/creator"
)

func TestPagesPreservesProfileAndRootRelativeCover(t *testing.T) {
	picture, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	source, err := creator.MarshalWithOptions(creator.Document{ID: "cover-profile", PageSize: creator.A4, CoverName: "cover.png", CoverData: picture, Pages: []creator.Page{{}, {}}}, creator.CreateOptions{DocType: "OFD-A"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Pages([]Source{{Data: source}}, &out, PageOptions{Pages: []int{2}}); err != nil {
		t.Fatal(err)
	}
	pkg, err := container.OpenBytes(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	root, err := pkg.Read("OFD.xml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(root, []byte(`DocType="OFD-A"`)) || !bytes.Contains(root, []byte("<ofd:Cover>")) {
		t.Fatalf("元数据丢失: %s", root)
	}
	cover, err := pkg.Read("Doc_0/Cover/cover.png")
	if err != nil || !bytes.Equal(cover, picture) {
		t.Fatalf("封面数据丢失: %v", err)
	}
}

func TestParsePagesRejectsUnboundedExpansion(t *testing.T) {
	if _, err := ParsePages("1-9223372036854775807"); err == nil {
		t.Fatal("未限制页码展开")
	}
}
