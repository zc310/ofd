package merge

import (
	"fmt"
	"path"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/ofdxml"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/spec"
)

type signatureEntryKind uint8

const (
	signatureList signatureEntryKind = iota + 1
	signatureDocument
	signatureData
)

// readSignatureEntries 按 GB/T 33190—2016 §18.1、§18.2 表 66、67 发现
// 签名的实际文件。清单可与正文共用目录，不能把它的父目录视为签名专属目录。
func readSignatureEntries(pkg *core.Package, body *etree.Element, maxBytes int64) (map[string]signatureEntryKind, error) {
	location := strings.TrimSpace(directChildText(body, "Signatures"))
	if location == "" {
		return nil, nil
	}
	list, err := resolveSignaturePath("", location)
	if err != nil {
		return nil, err
	}
	root, err := readSignatureRoot(pkg, list, "Signatures", maxBytes)
	if err != nil {
		return nil, err
	}
	entries := map[string]signatureEntryKind{list: signatureList}
	for _, entry := range directChildren(root, "Signature") {
		name, err := resolveSignaturePath(path.Dir(list), entry.SelectAttrValue("BaseLoc", ""))
		if err != nil {
			return nil, fmt.Errorf("签名清单 %s 的 BaseLoc 无效: %w", list, err)
		}
		if entries[name] == signatureDocument {
			continue
		}
		signature, err := readSignatureRoot(pkg, name, "Signature", maxBytes)
		if err != nil {
			return nil, err
		}
		entries[name] = signatureDocument
		walkElements(signature, func(element *etree.Element) {
			if err != nil {
				return
			}
			isPayload := localName(element) == "SignedValue"
			if parent := element.Parent(); parent != nil {
				isPayload = isPayload || localName(element) == "BaseLoc" && localName(parent) == "Seal"
			}
			if isPayload {
				var payload string
				payload, err = resolveSignaturePath(path.Dir(name), element.Text())
				if err == nil && entries[payload] == 0 {
					entries[payload] = signatureData
				}
			}
		})
		if err != nil {
			return nil, fmt.Errorf("签名文件 %s 的数据路径无效: %w", name, err)
		}
	}
	return entries, nil
}

func resolveSignaturePath(base, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("签名路径为空")
	}
	name := path.Clean(value)
	if !strings.HasPrefix(value, "/") {
		name = path.Join(base, value)
	}
	name = strings.TrimPrefix(name, "/")
	if err := core.ValidateEntryName(name); err != nil {
		return "", err
	}
	return name, nil
}

func readSignatureRoot(pkg *core.Package, name, tag string, maxBytes int64) (*etree.Element, error) {
	data, err := pkg.ReadLimit(name, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("读取签名文件 %s 失败: %w", name, err)
	}
	doc, err := ofdxml.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("解析签名文件 %s 失败: %w", name, err)
	}
	if root := doc.Root(); root != nil && localName(root) == tag {
		return root, nil
	}
	return nil, fmt.Errorf("签名文件 %s 的根元素不是 %s", name, tag)
}

func signaturePlanFor(name string, plans []bodyPlan) (bodyPlan, signatureEntryKind) {
	for _, plan := range plans {
		if kind := plan.signatureFiles[name]; kind != 0 {
			return plan, kind
		}
	}
	return bodyPlan{}, 0
}

type protectedRoot struct {
	input    string
	name     string
	original []byte
}

// validatePreservedSignatures 核对签名引用在搬运后仍指向原文件。
// §18.2.1、§18.2.2 要求受保护文件字节不变；OFD.xml 的检查需等最终主入口生成。
func validatePreservedSignatures(state *mergeState, pkg *core.Package, input string, plans []bodyPlan, rootData []byte) error {
	rootProtected := false
	for _, entry := range pkg.Entries() {
		_, kind := signaturePlanFor(entry.Path, plans)
		if kind != signatureDocument {
			continue
		}
		name := entry.Path
		root, err := readSignatureRoot(pkg, name, "Signature", state.writer.MaxEntryBytes())
		if err != nil {
			return err
		}
		newName, ok := relocatedEntry(name, plans, state.options.Orphans)
		if !ok {
			return fmt.Errorf("%s 的签名 %s 将被丢弃，preserve 模式无法保留", input, name)
		}
		walkElements(root, func(element *etree.Element) {
			if err != nil || localName(element) != "Reference" {
				return
			}
			ref := element.SelectAttrValue("FileRef", "")
			var target, newTarget string
			target, err = resolveSignaturePath(path.Dir(name), ref)
			if err != nil {
				return
			}
			newTarget, err = resolveSignaturePath(path.Dir(newName), ref)
			if err != nil {
				return
			}
			expected, kept := relocatedEntry(target, plans, state.options.Orphans)
			if !pkg.Has(target) || !kept || expected != newTarget {
				err = fmt.Errorf("引用 %q 在合并后无法指向原文件", ref)
				return
			}
			if target == spec.RootDocument && !rootProtected {
				state.protectedRoots = append(state.protectedRoots, protectedRoot{input: input, name: name, original: rootData})
				rootProtected = true
			}
		})
		if err != nil {
			return fmt.Errorf("%s 的签名 %s 无法在 preserve 模式保留: %w；请改用 rewrite 或 drop", input, name, err)
		}
	}
	return nil
}

func relocatedEntry(name string, plans []bodyPlan, orphans OrphanMode) (string, bool) {
	if name == spec.RootDocument {
		return name, true
	}
	if plan, ok := matchPlan(name, plans); ok {
		return plan.newDir + strings.TrimPrefix(name, plan.oldDir), true
	}
	return name, orphans == OrphanPreserve
}

// SignatureStatus 描述输出文档中一个签名的校验状态。
type SignatureStatus struct {
	// Type 为 Seal 或 Sign，用于区分两层 SES 与普通单层 SignedData。
	Type string
	// ID 是签名标识。
	ID string
	// DigestValid 仅表示签名引用与数据摘要匹配，不包含算法一致性与保护范围检查。
	DigestValid bool
	// Verified 表示 SM2/SES 密码学签名验证通过。
	Verified bool
	// VerificationError 是密码学验证的错误信息。
	VerificationError string
	// Validation 是完整验签结论；只有 Status 为 valid 才表示完整通过。
	Validation *parser.SignatureValidation
}

// VerifySignatures 解析 OFD 并汇总每个签名的摘要与密码学验证状态。
// input 支持文件路径、字节数据或 io.Reader。
func VerifySignatures(input any) ([]SignatureStatus, error) {
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return nil, fmt.Errorf("解析 OFD 失败: %w", err)
	}
	defer func() { _ = ofd.Close() }()

	var result []SignatureStatus
	for _, document := range ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachSignature(func(id string, signature *models.Signature) bool {
			status := SignatureStatus{ID: id, Type: signature.Type}
			if digest := document.GetDigestResult(id); digest != nil {
				status.DigestValid = digest.Valid
			}
			if verification := document.GetVerificationResult(id); verification != nil {
				status.Verified = verification.Valid
				status.Validation = verification.Validation
			}
			if verificationErr := document.GetVerificationError(id); verificationErr != nil {
				status.VerificationError = verificationErr.Error()
			}
			result = append(result, status)
			return true
		})
	}
	return result, nil
}
