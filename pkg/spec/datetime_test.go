package spec_test

import (
	"encoding/xml"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/spec"
)

func TestDateTimeParsesElementsAndAttributesWithoutLosingTimezoneOrPrecision(t *testing.T) {
	for _, text := range []string{
		"2026-01-02", "2026-01-02T03:04:05", "2026-01-02T03:04:05.123456789+08:00", "2026-01-02T03:04:05.123456789Z", "",
	} {
		var element spec.DateTime
		if err := xml.Unmarshal([]byte("<Date>"+text+"</Date>"), &element); err != nil {
			t.Fatal(err)
		}
		var attribute struct {
			Date spec.DateTime `xml:"Date,attr"`
		}
		if err := xml.Unmarshal([]byte(`<Value Date="`+text+`"/>`), &attribute); err != nil {
			t.Fatal(err)
		}
		if !element.Equal(attribute.Date.Time) {
			t.Fatalf("日期元素/属性解析不一致：%q", text)
		}
		if text == "" {
			if !element.IsZero() {
				t.Fatal("空日期未保留零值")
			}
			continue
		}
		data, err := xml.Marshal(element)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip spec.DateTime
		if err := xml.Unmarshal(data, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if !roundTrip.Equal(element.Time) {
			t.Fatalf("日期往返丢失精度：%s", data)
		}
		if text == "2026-01-02T03:04:05.123456789+08:00" {
			_, offset := element.Zone()
			if offset != 8*3600 || element.Nanosecond() != 123456789 {
				t.Fatal("时区或纳秒丢失")
			}
		}
		if text == "2026-01-02T03:04:05" && element.Location() != time.UTC {
			t.Fatal("无时区日期不再按UTC解释")
		}
	}
}

func TestDateTimeRetainsExistingCompatibilityFormatsAndRejectsInvalidInput(t *testing.T) {
	// 迁移前已支持的非规范日期格式，不为新增兼容行为背书。
	for _, text := range []string{"2026/1/2", "20260102", "2026-1-2", "01/02/2026 3:04:05 PM", "2026-01-02 03:04:05.123456789"} {
		var value spec.DateTime
		if err := xml.Unmarshal([]byte("<Date>"+text+"</Date>"), &value); err != nil {
			t.Fatalf("迁移丢失既有日期兼容：%s %v", text, err)
		}
	}
	for _, text := range []string{"invalid", "2026-02-30", "2026-13-01"} {
		var value spec.DateTime
		if err := xml.Unmarshal([]byte("<Date>"+text+"</Date>"), &value); err == nil {
			t.Fatalf("接受非法日期：%s", text)
		}
	}
}
