package main

import (
	"bytes"
	"errors"
	"io"

	"github.com/goccy/go-json/jsontext"
	"github.com/goccy/go-json/v2"
)

// decodeStrictJSON 严格解析 JSON：既拒绝未知字段，也拒绝尾随内容。
func decodeStrictJSON(data []byte, v any) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data), json.RejectUnknownMembers(true))
	if err := json.UnmarshalDecode(decoder, v); err != nil {
		return err
	}
	// 再解一个值并要求 EOF，区分多段 JSON 与尾随的非法内容。
	var extra any
	switch err := json.UnmarshalDecode(decoder, &extra); {
	case err == nil:
		return errors.New("JSON 之后还有第二个值，请求体必须只有一段")
	case errors.Is(err, io.EOF):
		return nil
	default:
		return err
	}
}
