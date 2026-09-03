package models

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CommandType 定义路径命令类型。
// S: SubPath起点 (Start point of SubPath) - 定义子路径的起始点坐标(x,y)
// M: 移动到 (Move to) - 移动画笔到指定点，不绘制线条
// L: 直线到 (Line to) - 从当前点到指定点绘制直线
// B: 三次贝塞尔曲线 (Cubic Bezier curve) - 需要两个控制点和一个终点的曲线
// Q: 二次贝塞尔曲线 (Quadratic Bezier curve) - 需要一个控制点和一个终点的曲线
// A: 椭圆弧 (Elliptical arc) - 绘制椭圆弧线
// C: 闭合路径 (Close path) - 从当前点绘制直线回到子路径起点
type CommandType string

const (
	// Start 表示子路径的起点。
	Start CommandType = "S"
	// MoveTo 表示移动到指定点但不绘制线条。
	MoveTo CommandType = "M"
	// LineTo 表示从当前点绘制直线到指定点。
	LineTo CommandType = "L"
	// CubicBezier 表示三次贝塞尔曲线，需要两个控制点和一个终点。
	CubicBezier CommandType = "B"
	// QuadTo 表示二次贝塞尔曲线，需要一个控制点和一个终点。
	QuadTo CommandType = "Q"
	// ArcTo 表示椭圆弧线。
	ArcTo CommandType = "A"
	// Close 表示闭合路径，从当前点绘制直线回到子路径起点。
	Close CommandType = "C"
)

// ArcData 定义椭圆弧参数。
type ArcData struct {
	// RX 椭圆在 X 轴方向的半径。
	RX float64
	// RY 椭圆在 Y 轴方向的半径。
	RY float64
	// XAxisRotation 椭圆相对于 X 轴的旋转角度，单位为度。
	XAxisRotation float64
	// LargeArcFlag 是否选择大于或等于 180 度的弧段。
	LargeArcFlag bool
	// SweepFlag 是否沿正向角度方向绘制弧段。
	SweepFlag bool
	// EndPoint 椭圆弧的终点坐标。
	EndPoint StPos
}

// PathCommand 定义一条路径命令及其参数。
type PathCommand struct {
	// Type 路径命令类型。
	Type CommandType
	// Points 命令使用的坐标点；弧命令使用 Arc 保存参数。
	Points []StPos
	// Arc 椭圆弧参数，仅当 Type 为 ArcTo 时有效。
	Arc *ArcData
}

// SVGPath 定义 SVG 路径，是 PathCommand 的切片类型。
// SVGPath 实现了 XML 编解码接口，可直接解析和编码 OFD 的路径数据。
type SVGPath []PathCommand

// UnmarshalXML 从 XML 元素的文本内容中解析路径命令。
// 路径数据由命令字母和空格分隔的数值参数组成，例如 M 10 20 L 30 40。
func (p *SVGPath) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	// 读取字符数据
	var data string
	if err := d.DecodeElement(&data, &start); err != nil {
		return fmt.Errorf("XML解码失败: %w", err)
	}

	// 解析路径数据
	data = strings.TrimSpace(data)
	if data == "" {
		*p = SVGPath{}
		return nil
	}

	commands, err := p.parsePathData(data)
	if err != nil {
		return fmt.Errorf("路径数据解析失败: %w", err)
	}

	*p = commands
	return nil
}

// MarshalXML 将路径命令编码为 XML 元素文本。
func (p SVGPath) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// 构建路径字符串
	pathStr := p.String()

	// 编码为XML
	return e.EncodeElement(pathStr, start)
}

// parsePathData 将路径字符串解析为 SVGPath。
// 除显式命令外，M 和 L 命令支持连续坐标对的隐式形式。
func (p *SVGPath) parsePathData(data string) (SVGPath, error) {
	tokens := strings.Fields(data)
	if len(tokens) == 0 {
		return SVGPath{}, nil
	}

	var commands SVGPath
	var currentCmd *PathCommand
	index := 0

	for index < len(tokens) {
		token := tokens[index]

		switch token {
		case "M", "S":
			cmd, nextIdx, err := p.parseMoveCommand(tokens, index)
			if err != nil {
				return nil, err
			}
			commands = append(commands, cmd)
			currentCmd = &commands[len(commands)-1]
			index = nextIdx

		case "L":
			if currentCmd == nil {
				return nil, fmt.Errorf("L命令前缺少M命令")
			}
			cmd, nextIdx, err := p.parseLineCommand(tokens, index)
			if err != nil {
				return nil, err
			}
			commands = append(commands, cmd)
			currentCmd = &commands[len(commands)-1]
			index = nextIdx

		case "Q":
			if currentCmd == nil {
				return nil, fmt.Errorf("Q命令前缺少M命令")
			}
			cmd, nextIdx, err := p.parseQuadToCommand(tokens, index)
			if err != nil {
				return nil, err
			}
			commands = append(commands, cmd)
			currentCmd = &commands[len(commands)-1]
			index = nextIdx

		case "B":
			if currentCmd == nil {
				return nil, fmt.Errorf("B命令前缺少M命令")
			}
			cmd, nextIdx, err := p.parseBezierCommand(tokens, index)
			if err != nil {
				return nil, err
			}
			commands = append(commands, cmd)
			currentCmd = &commands[len(commands)-1]
			index = nextIdx

		case "A":
			if currentCmd == nil {
				return nil, fmt.Errorf("A命令前缺少M命令")
			}
			cmd, nextIdx, err := p.parseArcCommand(tokens, index)
			if err != nil {
				return nil, err
			}
			commands = append(commands, cmd)
			currentCmd = &commands[len(commands)-1]
			index = nextIdx

		case "C":
			cmd := PathCommand{Type: Close}
			commands = append(commands, cmd)
			currentCmd = &commands[len(commands)-1]
			index = index + 1

		default:
			if currentCmd != nil && p.isCoordinatePair(token) {
				cmd, nextIdx, err := p.parseImplicitCommand(tokens, index, *currentCmd)
				if err != nil {
					return nil, err
				}
				commands = append(commands, cmd)
				index = nextIdx
			} else {
				return nil, fmt.Errorf("无法识别的token: %s", token)
			}
		}
	}

	return commands, nil
}

// parseMoveCommand 解析 M 或 S 命令及其坐标参数。
func (p *SVGPath) parseMoveCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 1)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("M命令解析失败: %w", err)
	}
	return PathCommand{Type: MoveTo, Points: points}, nextIdx, nil
}

// parseLineCommand 解析 L 命令及其坐标参数。
func (p *SVGPath) parseLineCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 1)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("L命令解析失败: %w", err)
	}
	return PathCommand{Type: LineTo, Points: points}, nextIdx, nil
}

// parseBezierCommand 解析 B 命令及其三个坐标点参数。
func (p *SVGPath) parseBezierCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 3)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("B命令解析失败: %w", err)
	}
	return PathCommand{Type: CubicBezier, Points: points}, nextIdx, nil
}

// parseQuadToCommand 解析 Q 命令及其两个坐标点参数。
func (p *SVGPath) parseQuadToCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 2)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("Q命令解析失败: %w", err)
	}
	return PathCommand{Type: QuadTo, Points: points}, nextIdx, nil
}

// parseArcCommand 解析 A 命令及其椭圆弧参数。
// A 命令参数依次为 RX、RY、旋转角度、大弧标志、扫过标志和终点坐标。
func (p *SVGPath) parseArcCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	if startIdx+7 >= len(tokens) {
		return PathCommand{}, startIdx, fmt.Errorf("A命令需要7个参数")
	}

	// 解析椭圆半径
	rx, err := strconv.ParseFloat(tokens[startIdx+1], 64)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("A命令rx解析失败: %w", err)
	}

	ry, err := strconv.ParseFloat(tokens[startIdx+2], 64)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("A命令ry解析失败: %w", err)
	}

	// 解析x轴旋转角度
	xAxisRotation, err := strconv.ParseFloat(tokens[startIdx+3], 64)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("A命令x轴旋转角度解析失败: %w", err)
	}

	// 解析大弧标志
	largeArcFlag, err := strconv.ParseFloat(tokens[startIdx+4], 64)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("A命令大弧标志解析失败: %w", err)
	}

	// 解析扫过标志
	sweepFlag, err := strconv.ParseFloat(tokens[startIdx+5], 64)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("A命令扫过标志解析失败: %w", err)
	}

	// 解析终点坐标
	endX, err := strconv.ParseFloat(tokens[startIdx+6], 64)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("A命令终点x坐标解析失败: %w", err)
	}

	endY, err := strconv.ParseFloat(tokens[startIdx+7], 64)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("A命令终点y坐标解析失败: %w", err)
	}

	arcData := &ArcData{
		RX:            math.Abs(rx),
		RY:            math.Abs(ry),
		XAxisRotation: xAxisRotation,
		LargeArcFlag:  largeArcFlag != 0,
		SweepFlag:     sweepFlag != 0,
		EndPoint:      StPos{X: endX, Y: endY},
	}

	return PathCommand{
		Type: ArcTo,
		Arc:  arcData,
	}, startIdx + 8, nil
}

// parseImplicitCommand 根据上一条命令解析省略命令字母的坐标参数。
func (p *SVGPath) parseImplicitCommand(tokens []string, startIdx int, lastCmd PathCommand) (PathCommand, int, error) {
	switch lastCmd.Type {
	case MoveTo, LineTo:
		points, nextIdx, err := p.parsePoints(tokens, startIdx, 1)
		if err != nil {
			return PathCommand{}, startIdx, fmt.Errorf("隐式命令解析失败: %w", err)
		}
		return PathCommand{Type: LineTo, Points: points}, nextIdx, nil
	default:
		return PathCommand{}, startIdx, fmt.Errorf("不支持%v命令的隐式形式", lastCmd.Type)
	}
}

// parsePoints 从令牌列表中解析指定数量的坐标点。
func (p *SVGPath) parsePoints(tokens []string, startIdx, numPoints int) ([]StPos, int, error) {
	var points []StPos
	idx := startIdx

	for len(points) < numPoints {
		if idx+1 >= len(tokens) {
			return nil, idx, fmt.Errorf("需要%d个点，但只找到%d个坐标", numPoints, len(points)*2)
		}

		x, err := strconv.ParseFloat(tokens[idx], 64)
		if err != nil {
			return nil, idx, fmt.Errorf("x坐标解析失败(%s): %w", tokens[idx], err)
		}

		y, err := strconv.ParseFloat(tokens[idx+1], 64)
		if err != nil {
			return nil, idx, fmt.Errorf("y坐标解析失败(%s): %w", tokens[idx+1], err)
		}

		points = append(points, StPos{X: x, Y: y})
		idx += 2
	}

	return points, idx, nil
}

// isCoordinatePair 检查令牌是否为可解析的数值坐标。
func (p *SVGPath) isCoordinatePair(token string) bool {
	_, err := strconv.ParseFloat(token, 64)
	return err == nil
}

// ParsePathData 解析路径字符串并返回 SVGPath。
// 输入为空或仅包含空白字符时返回空路径。
func ParsePathData(data string) (SVGPath, error) {
	var path SVGPath
	commands, err := path.parsePathData(data)
	if err != nil {
		return nil, err
	}
	return commands, nil
}

// String 返回路径的规范化字符串表示。
// 坐标和弧参数统一保留两位小数，命令之间使用单个空格分隔。
func (p SVGPath) String() string {
	var builder strings.Builder

	for i, cmd := range p {
		if i > 0 {
			builder.WriteString(" ")
		}

		switch cmd.Type {
		case ArcTo:
			builder.WriteString(string(cmd.Type))
			builder.WriteString(fmt.Sprintf(" %.2f %.2f %.2f", cmd.Arc.RX, cmd.Arc.RY, cmd.Arc.XAxisRotation))

			// 标志位
			if cmd.Arc.LargeArcFlag {
				builder.WriteString(" 1")
			} else {
				builder.WriteString(" 0")
			}
			if cmd.Arc.SweepFlag {
				builder.WriteString(" 1")
			} else {
				builder.WriteString(" 0")
			}

			// 终点坐标
			builder.WriteString(fmt.Sprintf(" %.2f %.2f", cmd.Arc.EndPoint.X, cmd.Arc.EndPoint.Y))

		default:
			builder.WriteString(string(cmd.Type))
			for _, point := range cmd.Points {
				builder.WriteString(fmt.Sprintf(" %.2f %.2f", point.X, point.Y))
			}
		}
	}

	return builder.String()
}

// Format 按易读格式返回路径命令列表。
// 每条命令单独占一行，并包含坐标或椭圆弧参数。
func (p SVGPath) Format() string {
	var builder strings.Builder

	for i, cmd := range p {
		builder.WriteString(fmt.Sprintf("命令%d: %s ", i+1, cmd.Type))

		switch cmd.Type {
		case ArcTo:
			builder.WriteString(fmt.Sprintf("rx=%.2f ry=%.2f rotation=%.2f° ",
				cmd.Arc.RX, cmd.Arc.RY, cmd.Arc.XAxisRotation))
			builder.WriteString(fmt.Sprintf("largeArc=%v sweep=%v ",
				cmd.Arc.LargeArcFlag, cmd.Arc.SweepFlag))
			builder.WriteString(fmt.Sprintf("终点(%.2f,%.2f)",
				cmd.Arc.EndPoint.X, cmd.Arc.EndPoint.Y))

		default:
			for j, point := range cmd.Points {
				if j > 0 {
					builder.WriteString(", ")
				}
				builder.WriteString(fmt.Sprintf("(%.2f,%.2f)", point.X, point.Y))
			}
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

// CalculateBoundingBox 计算路径中坐标点的轴对齐边界框。
// 返回值依次为最小 X、最小 Y、最大 X 和最大 Y；空路径返回四个零值。
func (p SVGPath) CalculateBoundingBox() (minX, minY, maxX, maxY float64) {
	if len(p) == 0 {
		return 0, 0, 0, 0
	}

	// 找到第一个有效的点作为起始点
	var firstPoint StPos
	for _, cmd := range p {
		if cmd.Type != Close && ((len(cmd.Points) > 0) || (cmd.Type == ArcTo && cmd.Arc != nil)) {
			if cmd.Type == ArcTo {
				firstPoint = cmd.Arc.EndPoint
			} else {
				firstPoint = cmd.Points[0]
			}
			break
		}
	}

	minX, maxX = firstPoint.X, firstPoint.X
	minY, maxY = firstPoint.Y, firstPoint.Y

	// 更新边界框
	updateBounds := func(x, y float64) {
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}

	for _, cmd := range p {
		switch cmd.Type {
		case ArcTo:
			if cmd.Arc != nil {
				// 简单处理：将弧的终点加入边界框
				// 注意：这只是一个简化版本，实际弧的边界框计算更复杂
				updateBounds(cmd.Arc.EndPoint.X, cmd.Arc.EndPoint.Y)
			}
		default:
			for _, point := range cmd.Points {
				updateBounds(point.X, point.Y)
			}
		}
	}

	return
}

// CountCommands 统计路径中各类命令的数量。
func (p SVGPath) CountCommands() map[CommandType]int {
	counts := make(map[CommandType]int)
	for _, cmd := range p {
		counts[cmd.Type]++
	}
	return counts
}

// GetStartPoint 获取指定命令之前最近的有效终点，作为该命令的起始点。
// 对于首条命令或找不到有效点的情况返回错误。
func (p SVGPath) GetStartPoint(cmdIndex int) (StPos, error) {
	if cmdIndex < 0 || cmdIndex >= len(p) {
		return StPos{}, fmt.Errorf("命令索引超出范围")
	}

	// 向前查找上一个非闭合命令的终点
	for i := cmdIndex - 1; i >= 0; i-- {
		cmd := p[i]
		if cmd.Type != Close {
			if cmd.Type == ArcTo && cmd.Arc != nil {
				return cmd.Arc.EndPoint, nil
			} else if len(cmd.Points) > 0 {
				return cmd.Points[len(cmd.Points)-1], nil
			}
		}
	}

	return StPos{}, fmt.Errorf("未找到有效的起始点")
}

// AddCommand 将一条路径命令追加到路径末尾。
func (p *SVGPath) AddCommand(cmd PathCommand) {
	*p = append(*p, cmd)
}

// Clear 清空路径中的全部命令。
func (p *SVGPath) Clear() {
	*p = SVGPath{}
}

// Length 返回路径中的命令数量。
func (p SVGPath) Length() int {
	return len(p)
}

// GetCommand 获取指定索引处的路径命令。
// 当索引越界时返回错误。
func (p SVGPath) GetCommand(index int) (PathCommand, error) {
	if index < 0 || index >= len(p) {
		return PathCommand{}, fmt.Errorf("索引超出范围")
	}
	return p[index], nil
}
