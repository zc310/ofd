package spec_test

import (
	"encoding/xml"
	"fmt"

	"github.com/zc310/ofd/pkg/spec"
)

func ExampleOFD() {
	var root spec.OFD
	data := []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><Title>示例文档</Title></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`)
	if err := xml.Unmarshal(data, &root); err != nil {
		panic(err)
	}
	body := root.DocBodies[0]
	fmt.Println(*body.DocInfo.Title)
	fmt.Println(body.DocRoot.Resolve("/"))
	// Output:
	// 示例文档
	// /Doc_0/Document.xml
}
