package sign

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/core"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/physicalsig"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/pkg/spec"
)

func signatureScope(physical bool) string {
	if physical {
		return "physical"
	}
	return "logical"
}

func preparePhysical(ctx context.Context, pkg *core.Package, entries []entry, index map[string][]byte, document *etree.Document, options Options) (*appendPlan, error) {
	var checked struct{}
	if err := physicalsig.ValidateXML(ctx, index[spec.RootDocument], "OFD", document.Root().SelectAttrValue("DocType", ""), &checked); err != nil {
		return nil, err
	}
	plan := &appendPlan{id: strings.TrimSpace(options.ID), catalogs: map[string]bool{}, remaining: 512 << 20}
	if plan.id == "" {
		plan.id = "sign-1"
	}
	if !securexml.ValidID(plan.id) {
		return nil, fmt.Errorf("签名 ID 必须为合法 xs:ID")
	}
	for _, item := range entries {
		if isSignatureEntry(item.name) {
			return nil, fmt.Errorf("%w: 物理签署暂不接受已有签名文件", protocol.ErrProtectedContent)
		}
		if item.name != spec.RootDocument {
			plan.remaining -= int64(len(item.data))
		}
	}
	for _, body := range directChildren(document.Root(), "DocBody") {
		if directChild(body, "Signatures") != nil {
			return nil, fmt.Errorf("%w: 物理签署要求没有已有签名入口", protocol.ErrProtectedContent)
		}
		docDir, err := docDirOf(body)
		if err != nil {
			return nil, err
		}
		listName := path.Join(docDir, "Signatures.xml")
		if plan.catalogs[listName] {
			return nil, fmt.Errorf("文档体签名列表重复")
		}
		dir := path.Join(docDir, "Signatures", plan.id)
		for _, name := range []string{listName, dir} {
			if err := unusedAppendPath(pkg, name); err != nil {
				return nil, err
			}
		}
		list := etree.NewDocument()
		root := list.CreateElement("Signatures")
		root.CreateAttr("xmlns", securexml.Namespace)
		node := root.CreateElement("Signature")
		node.CreateAttr("ID", plan.id)
		node.CreateAttr("Type", spec.SigTypeSign)
		node.CreateAttr("BaseLoc", "/"+path.Join(dir, "Signature_"+plan.id+".xml"))
		data, err := list.WriteToBytes()
		if err != nil {
			return nil, err
		}
		if err := physicalsig.ValidateXML(ctx, data, "Signatures", "", &checked); err != nil {
			return nil, err
		}
		plan.bodies = append(plan.bodies, appendBody{docDir: docDir, listName: listName, signatureDir: dir, listData: data})
		plan.catalogs[listName], index[listName] = true, data
		setBodySignatures(body, listName)
	}
	data, err := document.WriteToBytes()
	if err != nil {
		return nil, err
	}
	plan.root, index[spec.RootDocument] = data, data
	plan.remaining -= int64(len(data))
	if len(entries)+3*len(plan.bodies) > 10000 || plan.remaining < 0 {
		return nil, protocol.ErrLimitExceeded
	}
	return plan, nil
}

func physicalReferences(index map[string][]byte) []string {
	refs := make([]string, 0, len(index))
	for name := range index {
		refs = append(refs, "/"+name)
	}
	sort.Strings(refs)
	return refs
}

// VerifyPhysical 仅从明文 OFD.xml 的列表发现外层签名，直接验证加密物理包。
// 不需要解密凭据；不代替解密后的文档语义和防夹带闭包检查。
func VerifyPhysical(ctx context.Context, input any, options VerificationOptions) ([]Verification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pkg, err := openAppendPackage(input)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	return verifyPhysicalPackage(ctx, pkg, options)
}

func verifyPhysicalPackage(ctx context.Context, pkg *core.Package, options VerificationOptions) ([]Verification, error) {
	if options.CurrentTime.IsZero() {
		options.CurrentTime = time.Now()
	}
	if err := physicalsig.CheckContainer(ctx, pkg, protocol.Limits{}); err != nil {
		return nil, err
	}
	graph, err := physicalsig.Discover(ctx, pkg, protocol.Limits{})
	if err != nil {
		return nil, err
	}
	if graph == nil {
		return nil, fmt.Errorf("没有可从明文入口定位的物理包签名")
	}
	checked, err := physicalsig.Verify(ctx, pkg, graph, &options, protocol.Limits{})
	if err != nil {
		return nil, err
	}
	results := make([]Verification, 0, len(checked))
	for _, item := range checked {
		results = append(results, Verification{DocumentIndex: item.Record.DocumentIndex, ID: item.Record.ID, Scope: "physical", DigestValid: item.Digest.Valid, Result: item.Result})
	}
	return results, nil
}
