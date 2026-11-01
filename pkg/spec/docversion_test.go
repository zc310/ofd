package spec_test

import (
	"encoding/xml"
	"reflect"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/spec"
)

func TestDocVersionXMLPreservesFilePathsIDsAndOptionalMetadata(t *testing.T) {
	// GB/T 33190—2016 §19.2表71；文件路径保留原文，由加载层按版本描述文件定位。
	for _, source := range []string{
		`<DocVersion ID="old"><FileList><File ID="content">../Pages/Page_0/Content.xml</File><File ID="font">/Res/font.ttf</File></FileList><DocRoot>Document.xml</DocRoot></DocVersion>`,
		`<DocVersion ID="old" Version="" Name="历史版" CreationDate="2026-01-02T03:04:05.123456789+08:00"><FileList><File ID="content">../Pages/Page_0/Content.xml</File><File ID="font">/Res/font.ttf</File></FileList><DocRoot>Document.xml</DocRoot></DocVersion>`,
	} {
		var value spec.DocVersion
		if err := xml.Unmarshal([]byte(source), &value); err != nil {
			t.Fatal(err)
		}
		if value.ID != "old" || value.DocRoot != "Document.xml" || !reflect.DeepEqual(value.FileList.Files, []spec.VersionFile{{ID: "content", Path: "../Pages/Page_0/Content.xml"}, {ID: "font", Path: "/Res/font.ttf"}}) {
			t.Fatalf("版本文件列表改变：%+v", value)
		}
		if value.Name != nil {
			want, err := time.Parse(time.RFC3339Nano, "2026-01-02T03:04:05.123456789+08:00")
			if err != nil {
				t.Fatal(err)
			}
			if *value.Name != "历史版" || value.Version == nil || *value.Version != "" || value.CreationDate == nil || !value.CreationDate.Equal(want) {
				t.Fatal("版本可选元数据或日期精度丢失")
			}
		} else if value.Version != nil || value.CreationDate != nil {
			t.Fatal("未声明的版本元数据被填充")
		}
		data, err := xml.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip spec.DocVersion
		if err := xml.Unmarshal(data, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(value, roundTrip) {
			t.Fatalf("版本XML往返改变字段：%s", data)
		}
	}
}
