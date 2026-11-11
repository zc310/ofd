package models

import (
	"encoding/xml"
	"testing"
)

func TestSignatureXMLDecodeDoesNotPopulateLoadingContext(t *testing.T) {
	var signature Signature
	data := `<Signature ID="s1" Type="Seal" Relative="forged" FilePath="forged.xml"><SignedInfo><Provider ProviderName="测试"/><References/></SignedInfo><SignedValue>value.dat</SignedValue></Signature>`
	if err := xml.Unmarshal([]byte(data), &signature); err != nil {
		t.Fatal(err)
	}
	if signature.ID != "s1" || signature.SignedInfo.Provider.ProviderName != "测试" || signature.SignedValue != "value.dat" {
		t.Fatalf("内嵌标准声明未解码: %+v", signature)
	}
	if signature.Type != "" || signature.Relative != "" || signature.FilePath != "" {
		t.Fatal("XML 不应填入解析器维护的加载上下文")
	}
}
