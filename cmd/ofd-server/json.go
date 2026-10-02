package main

import (
	"bytes"
	"errors"
	"io"

	"github.com/goccy/go-json"
)

// decodeStrictJSON 严格解析 JSON：既拒绝未知字段，也拒绝尾随内容。
//
// 尾随内容必须一起拒。encoding/json 的 Decoder.Decode 只读第一个值，尾巴上
// 挂什么它都不管——实测把两个对象拼在一起发（`{...}{...}`），第一个照常生效、
// 不报任何错，尾上的第二个被整段丢掉。
//
// 这与"未知字段一律 400"的意图正好相反：调用方把请求体拼错了，拿到的是一个
// "成功"的任务，错误却出在几秒之后的任务日志里，排查时完全看不出是请求体有
// 两段。严格解码若只做一半，等于在最容易被客户端自动化代码踩到的地方留了
// 一个静默失败。
func decodeStrictJSON(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	// 再解一次，期望立刻撞上 EOF。解出值说明请求体不止一段 JSON；解出别的
	// 错误说明尾随内容不是合法 JSON——两种都要报出来。
	var extra any
	switch err := decoder.Decode(&extra); {
	case err == nil:
		return errors.New("JSON 之后还有第二个值，请求体必须只有一段")
	case errors.Is(err, io.EOF):
		return nil
	default:
		return err
	}
}
