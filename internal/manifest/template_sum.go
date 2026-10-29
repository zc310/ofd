package manifest

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	jsonv1 "github.com/goccy/go-json"
	"github.com/goccy/go-json/v2"
)

var templateDecimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// parseTemplateDecimal 仅接受有限十进制，限制指数与有效位数，避免将格式化金额误当数值。
func parseTemplateDecimal(value any) (*big.Rat, error) {
	var text string
	switch value := value.(type) {
	case string:
		text = value
	case jsonv1.Number:
		text = string(value)
	default:
		return nil, errors.New("求和字段必须为十进制字符串或 JSON 数字")
	}
	if len(text) > 4096 || !templateDecimalPattern.MatchString(text) {
		return nil, fmt.Errorf("无效十进制数 %q", text)
	}
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(text[index+1:])
		if err != nil || exponent < -1000 || exponent > 1000 {
			return nil, errors.New("十进制指数必须在 -1000—1000 之间")
		}
	}
	// Rat 的纯整数字面量允许八进制；补小数点，确保 "010" 仍按十进制 10 处理。
	mantissa, exponent := text, ""
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		mantissa, exponent = text[:index], text[index:]
	}
	if !strings.Contains(mantissa, ".") {
		mantissa += ".0"
	}
	text = mantissa + exponent
	result, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("无效十进制数 %q", text)
	}
	return result, nil
}

func aggregateTemplateRows(binding Binding, ctx templateContext) (string, error) {
	if ctx.detail == nil {
		return "", errors.New("aggregate 只能用于 paginate 页面或页面注解")
	}
	d := ctx.detail
	group := d.group
	places := 2
	if binding.DecimalPlaces != nil {
		places = *binding.DecimalPlaces
	}
	keyData, err := json.Marshal([]any{binding.Path, places, binding.Required, binding.Default})
	if err != nil {
		return "", err
	}
	start, end := 0, len(group.data)
	if binding.Aggregate == "page_sum" {
		start, end = d.start, d.end
	}
	key := fmt.Sprintf("%d:%d:%s", start, end, keyData)
	if cached, found := group.sums[key]; found {
		return cached, nil
	}
	total := new(big.Rat)
	for i := start; i < end; i++ {
		value, found := templateLookup(group.data[i], binding.Path)
		if !found || value == nil {
			if binding.Default != nil {
				value = *binding.Default
			} else if binding.Required != nil && !*binding.Required {
				continue
			} else {
				return "", fmt.Errorf("求和明细行 %d 缺少字段 %q", i, binding.Path)
			}
		}
		number, err := parseTemplateDecimal(value)
		if err != nil {
			return "", fmt.Errorf("求和明细行 %d 字段 %q: %w", i, binding.Path, err)
		}
		total.Add(total, number)
	}
	// Rat.FloatString 对恰好半单位的正负值均远离零舍入，不经过二进制浮点。
	result := total.FloatString(places)
	if strings.Trim(result, "-0.") == "" {
		result = strings.TrimPrefix(result, "-")
	}
	group.sums[key] = result
	return result, nil
}
