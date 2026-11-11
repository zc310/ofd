package crypt_test

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/spec"
)

// 先创建真实 OFD，再通过既有加密写端生成元数据，公开声明只负责解码，不提供编码器。
func TestCreatorAndEncryptOutputDecodesThroughPublicEncryptionModels(t *testing.T) {
	plain, err := creator.Marshal(creator.Document{ID: "public-encryption-models", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, independent := range []bool{false, true} {
		t.Run(map[bool]string{false: "共享密钥", true: "独立密钥"}[independent], func(t *testing.T) {
			password := []byte("测试口令")
			var encrypted bytes.Buffer
			report, err := crypt.Encrypt(t.Context(), plain, &encrypted, crypt.EncryptOptions{
				Recipients:   []crypt.EncryptRecipient{{UserName: "测试用户", Password: password}},
				PlaintextMap: true, IndependentKeys: independent,
			})
			if err != nil {
				t.Fatal(err)
			}
			physical := unpack(t, encrypted.Bytes())
			var root spec.Encryptions
			if err := xml.Unmarshal([]byte(physical[spec.EncryptionEntryPoint]), &root); err != nil {
				t.Fatal(err)
			}
			if root.XMLName.Space != spec.Namespace || len(root.Operations) != 1 || root.Operations[0].ID != report.OperationID {
				t.Fatalf("加密写端的操作标识或命名空间改变: %+v", root)
			}
			op := root.Operations[0]
			if op.Provider.Name != "ofd-crypt" || op.Scope != "All" {
				t.Fatalf("加密写端的组件或范围声明改变: %+v", op)
			}
			checkSeed := func(name string) {
				t.Helper()
				var seed spec.DecryptSeed
				if err := xml.Unmarshal([]byte(physical[strings.TrimPrefix(name, "/")]), &seed); err != nil {
					t.Fatal(err)
				}
				if seed.XMLName.Space != spec.Namespace || seed.ID != op.ID || seed.Scheme != crypt.PasswordScheme || len(seed.Users) != 1 || seed.Users[0].Name != "测试用户" || seed.ExtendParams == nil {
					t.Fatalf("加密写端的密钥描述或接收者改变: %+v", seed)
				}
				user := seed.Users[0]
				wrapped, err := base64.StdEncoding.DecodeString(user.WrappedKey)
				if err != nil || len(wrapped) != 32 || user.Type != "User" || user.IV == nil {
					t.Fatal("真实加密产物的包装密钥文本、角色或 IV 丢失")
				}
				iv, err := base64.StdEncoding.DecodeString(*user.IV)
				if err != nil || len(iv) != 16 {
					t.Fatal("真实加密产物的 IV 文本无法独立解码")
				}
			}
			checkSeed(op.DecryptSeedLoc)
			var mappings spec.EncryptedEntries
			if err := xml.Unmarshal([]byte(physical[strings.TrimPrefix(op.EntriesMapLoc, "/")]), &mappings); err != nil {
				t.Fatal(err)
			}
			if mappings.XMLName.Local != "EncryptedEntries" || mappings.ID != op.ID || len(mappings.Entries) != report.EncryptedEntries {
				t.Fatalf("真实加密产物的明密文映射改变: %+v", mappings)
			}
			originals := unpack(t, plain)
			for _, mapping := range mappings.Entries {
				if _, exists := originals[strings.TrimPrefix(mapping.Path, "/")]; !exists || !strings.HasPrefix(mapping.EPath, "/") {
					t.Fatalf("明文路径或密文绝对路径改变: %+v", mapping)
				}
				if (mapping.Seed != "") != independent {
					t.Fatalf("独立密钥引用状态改变: %+v", mapping)
				}
				if independent {
					checkSeed(mapping.Seed)
				}
			}
			info, err := crypt.Inspect(t.Context(), encrypted.Bytes(), crypt.InspectOptions{})
			if err != nil || info.Operations[0].ID != op.ID || len(info.Operations[0].Entries) != len(mappings.Entries) {
				t.Fatalf("公开声明与既有安全检查结果不一致: %+v, %v", info, err)
			}
			var restored bytes.Buffer
			if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &restored, crypt.DecryptOptions{Password: password}); err != nil {
				t.Fatal(err)
			}
			checkOriginals(t, restored.Bytes(), originals)
		})
	}
}

func TestPublicEncryptionDecodeDoesNotReplaceSecureMetadataPreflight(t *testing.T) {
	// 标准库解码只取声明，重复单值元素仍必须由安全读取链拒绝。
	const data = `<Encryptions xmlns="http://www.ofdspec.org/2016"><EncryptInfo ID="e1"><Provider Name="测试" Version="1"/><EncryptScope>All</EncryptScope><EncryptScope>other</EncryptScope><DecryptSeedLoc>seed.xml</DecryptSeedLoc><EntriesMapLoc>map.xml</EntriesMapLoc></EncryptInfo></Encryptions>`
	var root spec.Encryptions
	if err := xml.Unmarshal([]byte(data), &root); err != nil {
		t.Fatal(err)
	}
	_, err := crypt.Inspect(t.Context(), archive(t, map[string]string{spec.EncryptionEntryPoint: data}), crypt.InspectOptions{})
	if !errors.Is(err, crypt.ErrInvalidMetadata) || !strings.Contains(err.Error(), "单值元素重复") {
		t.Fatalf("迁移声明不能绕过安全 XML 预检: %v", err)
	}
}
