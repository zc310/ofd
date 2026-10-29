package creator

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/xmlcodec"
)

func pagePath(index int) string {
	return fmt.Sprintf("%s/Pages/Page_%d/Content.xml", docDir, index)
}

func pageResourcePath(pageIndex, resourceIndex int) string {
	return fmt.Sprintf("%s/Pages/Page_%d/PageRes_%d_%d.xml", docDir, pageIndex, pageIndex, resourceIndex)
}

func pageResourceImagePath(pageIndex int, name string) string {
	return fmt.Sprintf("%s/Pages/Page_%d/Images/%s", docDir, pageIndex, name)
}

// resourceFileNames 同时预留显式名称和已分配的短名称，避免自动命名覆盖用户文件。
type resourceFileNames map[string]bool

func (names resourceFileNames) reserve(directory, name string) {
	if name = strings.TrimSpace(name); name != "" {
		names[path.Join(directory, name)] = true
	}
}

func (names resourceFileNames) reserveDocument(document Document) {
	names.reserve("Cover", document.CoverName)
	for _, media := range document.Media {
		names.reserve("Res/Media", media.Name)
	}
	for _, space := range document.ColorSpaces {
		names.reserve("Res", space.Profile)
	}
	for _, attachment := range document.Attachments {
		names.reserve("Attachments/Files", attachment.FileName)
	}
	for _, tag := range document.CustomTags {
		names.reserve("CustomTags/Data", tag.DataName)
		names.reserve("CustomTags/Schemas", tag.SchemaName)
	}
	for _, extension := range document.Extensions {
		names.reserve("Extensions/Data", extension.DataName)
	}
	for _, resource := range document.PublicRes {
		name := filepath.ToSlash(strings.TrimSpace(resource.Name))
		names.reserve("", name)
		for _, file := range resource.Files {
			names.reserve(path.Dir(name), file.Path)
		}
	}
}

func (names resourceFileNames) allocate(prefix string, id uint64, format string) string {
	for {
		name := prefix + strconv.FormatUint(id, 10)
		if format != "" {
			name += "." + format
		}
		if !names[name] {
			names[name] = true
			return name
		}
		id++
	}
}

func (s *buildState) resourceFileName(directory string, id uint64, format string) string {
	return path.Base(s.fileNames.allocate(directory+"/", id, format))
}

func templatePath(index int) string {
	return fmt.Sprintf("%s/Pages/Template_%d/Content.xml", docDir, index)
}

func annotationPath(pageID uint64) string {
	return fmt.Sprintf("%s/Annotations/Page_%d.xml", docDir, pageID)
}

func attachmentPath(name string) string {
	return docDir + "/Attachments/Files/" + name
}

func customTagDataPath(name string) string {
	return docDir + "/CustomTags/Data/" + name
}

func customTagSchemaPath(name string) string {
	return docDir + "/CustomTags/Schemas/" + name
}

func extensionDataPath(name string) string {
	return docDir + "/Extensions/Data/" + name
}

func signaturePath(name string) string {
	return docDir + "/Signatures/" + name
}

func signatureDataPath(name string) string {
	return docDir + "/Signatures/Data/" + name
}

func versionPath(name string) string {
	return docDir + "/Versions/" + name
}

func versionRootPath(name string) string {
	return docDir + "/Versions/Files/" + name
}

func validateRawXML(data []byte) error {
	// 使用实际输出的命名空间上下文，允许片段继承默认命名空间和 ofd 前缀。
	wrapped := append([]byte(`<RawXML xmlns="`+ofNamespace+`" xmlns:ofd="`+ofNamespace+`">`), data...)
	wrapped = append(wrapped, []byte("</RawXML>")...)
	_, err := parseXMLRoot(wrapped)
	return err
}

func appendRawXML(w *xmlcodec.Writer, data []byte) error {
	if err := validateRawXML(data); err != nil {
		return err
	}
	w.Raw(data)
	return nil
}
