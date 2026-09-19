package export

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
)

func (e *documentExporter) exportDestination(value models.CtDest) (manifest.GotoAction, error) {
	page, ok := e.pageIndexes[models.StID(value.PageID)]
	if !ok {
		if e.skipInvalidDestinations {
			return manifest.GotoAction{}, errSkipAction
		}
		return manifest.GotoAction{}, fmt.Errorf("跳转引用了不存在的页面 ID %d", value.PageID)
	}
	return manifest.GotoAction{Page: page, Type: string(value.Type), Left: value.Left, Top: value.Top, Right: value.Right, Bottom: value.Bottom, Zoom: value.Zoom}, nil
}

func (e *documentExporter) exportOutlines(values []models.CTOutlineElem) ([]manifest.Outline, error) {
	result := make([]manifest.Outline, 0, len(values))
	for index, value := range values {
		item := manifest.Outline{Title: value.Title, Count: value.Count, Expanded: value.Expanded}
		if value.Actions != nil {
			actions, err := e.exportActionList(value.Actions.Actions)
			if err != nil {
				return nil, fmt.Errorf("[%d] 动作转换失败: %w", index, err)
			}
			item.Actions = actions
		}
		children, err := e.exportOutlines(value.OutlineElem)
		if err != nil {
			return nil, err
		}
		item.Children = children
		result = append(result, item)
	}
	return result, nil
}

func (e *documentExporter) exportActionList(values []models.CtAction) ([]manifest.Action, error) {
	result := make([]manifest.Action, 0, len(values))
	for index, value := range values {
		action, err := e.exportAction(value)
		if err != nil {
			if e.skipInvalidDestinations && errors.Is(err, errSkipAction) {
				continue
			}
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, action)
	}
	return result, nil
}

func (e *documentExporter) exportAction(value models.CtAction) (manifest.Action, error) {
	result := manifest.Action{Event: string(value.Event)}
	if value.Region != nil {
		region, err := exportActionRegion(value.Region)
		if err != nil {
			return manifest.Action{}, err
		}
		result.Region = region
	}
	if value.URI != nil {
		result.URI = &manifest.URIAction{URI: value.URI.URI, Base: valueOrEmpty(value.URI.Base), Target: valueOrEmpty(value.URI.Target)}
	}
	if value.Goto != nil {
		gotoValue := manifest.GotoAction{}
		if value.Goto.Bookmark != nil {
			gotoValue.Bookmark = value.Goto.Bookmark.Name
		} else if value.Goto.Dest != nil {
			var err error
			gotoValue, err = e.exportDestination(*value.Goto.Dest)
			if err != nil {
				return manifest.Action{}, err
			}
		} else {
			return manifest.Action{}, errors.New("goto 动作缺少 Dest 或 Bookmark")
		}
		result.Goto = &gotoValue
	}
	if value.GotoA != nil {
		newWindow := value.GotoA.NewWindow
		attachID := value.GotoA.AttachID
		if mapped, ok := e.attachmentIDs[attachID]; ok {
			attachID = mapped
		}
		result.GotoA = &manifest.GotoAAction{AttachID: attachID, NewWindow: &newWindow}
	}
	if value.Sound != nil {
		result.Sound = &manifest.SoundAction{ResourceID: uint64(value.Sound.ResourceID), Volume: value.Sound.Volume, Repeat: value.Sound.Repeat, Synchronous: value.Sound.Synchronous}
	}
	if value.Movie != nil {
		result.Movie = &manifest.MovieAction{ResourceID: uint64(value.Movie.ResourceID), Operator: string(value.Movie.Operator)}
	}
	return result, nil
}

func exportActionRegion(value *models.CtRegion) (*manifest.ActionRegion, error) {
	result := &manifest.ActionRegion{}
	for _, area := range value.Areas {
		converted := manifest.ActionArea{Start: manifest.Point{X: area.Start.X, Y: area.Start.Y}}
		for _, pathValue := range area.Paths {
			command := manifest.RegionCommand{}
			switch pathValue.XMLName.Local {
			case "Move":
				command.Type = "move"
				command.X, command.Y = pointValue(pathValue.Point1)
			case "Line":
				command.Type = "line"
				command.X, command.Y = pointValue(pathValue.Point1)
			case "QuadraticBezier":
				command.Type = "quadratic"
				command.ControlX, command.ControlY = pointValue(pathValue.Point1)
				command.X, command.Y = pointValue(pathValue.Point2)
			case "CubicBezier":
				command.Type = "cubic"
				command.Control1X, command.Control1Y = pointValue(pathValue.Point1)
				command.Control2X, command.Control2Y = pointValue(pathValue.Point2)
				command.X, command.Y = pointValue(pathValue.Point3)
			case "Arc":
				command.Type = "arc"
				if pathValue.SweepDirection != nil {
					command.SweepDirection = *pathValue.SweepDirection
				}
				if pathValue.LargeArc != nil {
					command.LargeArc = *pathValue.LargeArc
				}
				if pathValue.RotationAngle != nil {
					command.RotationAngle = *pathValue.RotationAngle
				}
				command.EllipseWidth, command.EllipseHeight = arrayPoint(pathValue.EllipseSize)
				command.X, command.Y = pointValue(pathValue.EndPoint)
			case "Close":
				command.Type = "close"
			default:
				return nil, fmt.Errorf("不支持的动作区域命令类型 %q", pathValue.XMLName.Local)
			}
			converted.Commands = append(converted.Commands, command)
		}
		result.Areas = append(result.Areas, converted)
	}
	return result, nil
}

func pointValue(value *models.StPos) (float64, float64) {
	if value == nil {
		return 0, 0
	}
	return value.X, value.Y
}

func arrayPoint(value *models.StArray) (float64, float64) {
	if value == nil || len(*value) < 2 {
		return 0, 0
	}
	x, _ := strconv.ParseFloat((*value)[0], 64)
	y, _ := strconv.ParseFloat((*value)[1], 64)
	return x, y
}

func (e *documentExporter) convertItems(items []models.PageItem) ([]manifest.Item, error) {
	result := make([]manifest.Item, 0, len(items))
	for _, item := range items {
		converted, err := e.convertItem(item)
		if err != nil {
			return nil, err
		}
		if converted.Type == "text" && strings.TrimSpace(converted.Value) == "" && len(converted.TextCodes) == 0 {
			continue
		}
		result = append(result, converted)
	}
	return result, nil
}

func (e *documentExporter) convertItem(item models.PageItem) (manifest.Item, error) {
	switch item.Kind {
	case models.PageItemText:
		text := item.Text.CtText
		result, err := e.graphicItem("text", text.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.Value = textValue(text.TextCode)
		result.Font = e.fonts[models.StID(text.Font)]
		result.Size = text.Size
		result.Stroke = text.Stroke
		result.Fill = optionalFill(text.Fill)
		result.HScale = text.HScale
		result.ReadDirection = text.ReadDirection
		result.CharDirection = text.CharDirection
		result.Weight = text.Weight
		result.Italic = text.Italic
		result.FillColor, err = e.exportColor(text.FillColor)
		if err != nil {
			return manifest.Item{}, err
		}
		result.StrokeColor, err = e.exportColor(text.StrokeColor)
		if err != nil {
			return manifest.Item{}, err
		}
		result.TextCodes = exportTextCodes(text.TextCode)
		for _, transform := range text.CGTransform {
			result.CGTransforms = append(result.CGTransforms, manifest.CGTransform{CodePosition: transform.CodePosition, CodeCount: transform.CodeCount, GlyphCount: transform.GlyphCount, Glyphs: append([]int(nil), transform.Glyphs...)})
		}
		return result, nil
	case models.PageItemPath:
		pathValue := item.Path.CtPath
		result, err := e.graphicItem("path", pathValue.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.Data = pathValue.AbbreviatedData.String()
		result.Stroke = pathValue.Stroke.Value(true)
		if pathValue.Stroke.IsSet() {
			value := result.Stroke
			result.StrokeSet = &value
		}
		fill := pathValue.Fill
		result.Fill = &fill
		result.Rule = pathValue.Rule
		result.FillColor, err = e.exportColor(pathValue.FillColor)
		if err != nil {
			return manifest.Item{}, err
		}
		result.StrokeColor, err = e.exportColor(pathValue.StrokeColor)
		if err != nil {
			return manifest.Item{}, err
		}
		return result, nil
	case models.PageItemImage:
		imageValue := item.Image.CtImage
		result, err := e.graphicItem("image", imageValue.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.ResourceID = uint64(imageValue.ResourceID)
		result.Substitution = uint64(imageValue.Substitution)
		result.ImageMask = uint64(imageValue.ImageMask)
		result.Border, err = e.exportImageBorder(imageValue.Border)
		if err != nil {
			return manifest.Item{}, err
		}
		return result, nil
	case models.PageItemComposite:
		composite := item.Composite.CtComposite
		result, err := e.graphicItem("composite", composite.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.ResourceID = uint64(composite.ResourceID)
		if composite.ResourceID != 0 {
			e.composites[models.StID(composite.ResourceID)] = true
		}
		return result, nil
	case models.PageItemBlock:
		items, err := e.convertItems(item.Block.Items)
		return manifest.Item{Type: "page-block", Items: items}, err
	default:
		return manifest.Item{}, fmt.Errorf("不支持的页面对象类型: %d", item.Kind)
	}
}

func (e *documentExporter) graphicItem(kind string, graphic models.CTGraphicUnit) (manifest.Item, error) {
	var dashPattern []float64
	if graphic.DashPattern != nil {
		dashPattern = append([]float64(nil), *graphic.DashPattern...)
	}
	boundary := exportBox(&graphic.Boundary)
	result := manifest.Item{Type: kind, X: boundary.X, Y: boundary.Y, Width: boundary.Width, Height: boundary.Height, Name: graphic.Name, DrawParam: e.drawParamName(graphic.DrawParam), LineWidth: graphic.LineWidth, Cap: graphic.Cap, Join: graphic.Join, MiterLimit: graphic.MiterLimit, DashOffset: graphic.DashOffset, DashPattern: dashPattern, Alpha: graphic.Alpha, CTM: exportCTM(graphic.CTM), Visible: graphic.Visible.Bool()}
	if graphic.Actions != nil {
		actions, err := e.exportActionList(graphic.Actions.Action)
		if err != nil {
			return manifest.Item{}, fmt.Errorf("转换图元动作失败: %w", err)
		}
		result.Actions = actions
	}
	clips, err := e.convertClips(graphic.Clips)
	if err != nil {
		return manifest.Item{}, fmt.Errorf("转换图元裁剪失败: %w", err)
	}
	result.Clips = clips
	return result, nil
}

// convertClips 将 OFD 图元裁剪区域转换为 manifest 裁剪定义。
func (e *documentExporter) convertClips(clips *models.Clips) ([]manifest.Clip, error) {
	if clips == nil || len(clips.Clip) == 0 {
		return nil, nil
	}
	result := make([]manifest.Clip, 0, len(clips.Clip))
	for clipIndex, clip := range clips.Clip {
		converted := manifest.Clip{Areas: make([]manifest.ClipArea, 0, len(clip.Area))}
		for areaIndex, area := range clip.Area {
			var drawParam models.StRefID
			if area.DrawParam != nil {
				drawParam = *area.DrawParam
			}
			value := manifest.ClipArea{DrawParam: e.drawParamName(drawParam), CTM: exportCTM(area.CTM)}
			if area.Path != nil {
				ctPath := area.Path
				fillColor, err := e.exportColor(ctPath.FillColor)
				if err != nil {
					return nil, fmt.Errorf("clips[%d].areas[%d].path.fill_color: %w", clipIndex, areaIndex, err)
				}
				strokeColor, err := e.exportColor(ctPath.StrokeColor)
				if err != nil {
					return nil, fmt.Errorf("clips[%d].areas[%d].path.stroke_color: %w", clipIndex, areaIndex, err)
				}
				var dashPattern []float64
				if ctPath.DashPattern != nil {
					dashPattern = append([]float64(nil), *ctPath.DashPattern...)
				}
				stroke := ctPath.Stroke.Value(true)
				var strokeSet *bool
				if ctPath.Stroke.IsSet() {
					strokeSet = &stroke
				}
				value.Path = &manifest.ClipPath{
					Boundary:    *exportBox(&ctPath.Boundary),
					Name:        ctPath.Name,
					Visible:     ctPath.Visible.Bool(),
					CTM:         exportCTM(ctPath.CTM),
					Data:        ctPath.AbbreviatedData.String(),
					Stroke:      stroke,
					StrokeSet:   strokeSet,
					Fill:        ctPath.Fill,
					Rule:        ctPath.Rule,
					LineWidth:   ctPath.LineWidth,
					Cap:         ctPath.Cap,
					Join:        ctPath.Join,
					MiterLimit:  ctPath.MiterLimit,
					DashOffset:  ctPath.DashOffset,
					DashPattern: dashPattern,
					Alpha:       ctPath.Alpha,
					StrokeColor: strokeColor,
					FillColor:   fillColor,
				}
			}
			if area.Text != nil {
				text := area.Text
				fillColor, err := e.exportColor(text.FillColor)
				if err != nil {
					return nil, fmt.Errorf("clips[%d].areas[%d].text.fill_color: %w", clipIndex, areaIndex, err)
				}
				strokeColor, err := e.exportColor(text.StrokeColor)
				if err != nil {
					return nil, fmt.Errorf("clips[%d].areas[%d].text.stroke_color: %w", clipIndex, areaIndex, err)
				}
				value.Text = &manifest.ClipText{
					Boundary:      *exportBox(&text.Boundary),
					CTM:           exportCTM(text.CTM),
					Font:          e.fonts[models.StID(text.Font)],
					Size:          text.Size,
					Value:         textValue(text.TextCode),
					TextCodes:     exportTextCodes(text.TextCode),
					Stroke:        text.Stroke,
					Fill:          optionalFill(text.Fill),
					HScale:        text.HScale,
					ReadDirection: text.ReadDirection,
					CharDirection: text.CharDirection,
					Weight:        text.Weight,
					Italic:        text.Italic,
					FillColor:     fillColor,
					StrokeColor:   strokeColor,
				}
			}
			converted.Areas = append(converted.Areas, value)
		}
		result = append(result, converted)
	}
	return result, nil
}
