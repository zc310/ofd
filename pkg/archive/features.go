package archive

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/spec"
)

// encryptionsFile 是 OFD 表达加密的文件名，见 GB/T 33190 的包内结构。
const encryptionsFile = "Encryptions.xml"

func scanFeatures(filename string, maxXMLBytes int64) (FeatureSummary, error) {
	packageReader, err := openPackage(filename)
	if err != nil {
		return FeatureSummary{}, err
	}
	defer packageReader.Close()
	var summary FeatureSummary
	var scanErr error
	err = packageReader.WalkEntries(func(entry core.Entry) bool {
		// 加密在 OFD 里由包内 Encryptions.xml 表达，文档 XML 中没有对应元素。
		// 早先的实现匹配 encrypt/encryption/encrypteddoc 三种元素名，而三者
		// 都不在 OFD 模式中，该计数对合规文件恒为 0。
		if !entry.IsDir && strings.EqualFold(filepath.Base(entry.Path), encryptionsFile) {
			summary.Encryption = 1
		}
		if entry.IsDir || filepath.Ext(entry.Path) != ".xml" {
			return true
		}
		reader, openErr := packageReader.OpenEntry(entry)
		if openErr != nil {
			scanErr = fmt.Errorf("打开 XML 条目 %s 失败：%w", entry.Path, openErr)
			return false
		}
		scanErr = scanXML(reader, &summary, maxXMLBytes)
		closeErr := reader.Close()
		if scanErr == nil && closeErr != nil {
			scanErr = fmt.Errorf("关闭 XML 条目 %s 失败：%w", entry.Path, closeErr)
		}
		if scanErr != nil {
			return false
		}
		return true
	})
	if err != nil {
		return summary, err
	}
	if scanErr != nil {
		return summary, scanErr
	}
	return summary, nil
}

// scanXML 统计包内 XML 的 OFD 要素。统计口径与 pkg/validator 的 profile 规则
// 对齐，只认 OFD 命名空间下的真实元素，避免两处结论相反。
//
// 早前的实现用元素名直接匹配且不限定命名空间，因此有两类问题：一是 audio、
// video、media、encrypt 等元素名在 OFD 模式中根本不存在（多媒体由 MultiMedia
// 表达、加密由 Encryptions.xml 表达），这些计数对合规文件恒为 0；二是包内若有
// 其它命名空间的同名元素（内嵌 SVG、自定义 XML 等）会被误计。
func scanXML(reader io.Reader, summary *FeatureSummary, maxXMLBytes int64) error {
	var limited *io.LimitedReader
	if maxXMLBytes > 0 && maxXMLBytes < int64(^uint64(0)>>1) {
		limited = &io.LimitedReader{R: reader, N: maxXMLBytes + 1}
		reader = limited
	}
	decoder := xml.NewDecoder(reader)
	// actionDepth 记录当前嵌套在几个 Actions 容器内：动作只统计 Actions 下的
	// Action，不把任意同名元素算进去。
	actionDepth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if limited != nil && limited.N == 0 {
				return fmt.Errorf("XML 内容超过大小限制：%d", maxXMLBytes)
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Space != spec.Namespace {
				// 跳过整个子树，其 EndElement 不会上抛，actionDepth 不会失衡。
				if err := decoder.Skip(); err != nil {
					return err
				}
				continue
			}
			switch element.Name.Local {
			case "Actions":
				actionDepth++
			case "Action":
				if actionDepth > 0 {
					summary.Actions++
				}
			case "MultiMedia":
				// 多媒体在 OFD 中是带 Type 属性的 MultiMedia 元素，没有独立的
				// audio/video/media 元素。
				summary.Media++
				switch strings.TrimSpace(xmlAttrValue(element.Attr, "Type")) {
				case "Audio":
					summary.Audio++
				case "Video":
					summary.Video++
				}
			case "Extension":
				summary.Extensions++
			}
		case xml.EndElement:
			if element.Name.Local == "Actions" && actionDepth > 0 {
				actionDepth--
			}
		}
	}
}

// xmlAttrValue 返回指定属性值。
func xmlAttrValue(attributes []xml.Attr, name string) string {
	for _, attribute := range attributes {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func extractAttachments(input, output string, attachments []Attachment, maxSize int64) ([]Attachment, []Issue, error) {
	packageReader, err := openPackage(input)
	if err != nil {
		return attachments, nil, err
	}
	defer packageReader.Close()
	if err := os.MkdirAll(output, 0o700); err != nil {
		return attachments, nil, err
	}
	used := map[string]bool{}
	var issues []Issue
	for index := range attachments {
		item := &attachments[index]
		if !item.Exists || item.SourcePath == "" {
			issues = append(issues, Issue{Severity: "warning", Code: "attachment.missing", Message: fmt.Sprintf("附件不存在：%s", item.SourcePath)})
			continue
		}
		entry, ok := packageReader.Lookup(item.SourcePath)
		if !ok || entry.IsDir {
			issues = append(issues, Issue{Severity: "warning", Code: "attachment.missing", Message: fmt.Sprintf("附件不存在：%s", item.SourcePath)})
			continue
		}
		if maxSize > 0 && entry.UncompressedSize > uint64(maxSize) {
			issues = append(issues, Issue{Severity: "error", Code: "attachment.too_large", Message: fmt.Sprintf("附件超过大小限制：%s", item.SourcePath)})
			continue
		}
		reader, openErr := packageReader.OpenEntry(entry)
		if openErr != nil {
			issues = append(issues, Issue{Severity: "error", Code: "attachment.open", Message: openErr.Error()})
			continue
		}
		name := uniquePath(item.Name, used)
		path := filepath.Join(output, name)
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if createErr != nil {
			_ = reader.Close()
			return attachments, issues, createErr
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		readerCloseErr := reader.Close()
		if copyErr != nil || closeErr != nil || readerCloseErr != nil {
			_ = os.Remove(path)
			issues = append(issues, Issue{Severity: "error", Code: "attachment.copy", Message: fmt.Sprintf("复制附件失败：%s", item.SourcePath)})
			continue
		}
		item.ArchivePath = filepath.ToSlash(filepath.Join("attachments", name))
		item.SHA256, err = hashFile(path)
		if err != nil {
			_ = os.Remove(path)
			issues = append(issues, Issue{Severity: "error", Code: "attachment.hash", Message: fmt.Sprintf("计算附件固定性失败：%s", item.SourcePath)})
			continue
		}
		item.Copied = true
	}
	return attachments, issues, nil
}
