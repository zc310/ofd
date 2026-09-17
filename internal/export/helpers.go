package export

import (
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
)

func exportBox(box *models.StBox) *manifest.Box {
	if box == nil {
		return nil
	}
	width, height := box.Width, box.Height
	// creator 要求对象边界为正数，而部分阅读器会生成零尺寸文本框。
	if width <= 0 {
		width = 0.001
	}
	if height <= 0 {
		height = 0.001
	}
	return &manifest.Box{X: box.X, Y: box.Y, Width: width, Height: height}
}

func exportCTM(ctm *models.CTM) []float64 {
	if ctm == nil {
		return nil
	}
	return append([]float64(nil), ctm[:]...)
}

func detectFontFormat(data []byte, hint string) string {
	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(hint)), ".")
	switch format {
	case "ttf", "otf", "ttc":
		return format
	}
	if len(data) < 4 {
		return ""
	}
	switch string(data[:4]) {
	case "\x00\x01\x00\x00", "true":
		return "ttf"
	case "OTTO":
		return "otf"
	case "ttcf":
		return "ttc"
	default:
		return ""
	}
}

func optionalFill(value string) *bool {
	if value == "" {
		return nil
	}
	result := strings.EqualFold(value, "true")
	return &result
}

func textValue(codes []models.TextCode) string {
	var result strings.Builder
	for _, code := range codes {
		result.WriteString(code.Value)
	}
	return result.String()
}

// exportTextCodes 将 OFD 文字代码列表转换为 manifest 文字代码，跳过空白文本。
func exportTextCodes(values []models.TextCode) []manifest.TextCode {
	var result []manifest.TextCode
	for _, code := range values {
		if strings.TrimSpace(code.Value) == "" {
			continue
		}
		x, y := code.X, code.Y
		result = append(result, manifest.TextCode{Value: code.Value, X: &x, Y: &y, DeltaX: append([]float64(nil), code.DeltaX...), DeltaY: append([]float64(nil), code.DeltaY...)})
	}
	return result
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func sortedIDs[T any](values map[models.StID]T) []models.StID {
	ids := make([]models.StID, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
