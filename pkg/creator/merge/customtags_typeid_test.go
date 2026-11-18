package merge

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

func TestPagesPreservesCustomTagBusinessTypeWithoutNamespaceSynthesis(t *testing.T) {
	input, err := creator.Marshal(creator.Document{
		ID: "cert-input", Pages: []creator.Page{{}},
		CustomTags: []creator.CustomTag{{TypeID: "cert", DataName: "certificate.xml", Data: []byte("<Certificate/>")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Pages([]Source{{Data: input}, {Data: input}}, &output, PageOptions{}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	tags, err := ofd.Documents[0].LoadCustomTags()
	if err != nil {
		t.Fatal(err)
	}
	if tags == nil || len(tags.CustomTags) != 2 {
		t.Fatalf("合并标引数量错误: %+v", tags)
	}
	for _, tag := range tags.CustomTags {
		if tag.TypeID != "cert" || tag.NameSpace != "" {
			t.Fatalf("合并不能给业务类别加前缀或合成命名空间: %+v", tag)
		}
	}
	if tags.CustomTags[0].FileLoc == tags.CustomTags[1].FileLoc {
		t.Fatal("合并仍需重命名不同来源的数据文件")
	}
	instance, err := validator.New(validator.WithCustomTagTypeID(true))
	if err != nil {
		t.Fatal(err)
	}
	report := instance.ValidateReader(t.Context(), bytes.NewReader(output.Bytes()), "merged.ofd")
	if report.HasErrors() {
		t.Fatalf("合并产物未通过校验: %+v", report.Issues)
	}
}
