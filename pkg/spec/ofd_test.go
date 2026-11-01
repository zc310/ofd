package spec_test

import (
	"encoding/xml"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestOFDXMLPreservesDocumentBodiesMetadataAndVersionEntrypoints(t *testing.T) {
	// GB/T 33190—2016 §7.4表3/4及§19.1表70；模型迁移不改XML字段及顺序。
	source := []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">
<DocBody><DocInfo><DocID>first</DocID><Title/><Author>作者</Author><CreationDate>2026-01-02</CreationDate><Cover>Res/Cover.png</Cover><Keywords><Keyword>甲</Keyword><Keyword>乙</Keyword><Keyword>甲</Keyword></Keywords><CustomDatas><CustomData Name="same">  &amp;一  </CustomData><CustomData Name="same">二</CustomData></CustomDatas></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot><Versions><Version ID="previous" Index="2" BaseLoc="Versions/Previous.xml"/><Version ID="latest" Index="3" Current="true" BaseLoc="Versions/Latest.xml"/></Versions><Signatures>Doc_0/Signatures.xml</Signatures></DocBody>
<DocBody><DocInfo><DocID>second</DocID></DocInfo><DocRoot>/Doc_1/Document.xml</DocRoot></DocBody>
</OFD>`)
	check := func(value *spec.OFD) {
		t.Helper()
		if value.XMLName.Local != "OFD" || value.Version != "1.0" || value.DocType != spec.DocTypeOFD || len(value.DocBodies) != 2 {
			t.Fatalf("主入口字段丢失：%+v", value)
		}
		first, second := value.DocBodies[0], value.DocBodies[1]
		if first.DocInfo.DocID != "first" || first.DocRoot != "Doc_0/Document.xml" || first.Signatures == nil || *first.Signatures != "Doc_0/Signatures.xml" {
			t.Fatalf("首个文档体丢失：%+v", first)
		}
		info := first.DocInfo
		if info.Title == nil || *info.Title != "" || info.Author == nil || *info.Author != "作者" || info.CreationDate == nil || info.CreationDate.Format("2006-01-02") != "2026-01-02" || info.Cover == nil || *info.Cover != "Res/Cover.png" {
			t.Fatalf("元数据可选值丢失：%+v", info)
		}
		if info.Keywords == nil || !reflect.DeepEqual(info.Keywords.Keyword, []string{"甲", "乙", "甲"}) {
			t.Fatal("关键词顺序或重复值丢失")
		}
		if info.CustomDatas == nil || !reflect.DeepEqual(info.CustomDatas.CustomData, []spec.CustomData{{Name: "same", Value: "  &一  "}, {Name: "same", Value: "二"}}) {
			t.Fatal("自定义数据名称、顺序或空白丢失")
		}
		if first.Versions == nil || !reflect.DeepEqual(first.Versions.VersionList, []spec.Version{{ID: "previous", Index: 2, BaseLoc: "Versions/Previous.xml"}, {ID: "latest", Index: 3, Current: true, BaseLoc: "Versions/Latest.xml"}}) {
			t.Fatal("版本入口及Current缺省值改变")
		}
		if second.DocInfo.DocID != "second" || second.DocRoot != "/Doc_1/Document.xml" || second.Signatures != nil || second.Versions != nil || second.DocInfo.Title != nil || second.DocInfo.CreationDate != nil || second.DocInfo.Keywords != nil || second.DocInfo.CustomDatas != nil {
			t.Fatalf("未声明字段被填充：%+v", second)
		}
	}
	var value spec.OFD
	if err := xml.Unmarshal(source, &value); err != nil {
		t.Fatal(err)
	}
	check(&value)
	data, err := xml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip spec.OFD
	if err := xml.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	check(&roundTrip)
}
