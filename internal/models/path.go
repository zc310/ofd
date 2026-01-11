package models

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CommandType 定义路径命令类型
// M: 移动到 (Move to)
// L: 直线到 (Line to)
// B: 三次贝塞尔曲线 (Cubic Bezier curve)
// Q: 二次贝塞尔曲线 (Quadratic Bezier curve)
// A: 椭圆弧 (Elliptical arc)
// C: 闭合路径 (Close path)
type CommandType string

const (
	MoveTo      CommandType = "M" // 移动到命令
	LineTo      CommandType = "L" // 直线命令
	CubicBezier CommandType = "B" // 三次贝塞尔曲线命令
	QuadTo      CommandType = "Q" // 二次贝塞尔曲线命令
	ArcTo       CommandType = "A" // 椭圆弧命令
	Close       CommandType = "C" // 闭合路径命令
)

// ArcData 定义椭圆弧参数
type ArcData struct {
	RX, RY        float64 // 椭圆半径
	XAxisRotation float64 // x轴旋转角度（度）
	LargeArcFlag  bool    // 大弧标志
	SweepFlag     bool    // 扫过标志
	EndPoint      StPos   // 终点坐标
}

// PathCommand 定义路径命令
type PathCommand struct {
	Type   CommandType
	Points []StPos
	Arc    *ArcData // 仅当Type为ArcTo时有意义
}

// SVGPath 定义SVG路径，是PathCommand的切片类型
// 可以用于直接解析和编码XML
type SVGPath []PathCommand

// UnmarshalXML 实现xml.Unmarshaler接口
// 支持解析如: <path>M 10 20 L 30 40</path>
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

// MarshalXML 实现xml.Marshaler接口
func (p SVGPath) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	// 构建路径字符串
	pathStr := p.String()

	// 编码为XML
	return e.EncodeElement(pathStr, start)
}

// parsePathData 解析路径字符串为SVGPath
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
		case "M":
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

// parseMoveCommand 解析M命令
func (p *SVGPath) parseMoveCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 1)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("M命令解析失败: %w", err)
	}
	return PathCommand{Type: MoveTo, Points: points}, nextIdx, nil
}

// parseLineCommand 解析L命令
func (p *SVGPath) parseLineCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 1)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("L命令解析失败: %w", err)
	}
	return PathCommand{Type: LineTo, Points: points}, nextIdx, nil
}

// parseBezierCommand 解析B命令
func (p *SVGPath) parseBezierCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 3)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("B命令解析失败: %w", err)
	}
	return PathCommand{Type: CubicBezier, Points: points}, nextIdx, nil
}

// parseQuadToCommand 解析Q命令
func (p *SVGPath) parseQuadToCommand(tokens []string, startIdx int) (PathCommand, int, error) {
	points, nextIdx, err := p.parsePoints(tokens, startIdx+1, 2)
	if err != nil {
		return PathCommand{}, startIdx, fmt.Errorf("Q命令解析失败: %w", err)
	}
	return PathCommand{Type: QuadTo, Points: points}, nextIdx, nil
}

// parseArcCommand 解析A命令（椭圆弧）
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

// parseImplicitCommand 解析隐式命令
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

// parsePoints 解析点坐标
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

// isCoordinatePair 检查是否为坐标对
func (p *SVGPath) isCoordinatePair(token string) bool {
	_, err := strconv.ParseFloat(token, 64)
	return err == nil
}

// ParsePathData 解析路径字符串为SVGPath (公开的工厂函数)
func ParsePathData(data string) (SVGPath, error) {
	var path SVGPath
	commands, err := path.parsePathData(data)
	if err != nil {
		return nil, err
	}
	return commands, nil
}

// String 返回路径的字符串表示
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

// Format 格式化输出命令
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

// CalculateBoundingBox 计算边界框
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

// CountCommands 统计命令数量
func (p SVGPath) CountCommands() map[CommandType]int {
	counts := make(map[CommandType]int)
	for _, cmd := range p {
		counts[cmd.Type]++
	}
	return counts
}

// GetStartPoint 获取指定命令的起始点（用于弧计算）
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

// AddCommand 添加路径命令
func (p *SVGPath) AddCommand(cmd PathCommand) {
	*p = append(*p, cmd)
}

// Clear 清空路径
func (p *SVGPath) Clear() {
	*p = SVGPath{}
}

// Length 获取路径中的命令数量
func (p SVGPath) Length() int {
	return len(p)
}

// GetCommand 获取指定索引的命令
func (p SVGPath) GetCommand(index int) (PathCommand, error) {
	if index < 0 || index >= len(p) {
		return PathCommand{}, fmt.Errorf("索引超出范围")
	}
	return p[index], nil
}
