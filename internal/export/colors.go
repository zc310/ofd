package export

import (
	"fmt"
	"strconv"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
)

func (e *documentExporter) exportColor(value *models.CTColor) (*manifest.Color, error) {
	if value == nil {
		return nil, nil
	}
	result := &manifest.Color{ColorSpace: uint64(value.ColorSpace), Alpha: value.Alpha}
	if value.Value != nil {
		result.R, result.G, result.B = value.Value.R, value.Value.G, value.Value.B
		if value.Value.A != 255 {
			alpha := value.Value.A
			result.Alpha = &alpha
		}
		if value.ColorSpace != 0 {
			components := 3
			switch e.colorSpaces[models.StID(value.ColorSpace)] {
			case "GRAY":
				components = 1
			case "CMYK":
				components = 4
			}
			result.Components = make([]int, components)
			for index := range result.Components {
				result.Components[index] = int(value.Value.R)
			}
		}
	}
	if value.ColorSpace != 0 && value.Value == nil && value.Index == 0 {
		// OFD 允许省略 Value，此时表示所引用颜色空间的零分量。
		// creator 要求显式表达这一语义。
		components := 3
		switch e.colorSpaces[models.StID(value.ColorSpace)] {
		case "GRAY":
			components = 1
		case "CMYK":
			components = 4
		}
		result.Components = make([]int, components)
	}
	if value.Index != 0 {
		index := value.Index
		result.Index = &index
	}
	if value.AxialShd != nil {
		segments, err := e.exportColorStops(value.AxialShd.Segment)
		if err != nil {
			return nil, fmt.Errorf("axial.segments: %w", err)
		}
		result.Axial = &manifest.AxialShading{
			MapType:    value.AxialShd.MapType,
			MapUnit:    value.AxialShd.MapUnit,
			Extend:     value.AxialShd.Extend,
			StartPoint: exportPoint(value.AxialShd.StartPoint),
			EndPoint:   exportPoint(value.AxialShd.EndPoint),
			Segments:   segments,
		}
	}
	if value.RadialShd != nil {
		segments, err := e.exportColorStops(value.RadialShd.Segment)
		if err != nil {
			return nil, fmt.Errorf("radial.segments: %w", err)
		}
		result.Radial = &manifest.RadialShading{
			MapType:      value.RadialShd.MapType,
			MapUnit:      value.RadialShd.MapUnit,
			Eccentricity: value.RadialShd.Eccentricity,
			Angle:        value.RadialShd.Angle,
			StartPoint:   exportPoint(value.RadialShd.StartPoint),
			StartRadius:  value.RadialShd.StartRadius,
			EndPoint:     exportPoint(value.RadialShd.EndPoint),
			EndRadius:    value.RadialShd.EndRadius,
			Extend:       value.RadialShd.Extend,
			Segments:     segments,
		}
	}
	if value.GouraudShd != nil {
		points := make([]manifest.GouraudPoint, 0, len(value.GouraudShd.Point))
		for index, point := range value.GouraudShd.Point {
			color, err := e.exportColor(&point.Color)
			if err != nil {
				return nil, fmt.Errorf("gouraud.points[%d].color: %w", index, err)
			}
			points = append(points, manifest.GouraudPoint{X: point.X, Y: point.Y, EdgeFlag: point.EdgeFlag, Color: *color})
		}
		backColor, err := e.exportColor(value.GouraudShd.BackColor)
		if err != nil {
			return nil, fmt.Errorf("gouraud.back_color: %w", err)
		}
		result.Gouraud = &manifest.Gouraud{Extend: value.GouraudShd.Extend, Points: points, BackColor: backColor}
	}
	laGouraud := value.LaGouraudShd
	if laGouraud == nil {
		laGouraud = value.LaGourandShd
	}
	if laGouraud != nil {
		points := make([]manifest.LaGouraudPoint, 0, len(laGouraud.Point))
		for index, point := range laGouraud.Point {
			color, err := e.exportColor(&point.Color)
			if err != nil {
				return nil, fmt.Errorf("la_gouraud.points[%d].color: %w", index, err)
			}
			points = append(points, manifest.LaGouraudPoint{X: point.X, Y: point.Y, Color: *color})
		}
		backColor, err := e.exportColor(laGouraud.BackColor)
		if err != nil {
			return nil, fmt.Errorf("la_gouraud.back_color: %w", err)
		}
		result.LaGouraud = &manifest.LaGouraud{VerticesPerRow: laGouraud.VerticesPerRow, Extend: laGouraud.Extend, Points: points, BackColor: backColor}
	}
	if value.Pattern != nil {
		items, err := e.convertItems(value.Pattern.CellContent.Items)
		if err != nil {
			return nil, fmt.Errorf("pattern.items: %w", err)
		}
		result.Pattern = &manifest.Pattern{
			Width:         value.Pattern.Width,
			Height:        value.Pattern.Height,
			XStep:         value.Pattern.XStep,
			YStep:         value.Pattern.YStep,
			ReflectMethod: value.Pattern.ReflectMethod,
			RelativeTo:    value.Pattern.RelativeTo,
			CTM:           exportStringFloatArray(value.Pattern.CTM),
			Thumbnail:     uint64(value.Pattern.CellContent.Thumbnail),
			Items:         items,
		}
	}
	return result, nil
}

// exportColorStops 将 OFD 渐变分段转换为 manifest 颜色停止点。
func (e *documentExporter) exportColorStops(values []models.Segment) ([]manifest.ColorStop, error) {
	result := make([]manifest.ColorStop, 0, len(values))
	for index, value := range values {
		color, err := e.exportColor(&value.Color)
		if err != nil {
			return nil, fmt.Errorf("[%d].color: %w", index, err)
		}
		stop := manifest.ColorStop{Color: *color}
		if value.PositionSet {
			position := value.Position
			stop.Position = &position
		}
		result = append(result, stop)
	}
	return result, nil
}

// exportPoint 将渐变坐标格式化为 "x y" 字符串。
func exportPoint(value models.StPos) string {
	return strconv.FormatFloat(value.X, 'g', -1, 64) + " " + strconv.FormatFloat(value.Y, 'g', -1, 64)
}

func (e *documentExporter) exportImageBorder(border *models.Border) (*manifest.ImageBorder, error) {
	if border == nil {
		return nil, nil
	}
	color, err := e.exportColor(border.BorderColor)
	if err != nil {
		return nil, err
	}
	return &manifest.ImageBorder{LineWidth: border.LineWidth, HorizontalRadius: border.HorizonalCornerRadius, VerticalRadius: border.VerticalCornerRadius, DashOffset: border.DashOffset, DashPattern: exportStringFloatArray(border.DashPattern), Color: color}, nil
}

func exportFloatArray(value *models.StArrayF) []float64 {
	if value == nil {
		return nil
	}
	return append([]float64(nil), *value...)
}

func exportStringFloatArray(value models.StArray) []float64 {
	result := make([]float64, 0, len(value))
	for _, item := range value {
		parsed, err := strconv.ParseFloat(item, 64)
		if err == nil {
			result = append(result, parsed)
		}
	}
	return result
}
