// Package coloricc 提供基于 ICC 色彩配置文件的设备颜色到 sRGB 的转换。
// 使用纯 Go 的 github.com/kovidgoyal/imaging/prism/meta/icc 引擎，不依赖 cgo。
package coloricc

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kovidgoyal/imaging/prism/meta/icc"
)

// Transformer 把某个设备颜色空间（1/3/4 通道）的颜色转换为 sRGB。
type Transformer struct {
	pipeline *icc.Pipeline
	channels int
	mu       sync.Mutex
}

type cacheKey struct {
	digest   [32]byte
	channels int
}

var buildCache = struct {
	sync.Mutex
	items map[cacheKey]*Transformer
}{items: make(map[cacheKey]*Transformer)}

// New 解析 ICC 配置文件并按输入通道数创建到 sRGB 的转换管线。
// inputChannels 必须与配置文件的颜色空间一致（GRAY=1、RGB=3、CMYK=4）。
// 相同配置文件与通道数的转换器会被缓存复用。
func New(profileData []byte, inputChannels int) (*Transformer, error) {
	if len(profileData) == 0 {
		return nil, fmt.Errorf("ICC 配置文件为空")
	}
	if inputChannels != 1 && inputChannels != 3 && inputChannels != 4 {
		return nil, fmt.Errorf("不支持的 ICC 输入通道数: %d", inputChannels)
	}
	key := cacheKey{digest: sha256.Sum256(profileData), channels: inputChannels}
	buildCache.Lock()
	if cached := buildCache.items[key]; cached != nil {
		buildCache.Unlock()
		return cached, nil
	}
	buildCache.Unlock()

	profile, err := icc.DecodeProfile(bytes.NewReader(profileData))
	if err != nil {
		return nil, fmt.Errorf("解析 ICC 配置文件失败: %w", err)
	}
	pipeline, err := profile.CreateTransformerToSRGB(icc.PerceptualRenderingIntent, true, inputChannels, true, true, true)
	if err != nil {
		return nil, fmt.Errorf("创建 ICC 转换失败: %w", err)
	}
	transformer := &Transformer{pipeline: pipeline, channels: inputChannels}
	buildCache.Lock()
	buildCache.items[key] = transformer
	buildCache.Unlock()
	return transformer, nil
}

// ToRGB 把分量（0-255）转换为 sRGB 分量（0-255）。
func (t *Transformer) ToRGB(components []uint8) (uint8, uint8, uint8) {
	if t == nil || len(components) < t.channels {
		return 0, 0, 0
	}
	// 管线中的变换器可能改变通道数，输入/输出缓冲都按 4 通道分配，
	// TransformGeneral 会在每步之后把输出复制回输入。
	in := make([]float64, 4)
	out := make([]float64, 4)
	for i := 0; i < t.channels; i++ {
		in[i] = float64(components[i]) / 255
	}
	t.mu.Lock()
	t.pipeline.TransformGeneral(out, in)
	t.mu.Unlock()
	return uint8(out[0]*255 + 0.5), uint8(out[1]*255 + 0.5), uint8(out[2]*255 + 0.5)
}

// deviceCMYKMatrix 是 Adobe/poppler 使用的 4 色印刷矩阵模型的 16 个油墨组合项。
// 索引的 bit0/bit1/bit2/bit3 分别表示青、品红、黄、黑油墨是否出现，值为该组合
// 对应的 sRGB 系数（0-1）。模型假设白纸反射，各组合按出现概率加权求和。
var deviceCMYKMatrix = [16][3]float64{
	{1, 1, 1},                // 无油墨
	{0, 0.6784, 0.9373},      // 青
	{0.9255, 0, 0.5490},      // 品红
	{0.1804, 0.1922, 0.5725}, // 青 + 品红
	{1, 0.9490, 0},           // 黄
	{0, 0.6510, 0.3137},      // 青 + 黄
	{0.9294, 0.1098, 0.1412}, // 品红 + 黄
	{0.2118, 0.2119, 0.2235}, // 青 + 品红 + 黄
	{0.1373, 0.1216, 0.1255}, // 黑
	{0, 0.0588, 0.1412},      // 青 + 黑
	{0.1412, 0, 0},           // 品红 + 黑
	{0, 0, 0.0078},           // 青 + 品红 + 黑
	{0.1098, 0.1020, 0},      // 黄 + 黑
	{0, 0.0745, 0},           // 青 + 黄 + 黑
	{0.1333, 0, 0},           // 品红 + 黄 + 黑
	{0, 0, 0},                // 青 + 品红 + 黄 + 黑
}

// DeviceCMYKToRGB 用 Adobe/poppler 的 4 色印刷矩阵模型把 CMYK 油墨量（0-1）
// 转换为 sRGB 分量（0-1）。当没有可用的 ICC 配置文件时，internal/pdf2ofd 与
// internal/render 都回退到该模型，保证同一 CMYK 颜色在两条路径上一致。
//
// 纯色结果与印刷色一致：纯青 (0,173,239)、纯品红 (236,0,140)、纯黄 (255,242,0)、
// 纯黑 (35,31,32)。相比逐油墨独立吸收的近似模型，混合色（尤其青 + 黄得到的绿）
// 更接近 poppler/Adobe 的渲染结果。
func DeviceCMYKToRGB(c, m, y, k float64) (float64, float64, float64) {
	amounts := [4]float64{c, m, y, k}
	var r, g, b float64
	for mask := 0; mask < 16; mask++ {
		weight := 1.0
		for ink := 0; ink < 4; ink++ {
			if mask&(1<<ink) != 0 {
				weight *= amounts[ink]
			} else {
				weight *= 1 - amounts[ink]
			}
		}
		r += deviceCMYKMatrix[mask][0] * weight
		g += deviceCMYKMatrix[mask][1] * weight
		b += deviceCMYKMatrix[mask][2] * weight
	}
	return r, g, b
}

// defaultCache 按 OFD_CMYK_ICC 的当前取值缓存默认转换器；路径变化时重新加载，
// 便于运行时（含测试）切换配置文件。
var defaultCache struct {
	sync.Mutex
	path        string
	transformer *Transformer
	loaded      bool
}

// DefaultCMYK 返回用于 DeviceCMYK（无内嵌配置文件）的默认转换器。
//
// 只有当显式设置环境变量 OFD_CMYK_ICC 指向一个 ICC 配置文件时才启用；
// 不做隐式的系统路径扫描，避免同一文档在不同机器上转换结果不一致。
// 未设置或加载失败时返回 (nil, nil)，调用方应回退到近似油墨模型。
func DefaultCMYK() (*Transformer, error) {
	path := strings.TrimSpace(os.Getenv("OFD_CMYK_ICC"))
	defaultCache.Lock()
	defer defaultCache.Unlock()
	if defaultCache.loaded && defaultCache.path == path {
		return defaultCache.transformer, nil
	}
	defaultCache.path = path
	defaultCache.loaded = true
	defaultCache.transformer = nil
	if path != "" {
		if data, err := os.ReadFile(filepath.Clean(path)); err == nil {
			if transformer, err := New(data, 4); err == nil {
				defaultCache.transformer = transformer
			}
		}
	}
	return defaultCache.transformer, nil
}
