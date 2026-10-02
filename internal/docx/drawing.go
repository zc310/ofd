package docx

import "strconv"

// DrawingML 与图片相关的命名空间。
const (
	namespaceWordprocessingDrawing = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	namespaceDrawingML             = "http://schemas.openxmlformats.org/drawingml/2006/main"
	namespaceDrawingMLPicture      = "http://schemas.openxmlformats.org/drawingml/2006/picture"
)

// emuPerMillimeter 是 EMU 与毫米的换算系数：1mm = 36000 EMU。
// DrawingML 的尺寸（cx/cy）一律用 EMU，而 OOXML 其余部分用 twip。
const emuPerMillimeter = 36000.0

// EMU 把毫米换算成 EMU。
func EMU(mm float64) int { return int(mm*emuPerMillimeter + 0.5) }

// 以下结构体描述 w:drawing 里内嵌图片的 XML 形状。字段顺序遵循各自 schema 的
// sequence 定义：CT_Inline 是 extent、effectExtent、docPr、cNvGraphicFramePr、
// graphic；pic:pic 是 nvPicPr、blipFill、spPr。顺序错了 Word 会判定文档损坏，
// 与 w:pPr 同理，drawingOrderTest 里用同样的方式断言。
//
// 属性名大多不带前缀（distT、cx、id、noChangeAspect），只有 r:embed 带 r 前缀，
// 因为它引用的是 OPC 关系而非 DrawingML 自身的属性。

// Drawing 是 w:drawing 的内容模型。
type Drawing struct {
	Inline *Inline `xml:"wp:inline"`
}

// Inline 是 wp:inline。距离文档正文的距离用 EMU，图片不做浮雕等效果时全为 0。
type Inline struct {
	DistanceTop    int             `xml:"distT,attr"`
	DistanceBottom int             `xml:"distB,attr"`
	DistanceLeft   int             `xml:"distL,attr"`
	DistanceRight  int             `xml:"distR,attr"`
	Extent         *Extent         `xml:"wp:extent"`
	EffectExtent   *EffectExtent   `xml:"wp:effectExtent"`
	DocProperties  *DocProperties  `xml:"wp:docPr"`
	FramePr        *GraphicFramePr `xml:"wp:cNvGraphicFramePr"`
	Graphic        *Graphic        `xml:"a:graphic"`
}

// Extent 是 wp:extent，图片的显示尺寸（EMU）。c 与 cy 必须与 a:ext 一致。
type Extent struct {
	Width  int `xml:"cx,attr"`
	Height int `xml:"cy,attr"`
}

// EffectExtent 是 wp:effectExtent，表示图片超出 extent 的部分。四项全为 0。
type EffectExtent struct {
	Left   int `xml:"l,attr"`
	Top    int `xml:"t,attr"`
	Right  int `xml:"r,attr"`
	Bottom int `xml:"b,attr"`
}

// DocProperties 是 wp:docPr。ID 在全文唯一，Name 供辅助功能与替换图片时使用。
type DocProperties struct {
	ID   int    `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

// GraphicFramePr 是 wp:cNvGraphicFramePr。锁定宽高比使 Word 缩放时不变形。
type GraphicFramePr struct {
	Locks *GraphicFrameLocks `xml:"a:graphicFrameLocks"`
}

// GraphicFrameLocks 是 a:graphicFrameLocks。NoChangeAspect 是 ST_OnOff，
// 这里写 "1" 而不是 "true"：两者在 Transitional 词法下都合法，但 Word 自己
// 写的是 "1"，照抄可以少一类差异。
type GraphicFrameLocks struct {
	NoChangeAspect string `xml:"noChangeAspect,attr"`
}

// Graphic 是 a:graphic，graphicData 的 uri 声明这里放的是图片而不是图表或形状。
type Graphic struct {
	Data *GraphicData `xml:"a:graphicData"`
}

// GraphicData 是 a:graphicData。
type GraphicData struct {
	URI     string   `xml:"uri,attr"`
	Picture *Picture `xml:"pic:pic"`
}

// Picture 是 pic:pic。
type Picture struct {
	NonVisual  *PictureNonVisual `xml:"pic:nvPicPr"`
	Fill       *PictureBlipFill  `xml:"pic:blipFill"`
	Properties *ShapeProperties  `xml:"pic:spPr"`
}

// PictureNonVisual 是 pic:nvPicPr。
type PictureNonVisual struct {
	Properties   *NonVisualProperties `xml:"pic:cNvPr"`
	PictureProps *PictureProperties   `xml:"pic:cNvPicPr"`
}

// NonVisualProperties 是 pic:cNvPr。
type NonVisualProperties struct {
	ID   int    `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

// PictureProperties 是 pic:cNvPicPr。
type PictureProperties struct {
}

// PictureBlipFill 是 pic:blipFill。Embed 是指向 word/media 条目的关系 ID。
type PictureBlipFill struct {
	Blip    *Blip    `xml:"a:blip"`
	Stretch *Stretch `xml:"a:stretch"`
}

// Blip 是 a:blip。Embed 带 r 前缀，指向 OPC 关系。
type Blip struct {
	Embed string `xml:"r:embed,attr"`
}

// Stretch 是 a:stretch，FillRect 让图片填满 extent。
type Stretch struct {
	Rect *FillRect `xml:"a:fillRect"`
}

// FillRect 是 a:fillRect，无属性即铺满。
type FillRect struct {
}

// ShapeProperties 是 pic:spPr。
type ShapeProperties struct {
	Transform *Transform   `xml:"a:xfrm"`
	Geometry  *PresetShape `xml:"a:prstGeom"`
}

// Transform 是 a:xfrm。图片自身没有偏移，宽高必须与 wp:extent 一致。
type Transform struct {
	Offset *Offset `xml:"a:off"`
	Extent *Extent `xml:"a:ext"`
}

// Offset 是 a:off。
type Offset struct {
	X int `xml:"x,attr"`
	Y int `xml:"y,attr"`
}

// PresetShape 是 a:prstGeom。图片一律是矩形。
type PresetShape struct {
	Preset string           `xml:"prst,attr"`
	List   *AdjustValueList `xml:"a:avLst"`
}

// AdjustValueList 是 a:avLst，矩形无调节点，留空即可。
type AdjustValueList struct {
}

// NewInlineImage 组装一张内嵌图片的 w:drawing。widthMM 与 heightMM 是显示尺寸
// （毫米），relationID 是图片部件的 OPC 关系 ID。
func NewInlineImage(relationID string, widthMM, heightMM float64, id int, name string) *Drawing {
	width, height := EMU(widthMM), EMU(heightMM)
	extent := &Extent{Width: width, Height: height}
	return &Drawing{
		Inline: &Inline{
			EffectExtent:  &EffectExtent{},
			Extent:        extent,
			DocProperties: &DocProperties{ID: id, Name: name},
			FramePr:       &GraphicFramePr{Locks: &GraphicFrameLocks{NoChangeAspect: "1"}},
			Graphic: &Graphic{Data: &GraphicData{
				URI: namespaceDrawingMLPicture,
				Picture: &Picture{
					NonVisual: &PictureNonVisual{
						Properties:   &NonVisualProperties{ID: id, Name: name},
						PictureProps: &PictureProperties{},
					},
					Fill: &PictureBlipFill{
						Blip:    &Blip{Embed: relationID},
						Stretch: &Stretch{Rect: &FillRect{}},
					},
					Properties: &ShapeProperties{
						Transform: &Transform{
							Offset: &Offset{},
							Extent: &Extent{Width: width, Height: height},
						},
						Geometry: &PresetShape{Preset: "rect", List: &AdjustValueList{}},
					},
				},
			}},
		},
	}
}

// drawingName 生成图片的辅助名称，Word 的替换图片对话框会显示它。
func drawingName(index int) string {
	return "图片 " + strconv.Itoa(index)
}
