package spec

import "encoding/xml"

// DestType 是 GB/T 33190—2016 §14.2 表54的跳转目标描述方式。
type DestType string

// Dest 保存目标页面的标识及可选坐标，不执行页码解析或坐标换算。
type Dest struct {
	Type   DestType `xml:"Type,attr"`
	PageID StRefID  `xml:"PageID,attr"` //nolint:tagliatelle // 标准属性名为 PageID。
	Left   *float64 `xml:"Left,attr,omitempty"`
	Top    *float64 `xml:"Top,attr,omitempty"`
	Right  *float64 `xml:"Right,attr,omitempty"`
	Bottom *float64 `xml:"Bottom,attr,omitempty"`
	Zoom   *float64 `xml:"Zoom,attr,omitempty"`
}

// ActionEvent 是 §14.1 表52的动作触发事件。
type ActionEvent string

// Action 是 §14.1 表51的动作声明，不负责触发或执行动作。
// Goto、URI、GotoA、Sound、Movie 按标准应恰有一项，模型解码本身不代替校验。
type Action struct {
	Event  ActionEvent  `xml:"Event,attr"`
	Region *Region      `xml:"Region,omitempty"`
	Goto   *ActionGoto  `xml:"Goto,omitempty"`
	URI    *ActionURI   `xml:"URI,omitempty"` //nolint:tagliatelle // 标准元素名为 URI。
	GotoA  *ActionGotoA `xml:"GotoA,omitempty"`
	Sound  *ActionSound `xml:"Sound,omitempty"`
	Movie  *ActionMovie `xml:"Movie,omitempty"`
}

// ActionGoto 是 §14.2 表53的文档内跳转，目标区域和书签引用应恰有一项。
type ActionGoto struct {
	Dest     *Dest        `xml:"Dest,omitempty"`
	Bookmark *BookmarkRef `xml:"Bookmark,omitempty"`
}

// BookmarkRef 按名称引用文档书签，而不是再次声明书签目标。
type BookmarkRef struct {
	Name string `xml:"Name,attr"`
}

// ActionURI 是 §14.4 表56及附录A的 URI 动作声明，不打开外部链接。
type ActionURI struct {
	URI    string  `xml:"URI,attr"` //nolint:tagliatelle // 标准属性名为 URI。
	Base   *string `xml:"Base,attr,omitempty"`
	Target *string `xml:"Target,attr,omitempty"`
}

// ActionGotoA 是 §14.3 表55的附件跳转声明。
type ActionGotoA struct {
	AttachID string `xml:"AttachID,attr"` //nolint:tagliatelle // 标准属性名为 AttachID。
	// NewWindow 保留属性省略状态；附录A的缺省值为 true，由动作执行方解释。
	NewWindow *bool `xml:"NewWindow,attr,omitempty"`
}

// ActionSound 是 §14.5 表57的音频动作声明，省略状态由指针保留。
type ActionSound struct {
	ResourceID  StRefID `xml:"ResourceID,attr"` //nolint:tagliatelle // 标准属性名为 ResourceID。
	Volume      *int    `xml:"Volume,attr,omitempty"`
	Repeat      *bool   `xml:"Repeat,attr,omitempty"`
	Synchronous *bool   `xml:"Synchronous,attr,omitempty"`
}

// MovieOperator 是 §14.6 表59的视频播放操作。
type MovieOperator string

// ActionMovie 是 §14.6 表58的视频动作声明，Operator 为空表示省略，不提前补 Play。
type ActionMovie struct {
	ResourceID StRefID       `xml:"ResourceID,attr"` //nolint:tagliatelle // 标准属性名为 ResourceID。
	Operator   MovieOperator `xml:"Operator,attr,omitempty"`
}

// Region 是 §9.3 及附录A的复杂区域，可用于动作启动范围。
type Region struct {
	Areas []RegionArea `xml:"Area"`
}

// RegionArea 保存一个区域的起点及按文档顺序排列的线段。
type RegionArea struct {
	Start StPos        `xml:"Start,attr"`
	Paths []RegionPath `xml:",any"`
}

// RegionPath 保存 Move、Line、QuadraticBezier、CubicBezier、Arc 或 Close 元素。
// XMLName 决定具体线段名称，既有解码行为也会保留未知元素名，不在这里执行校验。
type RegionPath struct {
	XMLName        xml.Name          `xml:""` //nolint:tagliatelle // 空标签保留 XMLName 动态选择的线段元素名。
	Point1         *StPos            `xml:"Point1,attr,omitempty"`
	Point2         *StPos            `xml:"Point2,attr,omitempty"`
	Point3         *StPos            `xml:"Point3,attr,omitempty"`
	SweepDirection *bool             `xml:"SweepDirection,attr,omitempty"`
	LargeArc       *bool             `xml:"LargeArc,attr,omitempty"`
	RotationAngle  *float64          `xml:"RotationAngle,attr,omitempty"`
	EllipseSize    *StArray[float64] `xml:"EllipseSize,attr,omitempty"`
	EndPoint       *StPos            `xml:"EndPoint,attr,omitempty"`
}
