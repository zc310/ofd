package securexml

import (
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestDecodeOFDAllowsStandardMetadataCollectionsWithoutRelaxingSingleValues(t *testing.T) {
	raw := `<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocInfo><DocID>doc</DocID>
<Keywords><Keyword>一</Keyword><Keyword>二</Keyword></Keywords>
<CustomDatas><CustomData Name="certificateName">示例</CustomData><CustomData Name="ZZBH">001</CustomData></CustomDatas>
</DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`
	var root spec.OFD
	if err := Decode(t.Context(), []byte(raw), "OFD", &root); err != nil {
		t.Fatal(err)
	}
	if len(root.DocBodies[0].DocInfo.CustomDatas.CustomData) != 2 || len(root.DocBodies[0].DocInfo.Keywords.Keyword) != 2 {
		t.Fatal("标准元数据集合未完整解码")
	}
	for _, invalid := range []string{
		`<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocInfo><DocID>a</DocID><DocID>b</DocID></DocInfo></DocBody></OFD>`,
		`<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><DocInfo><CustomDatas/><CustomDatas/></DocInfo></DocBody></OFD>`,
		`<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><CustomDatas><CustomData/><CustomData/></CustomDatas></SignedInfo></Signature>`,
		`<OFD xmlns="http://www.ofdspec.org/2016"><DocBody><CustomData/><CustomData/></DocBody></OFD>`,
	} {
		var target any
		name := "OFD"
		if invalid[:10] == "<Signature" {
			name = "Signature"
		}
		if err := Decode(t.Context(), []byte(invalid), name, &target); err == nil {
			t.Fatalf("不应放宽非集合或其他根: %s", invalid)
		}
	}
}
