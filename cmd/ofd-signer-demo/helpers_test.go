package main

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestPictureSizeOfUsesActualImageDimensions(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 23, 17))); err != nil {
		t.Fatal(err)
	}
	width, height, err := pictureSizeOf(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if width != 23 || height != 17 {
		t.Fatalf("应读取图片实际像素尺寸 23x17，实际 %dx%d", width, height)
	}
}

func TestPictureSizeOfRejectsInvalidImage(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "empty"},
		{name: "unknown format", data: []byte("not an image")},
		{name: "truncated PNG", data: []byte("\x89PNG\r\n\x1a\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			width, height, err := pictureSizeOf(test.data)
			if err == nil || !strings.Contains(err.Error(), "解析演示印章图片失败") {
				t.Fatalf("应返回带上下文的图片解析错误，实际 %v", err)
			}
			if width != 0 || height != 0 {
				t.Errorf("解析失败应返回零尺寸，实际 %dx%d", width, height)
			}
		})
	}
}
