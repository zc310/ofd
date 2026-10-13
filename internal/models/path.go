package models

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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

// pathScanner 按需读取 token，不分配偏移表；ASCII 和 Unicode 空白
// 使用相同的命令分词规则。
type pathScanner struct {
	data  string
	index int
}

func (s *pathScanner) next() string {
	data := s.data
	i := s.index
	for i < len(data) {
		for i < len(data) {
			if data[i] < utf8.RuneSelf {
				if !isPathSeparator(data[i]) {
					break
				}
				i++
			} else {
				r, width := utf8.DecodeRuneInString(data[i:])
				if !unicode.IsSpace(r) {
					break
				}
				i += width
			}
		}
		if i >= len(data) {
			break
		}
		start := i
		if isPathCommand(data[i]) {
			i++
		} else {
			for i < len(data) {
				if data[i] < utf8.RuneSelf {
					if isPathSeparator(data[i]) || isPathCommand(data[i]) {
						break
					}
					i++
				} else {
					r, width := utf8.DecodeRuneInString(data[i:])
					if unicode.IsSpace(r) {
						break
					}
					i += width
				}
			}
		}
		s.index = i
		return data[start:i]
	}
	s.index = i
	return ""
}

func isPathSeparator(value byte) bool {
	return value == ',' || value == ' ' || value == '\t' || value == '\v' || value == '\f' || value == '\r' || value == '\n'
}

func isPathCommand(value byte) bool {
	switch value {
	case 'M', 'S', 'L', 'Q', 'B', 'A', 'C':
		return true
	default:
		return false
	}
}

// parsePathData 将路径字符串解析为 SVGPath。
// 除显式命令外，M 和 L 命令支持连续坐标对的隐式形式。
func (p *SVGPath) parsePathData(data string) (SVGPath, error) {
	scanner := pathScanner{data: data}
	tokenCount, commandCount, explicitPoints, arcCount := 0, 0, 0, 0
	for token := scanner.next(); token != ""; token = scanner.next() {
		tokenCount++
		switch token {
		case "M", "S", "L":
			commandCount++
			explicitPoints++
		case "Q":
			commandCount++
			explicitPoints += 2
		case "B":
			commandCount++
			explicitPoints += 3
		case "A":
			commandCount++
			arcCount++
		case "C":
			commandCount++
		}
	}
	if tokenCount == 0 {
		return SVGPath{}, nil
	}

	scanner.index = 0
	// 扣除命令和弧参数后，每两个 token 对应一个点；额外的点对应隐式 L。
	// 非法输入只用于容量估计，具体错误由下面的解析报告。
	pointCount := max(0, (tokenCount-commandCount-7*arcCount)/2)
	commandCount += max(0, pointCount-explicitPoints)
	commands := make(SVGPath, 0, commandCount)
	// 各命令共享坐标存储，但切片容量限制在自身范围内。
	points := make([]StPos, 0, pointCount)
	var currentType CommandType
	for token := scanner.next(); token != ""; token = scanner.next() {

		switch token {
		case "M", "S", "L", "Q", "B":
			commandType := CommandType(token)
			numPoints := 1
			switch commandType {
			case Start:
				commandType = MoveTo // 保留现有的 S 归一化行为。
			case QuadTo:
				numPoints = 2
			case CubicBezier:
				numPoints = 3
			}
			if commandType != MoveTo && currentType == "" {
				return nil, fmt.Errorf("%s命令前缺少M命令", token)
			}
			start := len(points)
			parsed, err := parsePathPoints(&scanner, "", numPoints, points)
			if err != nil {
				return nil, fmt.Errorf("%s命令解析失败: %w", token, err)
			}
			points = parsed
			commands = append(commands, PathCommand{Type: commandType, Points: points[start:len(points):len(points)]})
			currentType = commandType

		case "A":
			if currentType == "" {
				return nil, fmt.Errorf("A命令前缺少M命令")
			}
			cmd, err := parseArcCommand(&scanner)
			if err != nil {
				return nil, err
			}
			commands = append(commands, cmd)
			currentType = ArcTo

		case "C":
			cmd := PathCommand{Type: Close}
			commands = append(commands, cmd)
			currentType = Close

		default:
			if currentType == MoveTo || currentType == LineTo {
				start := len(points)
				parsed, err := parsePathPoints(&scanner, token, 1, points)
				if err != nil {
					return nil, fmt.Errorf("隐式命令解析失败: %w", err)
				}
				points = parsed
				commands = append(commands, PathCommand{Type: LineTo, Points: points[start:len(points):len(points)]})
				currentType = LineTo
			} else {
				return nil, fmt.Errorf("无法识别的token: %s", token)
			}
		}
	}

	return commands, nil
}

// parseArcCommand 解析 A 命令及其椭圆弧参数。
// A 命令参数依次为 RX、RY、旋转角度、大弧标志、扫过标志和终点坐标。
func parseArcCommand(scanner *pathScanner) (PathCommand, error) {
	names := [...]string{"A命令rx", "A命令ry", "A命令x轴旋转角度", "A命令大弧标志", "A命令扫过标志", "A命令终点x坐标", "A命令终点y坐标"}
	var values [7]float64
	for i, name := range names {
		token := scanner.next()
		if token == "" {
			return PathCommand{}, fmt.Errorf("A命令需要7个参数")
		}
		value, err := parseFiniteFloat(token, name)
		if err != nil {
			return PathCommand{}, err
		}
		values[i] = value
	}

	arcData := &ArcData{
		RX:            math.Abs(values[0]),
		RY:            math.Abs(values[1]),
		XAxisRotation: values[2],
		LargeArcFlag:  values[3] != 0,
		SweepFlag:     values[4] != 0,
		EndPoint:      StPos{X: values[5], Y: values[6]},
	}

	return PathCommand{
		Type: ArcTo,
		Arc:  arcData,
	}, nil
}

// parsePathPoints 将指定数量的坐标点追加到整条路径的坐标缓冲区。
func parsePathPoints(scanner *pathScanner, firstToken string, numPoints int, points []StPos) ([]StPos, error) {
	for n := 0; n < numPoints; n++ {
		xToken := firstToken
		firstToken = ""
		if xToken == "" {
			xToken = scanner.next()
		}
		yToken := scanner.next()
		if xToken == "" || yToken == "" {
			return nil, fmt.Errorf("需要%d个点，但只找到%d个坐标", numPoints, n*2)
		}
		x, err := parseFiniteFloat(xToken, "x坐标")
		if err != nil {
			return nil, err
		}

		y, err := parseFiniteFloat(yToken, "y坐标")
		if err != nil {
			return nil, err
		}

		points = append(points, StPos{X: x, Y: y})
	}

	return points, nil
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
	// 提前计算精确长度，避免长路径输出反复扩容。固定小数格式最多需要
	// 313 字节（负号、309 位整数、小数点和两位小数）。
	var scratch [320]byte
	size := 0
	measureFloat := func(value float64) {
		size += 1 + len(strconv.AppendFloat(scratch[:0], value, 'f', 2, 64))
	}
	for i, cmd := range p {
		if i > 0 {
			size++
		}
		size += len(cmd.Type)
		if cmd.Type == ArcTo {
			measureFloat(cmd.Arc.RX)
			measureFloat(cmd.Arc.RY)
			measureFloat(cmd.Arc.XAxisRotation)
			size += 4 // 两个标志及其前导空格。
			measureFloat(cmd.Arc.EndPoint.X)
			measureFloat(cmd.Arc.EndPoint.Y)
		} else {
			for _, point := range cmd.Points {
				measureFloat(point.X)
				measureFloat(point.Y)
			}
		}
	}
	builder.Grow(size)
	writeFloat := func(value float64) {
		builder.WriteByte(' ')
		builder.Write(strconv.AppendFloat(scratch[:0], value, 'f', 2, 64))
	}

	for i, cmd := range p {
		if i > 0 {
			builder.WriteString(" ")
		}

		switch cmd.Type {
		case ArcTo:
			builder.WriteString(string(cmd.Type))
			writeFloat(cmd.Arc.RX)
			writeFloat(cmd.Arc.RY)
			writeFloat(cmd.Arc.XAxisRotation)

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
			writeFloat(cmd.Arc.EndPoint.X)
			writeFloat(cmd.Arc.EndPoint.Y)

		default:
			builder.WriteString(string(cmd.Type))
			for _, point := range cmd.Points {
				writeFloat(point.X)
				writeFloat(point.Y)
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
func (p SVGPath) CalculateBoundingBox() (float64, float64, float64, float64) {
	if len(p) == 0 {
		return 0, 0, 0, 0
	}
	var minX, minY, maxX, maxY float64

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

	return minX, minY, maxX, maxY
}

// CountCommands 统计路径中各类命令的数量。
func (p SVGPath) CountCommands() map[CommandType]int {
	counts := make(map[CommandType]int)
	for _, cmd := range p {
		counts[cmd.Type]++
	}
	return counts
}

// GetStartPoint 获取指定命令执行前的当前点；闭合后返回子路径起点。
// 对于首条命令或找不到有效点的情况返回错误。
func (p SVGPath) GetStartPoint(cmdIndex int) (StPos, error) {
	if cmdIndex < 0 || cmdIndex >= len(p) {
		return StPos{}, fmt.Errorf("命令索引超出范围")
	}

	// GB/T 33190—2016 表 36、表 37：闭合到分路径起点，并以该点为当前点。
	for i := cmdIndex - 1; i >= 0; i-- {
		cmd := p[i]
		if cmd.Type == Close {
			for j := i - 1; j >= 0; j-- {
				if (p[j].Type == Start || p[j].Type == MoveTo) && len(p[j].Points) > 0 {
					return p[j].Points[0], nil
				}
			}
			return StPos{}, fmt.Errorf("未找到有效的子路径起点")
		}
		if cmd.Type == ArcTo && cmd.Arc != nil {
			return cmd.Arc.EndPoint, nil
		} else if len(cmd.Points) > 0 {
			return cmd.Points[len(cmd.Points)-1], nil
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
