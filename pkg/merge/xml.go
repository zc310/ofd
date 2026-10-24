package merge

import (
	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/xmlcodec"
)

func marshalOFDXML(doc *etree.Document) ([]byte, error) {
	if root := doc.Root(); root != nil {
		prefixOFDXML(root)
	}
	return xmlcodec.MarshalDocument(doc)
}
