package models

import (
	"fmt"
	"strconv"
)

// 以下数值助手只服务快速解析，标准库入口的解码由 pkg/spec 自行完成。
func decodeFloatAttr(value, name string) (float64, error) {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("解析%s失败: %w", name, err)
	}
	return v, nil
}

func decodeIntAttr(value, name string) (int, error) {
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析%s失败: %w", name, err)
	}
	return int(v), nil
}

func decodeBoolAttr(value, name string) (bool, error) {
	v, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("解析%s失败: %w", name, err)
	}
	return v, nil
}

func decodeAlphaAttr(value string) (*uint8, error) {
	n, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return nil, fmt.Errorf("解析Alpha失败: %w", err)
	}
	v := uint8(n)
	return &v, nil
}
