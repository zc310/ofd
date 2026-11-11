package spec_test

import (
	"encoding/xml"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestSignaturesDecodeKeepsIndexSeparateFromDescriptions(t *testing.T) {
	data := `<Signatures xmlns="http://www.ofdspec.org/2016"><MaxSignId>s20</MaxSignId><Signature ID="s19" BaseLoc="Signs/../Signs/first.xml" Type="Sign"/><Signature ID="s20" BaseLoc="Signs/second.xml" Type="Seal" Relative="s19"/></Signatures>`
	var index spec.Signatures
	if err := xml.Unmarshal([]byte(data), &index); err != nil {
		t.Fatal(err)
	}
	if index.XMLName.Space != spec.Namespace || index.MaxSignID == nil || *index.MaxSignID != "s20" || len(index.Signatures) != 2 {
		t.Fatalf("签名索引声明丢失: %+v", index)
	}
	if index.Signatures[0].ID != "s19" || index.Signatures[0].BaseLoc != "Signs/../Signs/first.xml" || index.Signatures[1].Relative != "s19" {
		t.Fatalf("签名顺序、原始路径或基础签名关联改变: %+v", index.Signatures)
	}
	var omitted spec.Signatures
	if err := xml.Unmarshal([]byte(`<Signatures><Signature ID="12" BaseLoc="missing.xml"/></Signatures>`), &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.MaxSignID != nil || omitted.Signatures[0].ID != "12" || omitted.Signatures[0].Type != "" {
		t.Fatal("解码不应映射数字标识、补充类型或读取文件")
	}
}

func TestSignatureDecodePreservesDigestTextAndExtensions(t *testing.T) {
	// 只检查 XML 声明，Base64 文本和算法均留给验证链解释，不在模型层认证。
	data := `<Signature ID="s1"><SignedInfo><Provider ProviderName="测试组件" Version="1" Company="厂商" ProtocolVer="2"><ExtendData>扩展</ExtendData></Provider><SignatureMethod>自定义算法</SignatureMethod><SignatureDateTime>原始时间</SignatureDateTime><Parameters><Parameter Name="policy" Type="string">归档</Parameter></Parameters><References CheckMethod="SHA256"><Reference FileRef="/Doc_0/../Doc_0/Document.xml"><CheckValue> YWJj </CheckValue></Reference></References><StampAnnot ID="stamp_1" PageRef="7" Boundary="1 2 30 40" Clip="3 4 5 6"/><StampAnnot ID="stamp_2" PageRef="8" Boundary="0 0 30 40"/><Seal><BaseLoc>Seal.dat</BaseLoc><ImageLoc>Seal.png</ImageLoc></Seal></SignedInfo><SignedValue>Signs/../value.dat</SignedValue></Signature>`
	var signature spec.Signature
	if err := xml.Unmarshal([]byte(data), &signature); err != nil {
		t.Fatal(err)
	}
	info := signature.SignedInfo
	if signature.ID != "s1" || signature.SignedValue != "Signs/../value.dat" || info.SignatureDateTime != "原始时间" || info.SignatureMethod != "自定义算法" {
		t.Fatalf("签名原始声明改变: %+v", signature)
	}
	if info.Provider.ProtocolVer != "2" || info.Provider.ExtendData != "扩展" || info.Parameters.Items[0].Value != "归档" {
		t.Fatal("GM/T 0099 扩展声明丢失")
	}
	ref := info.References.Reference[0]
	if ref.FileRef != "/Doc_0/../Doc_0/Document.xml" || string(ref.CheckValue) != " YWJj " || info.References.CheckMethod != "SHA256" {
		t.Fatalf("摘要文本不应解码或修改: %+v", ref)
	}
	if len(info.StampAnnot) != 2 || info.StampAnnot[0].ID != "stamp_1" || info.StampAnnot[0].PageRef != 7 || info.StampAnnot[0].Clip != (spec.StBox{X: 3, Y: 4, Width: 5, Height: 6}) || info.StampAnnot[1].Clip != (spec.StBox{}) {
		t.Fatalf("签名外观顺序或裁剪改变: %+v", info.StampAnnot)
	}
	if info.Seal.BaseLoc != "Seal.dat" || info.Seal.ImageLoc != "Seal.png" {
		t.Fatalf("电子印章路径改变: %+v", info.Seal)
	}
}
