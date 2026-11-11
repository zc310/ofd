package spec_test

import (
	"encoding/xml"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestEncryptionsDecodePreservesRawDeclarationsAndProviderExtensions(t *testing.T) {
	data := `<Encryptions xmlns="http://www.ofdspec.org/2016"><EncryptInfo ID="e2" Relative="e1"><Provider Name="测试" Version="1" Company="厂商" ProtocolVer="2"><ExtendData> YWJj </ExtendData></Provider><EncryptScope> 自定义范围 </EncryptScope><Parameters><Parameter Name="policy" Type="string">归档</Parameter></Parameters><EncryptDate>原始日期</EncryptDate><DecryptSeedLoc>Seeds/../seed.xml</DecryptSeedLoc><EntriesMapLoc>/Security/map.xml</EntriesMapLoc></EncryptInfo><EncryptInfo ID="e1"><Provider Name="初始" Version="1"/></EncryptInfo></Encryptions>`
	var root spec.Encryptions
	if err := xml.Unmarshal([]byte(data), &root); err != nil {
		t.Fatal(err)
	}
	if root.XMLName.Space != spec.Namespace || len(root.Operations) != 2 || root.Operations[0].ID != "e2" || root.Operations[1].ID != "e1" {
		t.Fatalf("加密操作顺序或命名空间改变: %+v", root)
	}
	info := root.Operations[0]
	if info.Relative != "e1" || info.Scope != " 自定义范围 " || info.EncryptDate != "原始日期" || info.DecryptSeedLoc != "Seeds/../seed.xml" || info.EntriesMapLoc != "/Security/map.xml" {
		t.Fatalf("原始加密声明不应补值、解释或规范化: %+v", info)
	}
	if info.Provider.ProtocolVer != "2" || info.Provider.ExtendData != " YWJj " || info.Parameters.Items[0].Name != "policy" || info.Parameters.Items[0].Value != "归档" {
		t.Fatal("加密组件或操作扩展声明丢失")
	}
	if root.Operations[1].Parameters != nil || root.Operations[1].Relative != "" || root.Operations[1].EncryptDate != "" {
		t.Fatal("缺省加密声明被提前填入")
	}
}

func TestDecryptSeedDecodePreservesBase64TextAndOptionalIV(t *testing.T) {
	const params = `<Parameter Name="vendor" Value="a">原始扩展</Parameter>`
	data := `<DecyptSeed xmlns="http://www.ofdspec.org/2016" ID="12" EncryptCaseId="custom"><UserInfo UserName="甲"><UserCert> invalid-base64 </UserCert><EncryptedWK> YWJj </EncryptedWK></UserInfo><UserInfo UserName="乙" UserType="Owner"><EncryptedWK>wrapped</EncryptedWK><IVValue/></UserInfo><UserInfo UserName="丙"><EncryptedWK>wrapped</EncryptedWK><IVValue> AAAA </IVValue></UserInfo><ExtendParams>` + params + `</ExtendParams></DecyptSeed>`
	var seed spec.DecryptSeed
	if err := xml.Unmarshal([]byte(data), &seed); err != nil {
		t.Fatal(err)
	}
	if seed.XMLName.Local != "DecyptSeed" || seed.ID != "12" || seed.Scheme != "custom" || len(seed.Users) != 3 {
		t.Fatalf("密钥描述拼写、原始标识或接收者顺序改变: %+v", seed)
	}
	if seed.Users[0].Type != "" || seed.Users[0].IV != nil || seed.Users[0].Certificate != " invalid-base64 " || seed.Users[0].WrappedKey != " YWJj " {
		t.Fatal("密钥描述不应解码 Base64、验证证书或填充默认角色和 IV")
	}
	if seed.Users[1].Type != "Owner" || seed.Users[1].IV == nil || *seed.Users[1].IV != "" || seed.Users[2].IV == nil || *seed.Users[2].IV != " AAAA " {
		t.Fatal("缺省、显式空值和给定 IV 不应混同")
	}
	if seed.ExtendParams == nil || seed.ExtendParams.InnerXML != params {
		t.Fatal("密钥描述扩展参数子内容丢失")
	}
	for _, tc := range []struct {
		data    string
		present bool
	}{
		{`<DecyptSeed/>`, false},
		{`<DecyptSeed><ExtendParams/></DecyptSeed>`, true},
	} {
		var omitted spec.DecryptSeed
		if err := xml.Unmarshal([]byte(tc.data), &omitted); err != nil {
			t.Fatal(err)
		}
		if (omitted.ExtendParams != nil) != tc.present || omitted.Scheme != "" {
			t.Fatal("扩展参数存在状态或缺省方案被改变")
		}
	}
}

func TestEncryptedEntriesDecodePreservesRootSpellingAndLexicalPaths(t *testing.T) {
	for _, name := range []string{"EncryptedEntries", "EncryptEntries"} {
		t.Run(name, func(t *testing.T) {
			data := `<` + name + ` xmlns="http://www.ofdspec.org/2016" ID="e1"><EncryptEntry Path="/Doc_0/a&amp;b.xml" EPath="/Cipher/../cipher.dat" DecryptSeedLoc="seed.xml"/><EncryptEntry Path="/Doc_0/empty.xml"/></` + name + `>`
			var entries spec.EncryptedEntries
			if err := xml.Unmarshal([]byte(data), &entries); err != nil {
				t.Fatal(err)
			}
			if entries.XMLName.Local != name || entries.XMLName.Space != spec.Namespace || entries.ID != "e1" || len(entries.Entries) != 2 {
				t.Fatalf("映射表根拼写或顺序改变: %+v", entries)
			}
			first, second := entries.Entries[0], entries.Entries[1]
			if first.Path != "/Doc_0/a&b.xml" || first.EPath != "/Cipher/../cipher.dat" || first.Seed != "seed.xml" || second.EPath != "" || second.Seed != "" {
				t.Fatal("声明解码不应规范化路径、拒绝词法路径或填充 EPath/Seed")
			}
		})
	}
}
