package creator

import (
	"fmt"

	"github.com/zc310/ofd/internal/xmlcodec"
)

func pagePath(index int) string {
	return fmt.Sprintf("%s/Pages/Page_%d/Content.xml", docDir, index)
}

func pageResourcePath(pageIndex, resourceIndex int) string {
	return fmt.Sprintf("%s/Pages/Page_%d/PageRes_%d_%d.xml", docDir, pageIndex, pageIndex, resourceIndex)
}

func pageResourceImagePath(pageIndex int, imageID uint64, name string) string {
	return fmt.Sprintf("%s/Pages/Page_%d/Images/%d-%s", docDir, pageIndex, imageID, name)
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
