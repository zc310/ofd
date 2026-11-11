package spec

import (
	"encoding/xml"
	"errors"
	"fmt"
)

var errArcParameters = errors.New("圆弧 A 命令必须仅使用 Arc 参数")

// MarshalXML 将路径编码为表36的紧缩命令文本，不使用 String 的两位小数诊断格式。
// 命令参数数量必须匹配，数值必须有限；完整文档的图形规则仍由校验器检查。
func (p PathData) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	var data []byte
	for i, command := range p {
		values, err := command.xmlParameters()
		if err != nil {
			return fmt.Errorf("第 %d 条路径命令: %w", i+1, err)
		}
		parameters, err := marshalFiniteNumbers(values)
		if err != nil {
			return fmt.Errorf("第 %d 条路径命令: %w", i+1, err)
		}
		if i > 0 {
			data = append(data, ' ')
		}
		data = append(data, command.Type...)
		if len(parameters) > 0 {
			data = append(data, ' ')
			data = append(data, parameters...)
		}
	}
	return e.EncodeElement(string(data), start)
}

//nolint:err113 // 错误包含具体命令及参数数量，便于定位直接构造的不完整命令。
func (c PathCommand) xmlParameters() ([]float64, error) {
	points := 0
	switch c.Type {
	case Start, MoveTo, LineTo:
		points = 1
	case QuadTo:
		points = 2
	case CubicBezier:
		points = 3
	case ArcTo:
		if c.Arc == nil || len(c.Points) != 0 {
			return nil, errArcParameters
		}
		a := c.Arc
		large, sweep := 0.0, 0.0
		if a.LargeArcFlag {
			large = 1
		}
		if a.SweepFlag {
			sweep = 1
		}
		return []float64{a.RX, a.RY, a.XAxisRotation, large, sweep, a.EndPoint.X, a.EndPoint.Y}, nil
	case Close:
	default:
		return nil, fmt.Errorf("未知 OFD 路径命令 %q", c.Type)
	}
	if len(c.Points) != points || c.Arc != nil {
		return nil, fmt.Errorf("%s 命令需要 %d 个点且不能指定 Arc", c.Type, points)
	}
	values := make([]float64, 0, points*2)
	for _, point := range c.Points {
		values = append(values, point.X, point.Y)
	}
	return values, nil
}
