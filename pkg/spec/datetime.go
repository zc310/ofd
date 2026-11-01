package spec

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"
)

var errDateTime = errors.New("无法解析时间")

// DateTime 保存OFD的日期或日期时间，支持XML元素和属性解码。
// 零值表示未设置；除标准日期和RFC3339格式外，保留既有非规范日期格式的兼容解析。
type DateTime struct {
	time.Time
}

// UnmarshalXML 解析日期或日期时间元素。
func (t *DateTime) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var v string
	if err := d.DecodeElement(&v, &start); err != nil {
		return err
	}
	return t.parseTime(v)
}

// UnmarshalXMLAttr 解析日期或日期时间属性。
func (t *DateTime) UnmarshalXMLAttr(attr xml.Attr) error {
	return t.parseTime(attr.Value)
}

func (t *DateTime) parseTime(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		*t = DateTime{}
		return nil
	}
	formats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,

		// 以下保留既有兼容格式，不作为标准格式要求。
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999Z07:00",
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-1-2 15:4:5",
		"2006-1-2",
		"2006/01/02 15:04:05",
		"2006/1/02 15:04:05",
		"2006/1/2 15:4:5",
		"2006/1/2",
		"01/02/2006 3:04:05 PM",
		"01/02/2006 15:04:05",
		"20060102",
		"20060102150405",
		"200601021504",
		"2006-01-02-15-04-05",
	}
	for _, format := range formats {
		if parsedTime, err := time.Parse(format, v); err == nil {
			*t = DateTime{parsedTime}
			return nil
		}
	}
	return fmt.Errorf("%w: %s", errDateTime, v)
}
