package crypt

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/zc310/ofd/pkg/spec"
)

// prepareEncryption 依据 C.3 为新操作分配标识。追加仅接受一条明确的线性链。
func (s *inspector) prepareEncryption(opt EncryptOptions) (id, relative string, root []byte, err error) {
	if opt.Append == nil {
		if entry, ok := s.files[spec.RootDocument]; !ok || entry.IsDir {
			return "", "", nil, invalid(spec.RootDocument, "缺少主入口")
		}
		return "enc1", "", nil, nil
	}
	if !Present(s.pkg) {
		return "", "", nil, ErrNotEncrypted
	}
	if _, ok := s.files[EntryPoint]; !ok {
		return "", "", nil, invalid(EntryPoint, "追加要求规范的入口文件名")
	}
	// 预检会解开隐藏的签名引用；不能仅凭可见 XML 判断入口是否受保护。
	view, err := OpenView(s.ctx, s.pkg, OpenOptions{DecryptOptions: *opt.Append}, nil)
	if err != nil {
		return "", "", nil, err
	}
	defer view.Close()
	operations := view.info.Operations
	if len(operations) >= opt.Limits.MaxOperations || len(operations) >= opt.Limits.MaxLayers {
		return "", "", nil, limitError(EntryPoint, "追加后操作数量或依赖深度超过上限")
	}
	ids, parents := map[string]bool{}, map[string]bool{}
	for _, op := range operations {
		ids[op.ID] = true
		parents[op.Relative] = true
	}
	for _, op := range operations {
		if !parents[op.ID] {
			if relative != "" {
				return "", "", nil, fmt.Errorf("%w: 追加要求已有操作组成单一线性链；请先完整解密并处理已有保护，再重新加密", ErrUnsupported)
			}
			relative = op.ID
		}
	}
	if _, err := view.Export(s.ctx, func(string, []byte) error { return nil }); err != nil {
		return "", "", nil, err
	}
	// 输入控制文件与新生成控制文件共用累计预算。
	s.remaining = view.metadataRemaining
	s.recipients = view.recipients
	s.mappings = view.mappings
	s.opt.Mode = opt.Append.Mode
	for n := 1; ; n++ {
		id = fmt.Sprintf("enc%d", n)
		if !ids[id] {
			break
		}
	}
	root, err = s.readEntry(EntryPoint, opt.Limits.MaxMetadataBytes)
	return id, relative, root, err
}

// appendEncryptionInfo 仅在原根结束标签前插入新记录，保留旧记录及扩展的原始字节。
// 新节点显式声明命名空间，不依赖旧入口选择的前缀。
func appendEncryptionInfo(root []byte, info outputInfo) ([]byte, error) {
	node := struct {
		XMLName xml.Name `xml:"ofd:EncryptInfo"`
		NS      string   `xml:"xmlns:ofd,attr"`
		outputInfo
	}{NS: Namespace, outputInfo: info}
	data, err := xml.Marshal(node)
	if err != nil {
		return nil, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(root))
	depth := 0
	for {
		offset := decoder.InputOffset()
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				err = invalid(EntryPoint, "缺少根结束标签")
			}
			return nil, err
		}
		switch token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
			if depth == 0 {
				out := make([]byte, 0, len(root)+len(data))
				out = append(out, root[:offset]...)
				out = append(out, data...)
				return append(out, root[offset:]...), nil
			}
		}
	}
}
