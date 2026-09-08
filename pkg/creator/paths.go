package creator

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
)

func newXMLDocument() *etree.Document {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	return doc
}

func documentBytes(doc *etree.Document) ([]byte, error) {
	data, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("生成 OFD XML 失败: %w", err)
	}
	return data, nil
}

func createOptionalText(parent *etree.Element, name, value string) {
	if strings.TrimSpace(value) != "" {
		parent.CreateElement(name).SetText(value)
	}
}

func createOptionalDate(parent *etree.Element, name string, value time.Time) {
	if !value.IsZero() {
		parent.CreateElement(name).SetText(value.Format("2006-01-02"))
	}
}

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

func optionalBoolElement(parent *etree.Element, name string, value *bool) {
	if value != nil {
		parent.CreateElement(name).SetText(strconv.FormatBool(*value))
	}
}

func optionalNumberAttr(element *etree.Element, name string, value *float64) {
	if value != nil {
		element.CreateAttr(name, number(*value))
	}
}

func rawXMLChildren(data []byte) (*etree.Element, error) {
	if strings.HasPrefix(strings.TrimSpace(string(data)), "<?xml") {
		return nil, fmt.Errorf("XML 片段不能包含 XML 声明")
	}
	document := etree.NewDocument()
	wrapped := append([]byte("<RawXML>"), data...)
	wrapped = append(wrapped, []byte("</RawXML>")...)
	if err := document.ReadFromBytes(wrapped); err != nil {
		return nil, err
	}
	return document.Root(), nil
}

func appendRawXML(parent *etree.Element, data []byte) error {
	root, err := rawXMLChildren(data)
	if err != nil {
		return err
	}
	for _, child := range root.Child {
		switch value := child.(type) {
		case *etree.Element:
			parent.AddChild(value.Copy())
		case *etree.CharData:
			if value.IsCData() {
				parent.AddChild(etree.NewCData(value.Data))
			} else {
				parent.CreateText(value.Data)
			}
		case *etree.Comment:
			parent.CreateComment(value.Data)
		case *etree.Directive:
			parent.CreateDirective(value.Data)
		case *etree.ProcInst:
			parent.CreateProcInst(value.Target, value.Inst)
		}
	}
	return nil
}
