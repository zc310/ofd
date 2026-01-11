package models

type Res struct {
	BaseLoc               StLoc                  `xml:"BaseLoc,attr"`
	ColorSpaces           *ColorSpaces           `xml:"ColorSpaces"`
	DrawParams            *DrawParams            `xml:"DrawParams"`
	Fonts                 *Fonts                 `xml:"Fonts"`
	MultiMedias           *MultiMedias           `xml:"MultiMedias"`
	CompositeGraphicUnits *CompositeGraphicUnits `xml:"CompositeGraphicUnits"`
}

type ColorSpaces struct {
	ColorSpace []ColorSpace `xml:"ColorSpace"`
}

type ColorSpace struct {
	ID               StID     `xml:"ID,attr"`
	Type             string   `xml:"Type,attr"` // GRAY, RGB, CMYK
	BitsPerComponent int      `xml:"BitsPerComponent,attr,omitempty"`
	Profile          StLoc    `xml:"Profile,attr,omitempty"`
	Palette          *Palette `xml:"Palette"`
}

type Palette struct {
	CV []StArray `xml:"CV"`
}

type DrawParams struct {
	DrawParam []*DrawParam `xml:"DrawParam"`
}

type DrawParam struct {
	ID          StID      `xml:"ID,attr"`
	Relative    StRefID   `xml:"Relative,attr,omitempty"`
	LineWidth   float64   `xml:"LineWidth,attr,omitempty"`
	Join        string    `xml:"Join,attr,omitempty"` // Miter, Round, Bevel
	Cap         string    `xml:"Cap,attr,omitempty"`  // Butt, Round, Square
	DashOffset  float64   `xml:"DashOffset,attr,omitempty"`
	DashPattern *StArrayF `xml:"DashPattern,attr,omitempty"`
	MiterLimit  float64   `xml:"MiterLimit,attr,omitempty"`
	FillColor   *CTColor  `xml:"FillColor"`
	StrokeColor *CTColor  `xml:"StrokeColor"`
}

type Fonts struct {
	Font []Font `xml:"Font"`
}

type Font struct {
	ID         StID   `xml:"ID,attr"`
	FontName   string `xml:"FontName,attr"`
	FamilyName string `xml:"FamilyName,attr,omitempty"`
	Charset    string `xml:"Charset,attr,omitempty"` // symbol, prc, big5, shift-jis, wansung, johab, unicode
	Italic     bool   `xml:"Italic,attr,omitempty"`
	Bold       bool   `xml:"Bold,attr,omitempty"`
	Serif      bool   `xml:"Serif,attr,omitempty"`
	FixedWidth bool   `xml:"FixedWidth,attr,omitempty"`
	FontFile   StLoc  `xml:"FontFile,omitempty"`
}

type MultiMedias struct {
	MultiMedia []*MultiMedia `xml:"MultiMedia"`
}

type MultiMedia struct {
	ID        StID   `xml:"ID,attr"`
	Type      string `xml:"Type,attr"` // Image, Audio, Video
	Format    string `xml:"Format,attr,omitempty"`
	MediaFile StLoc  `xml:"MediaFile"`
}

type CompositeGraphicUnits struct {
	CompositeGraphicUnit []CompositeGraphicUnit `xml:"CompositeGraphicUnit"`
}

type CompositeGraphicUnit struct {
	ID           StID        `xml:"ID,attr"`
	Width        float64     `xml:"Width,attr"`
	Height       float64     `xml:"Height,attr"`
	Thumbnail    StRefID     `xml:"Thumbnail,omitempty"`
	Substitution StRefID     `xml:"Substitution,omitempty"`
	Content      CTPageBlock `xml:"Content"`
}
