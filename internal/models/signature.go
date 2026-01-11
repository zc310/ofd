package models

type Signature struct {
	SignedInfo  SignedInfo `xml:"SignedInfo"`
	SignedValue StLoc      `xml:"SignedValue"`
}

type SignedInfo struct {
	Provider          Provider      `xml:"Provider"`
	SignatureMethod   string        `xml:"SignatureMethod,omitempty"`
	SignatureDateTime string        `xml:"SignatureDateTime,omitempty"`
	References        References    `xml:"References"`
	StampAnnot        []*StampAnnot `xml:"StampAnnot,omitempty"`
	Seal              *Seal         `xml:"Seal,omitempty"`
}

type Provider struct {
	ProviderName string `xml:"ProviderName,attr"`
	Version      string `xml:"Version,attr,omitempty"`
	Company      string `xml:"Company,attr,omitempty"`
}

type References struct {
	CheckMethod string      `xml:"CheckMethod,attr,omitempty"` // MD5, SHA1
	Reference   []Reference `xml:"Reference"`
}

type Reference struct {
	FileRef    StLoc  `xml:"FileRef,attr"`
	CheckValue []byte `xml:"CheckValue"`
}

type StampAnnot struct {
	ID       string  `xml:"ID,attr"` // xs:ID 类型
	PageRef  StRefID `xml:"PageRef,attr"`
	Boundary StBox   `xml:"Boundary,attr"`
	Clip     StBox   `xml:"Clip,attr,omitempty"`
}

type Seal struct {
	BaseLoc StLoc `xml:"BaseLoc"`
}
