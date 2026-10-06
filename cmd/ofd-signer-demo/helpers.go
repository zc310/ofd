package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"

	"github.com/zc310/ofd/internal/sealimg"
)

// placeholderSeal 返回演示用的印章图片。
//
// 图片由 internal/sealimg 现场渲染：底部印有「非正式印章」，顶��是项目来源，
// 中间是五角星。它不是有效签章，只让 SignedValue.dat 结构和页面渲染效果完整——
// 真实印章图片来自单位备案，证书来自 CA。
//
// 早期版本内嵌一张 938x938 的 PNG 占位图（红圈加「中」字）。改为渲染之后不再
// 维护二进制资源，印章外观也与其他测试产物统一；demo 本来每次运行都重新生成
// 密钥和证书，输出本来就不固定，因此不影响任何确定性约定。
func placeholderSeal() ([]byte, error) {
	return sealimg.RenderPNG(sealimg.Options{Width: 512, Height: 512})
}

// pictureSize 返回演示印章图片的宽高，用于填充 SES_ESPictrueInfo.Width/Height。
func pictureSize() (int, int, error) {
	data, err := placeholderSeal()
	if err != nil {
		return 0, 0, fmt.Errorf("生成演示印章图片失败: %w", err)
	}
	return pictureSizeOf(data)
}

// pictureSizeOf 读取印章图片的像素尺寸。SES_ESPictrueInfo.Width/Height 必须与
// 图片实际尺寸一致，不能凭空填。
func pictureSizeOf(data []byte) (int, int, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("解析演示印章图片失败: %w", err)
	}
	return config.Width, config.Height, nil
}
