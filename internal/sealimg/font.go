package sealimg

import (
	"fmt"
	"strings"

	"github.com/tdewolff/canvas"
)

// fontCandidates 是按名字查找系统字体的候选顺序。
//
// 顺序按「中文环境下的常见程度」排：Windows 走微软雅黑/黑体，macOS 走苹方/
// 华文黑体，Linux 与容器走 Noto/思源/文泉驿。带上英文名是因为 canvas 的
// FindSystemFont 走 fontconfig 家族匹配，不同平台登记的家族名不一致。
// 这份列表抄自 internal/render/backends/canvas/document_font.go 的同类候选，
// 新增平台时两处一起改。
var fontCandidates = []string{
	"Noto Sans CJK SC", "Noto Sans SC", "Source Han Sans SC", "Noto Serif CJK SC",
	"WenQuanYi Micro Hei", "WenQuanYi Zen Hei", "Droid Sans Fallback",
	"Microsoft YaHei", "微软雅黑", "SimHei", "黑体",
	"PingFang SC", "Hiragino Sans GB", "STHeiti", "Heiti SC",
}

// pointsPerUnit 是 canvas 字体接口的换算系数：Face 的字号单位是磅，而本包的
// 画布单位是像素（1 单位 = 1 磅对应的 1/2.83465 毫米）。与
// internal/render/backends/canvas/font_face.go 里的 2.83465 同一个来源。
const pointsPerUnit = 2.83465

// faces 是渲染一枚印章所需的三档字号字体。
type faces struct {
	main   *canvas.FontFace // 底字弧字
	top    *canvas.FontFace // 顶部弧字，椭圆章单独一档
	center *canvas.FontFace // 五角星下方来源小字
}

// resolveFaces 按文案确定要用的三档字体。
//
// 中文文案在中文字体缺失时退回英文文案重试一次：这是让没有中文字体的英文系统
// 仍能出图的关键一步。回退只在「自动选字体」时发生——调用方显式点名了字体却
// 拿不到中文字形，属于字体名写错或字体不对版，应该报错而不是悄悄换一套文案。
func resolveFaces(opts Options, shortSide int) (faces, sealText, error) {
	text := resolveText(opts.Language)
	built, err := loadAllFaces(opts, shortSide, text)
	if err == nil {
		return built, text, nil
	}
	// 英文文案在缺中文字体时本就不该走到这里（它只要拉丁字形），所以这里必然是
	// 中文文案失败，且失败原因是缺中文字形。
	if !text.needsCJK() || opts.FontName != "" {
		return faces{}, sealText{}, err
	}
	text = textEnglish
	built, fallbackErr := loadAllFaces(opts, shortSide, text)
	if fallbackErr != nil {
		return faces{}, sealText{}, fallbackErr
	}
	return built, text, nil
}

// loadAllFaces 按文案取齐三档字号字体。
func loadAllFaces(opts Options, shortSide int, text sealText) (faces, error) {
	var result faces
	var err error
	if result.main, err = loadFace(opts.FontName, float64(shortSide)*textSizeRatio, text); err != nil {
		return faces{}, err
	}
	result.top = result.main
	if opts.Shape == ShapeEllipse {
		if result.top, err = loadFace(opts.FontName, float64(shortSide)*ellipseTopTextSizeRatio, text); err != nil {
			return faces{}, err
		}
	}
	if result.center, err = loadFace(opts.FontName, float64(shortSide)*centerTextSizeRatio, text); err != nil {
		return faces{}, err
	}
	return result, nil
}

// loadFace 按家族名取字体并返回指定字号的合成粗体 FontFace。使用合成粗体而不是
// 依赖系统是否安装独立粗体字面，可以保持不同平台上的字宽和弧形布局稳定。
//
// 显式指定 FontName 时只试这一个名字：调用方点名要某个字体，静默换成另一个
// 会让"我指定的字体没生效"变成一件查不出来的事。只在未指定时才按候选列表逐个
// 尝试，并要求候选字体真的含文案用到的全部字形——底字缺字形等于交付一张带
// 方框的图。
func loadFace(name string, size float64, text sealText) (*canvas.FontFace, error) {
	needCJK := text.needsCJK()
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		face, err := loadNamedFace(trimmed, size)
		if err != nil {
			return nil, fmt.Errorf("加载字体 %q 失败: %w", trimmed, err)
		}
		if !supportsText(face, text) {
			if needCJK {
				return nil, fmt.Errorf("字体 %q 不含中文字形，底字会渲染成方框", trimmed)
			}
			return nil, fmt.Errorf("字体 %q 不含文案用到的字形，底字会渲染成方框", trimmed)
		}
		return face, nil
	}

	tried := make([]string, 0, len(fontCandidates))
	for _, candidate := range fontCandidates {
		tried = append(tried, candidate)
		face, err := loadNamedFace(candidate, size)
		if err != nil {
			continue
		}
		if supportsText(face, text) {
			return face, nil
		}
	}
	if needCJK {
		return nil, fmt.Errorf("未找到覆盖中文的系统字体（已尝试 %s）；"+
			"请用 Options.FontName 指定字体家族名", strings.Join(tried, ", "))
	}
	return nil, fmt.Errorf("未找到覆盖英文文案的系统字体（已尝试 %s）；"+
		"请用 Options.FontName 指定字体家族名", strings.Join(tried, ", "))
}

func loadNamedFace(name string, size float64) (*canvas.FontFace, error) {
	font, err := canvas.LoadSystemFont(name, canvas.FontRegular)
	if err != nil {
		return nil, err
	}
	face := font.Face(size*pointsPerUnit, sealInk)
	face.FauxBold = 0.02
	return face, nil
}

// supportsText 判断字体是否覆盖这套文案的全部字符。
//
// 字体能被加载不等于有汉字：Noto Sans CJK 这类字体的 Latin 子集没问题，但换成
// 只装了 Latin 的字体时，canvas 不会报错，只会把字形替换成 .notdef 方框。只能
// 靠字形 ID 判断。英文文案只含 ASCII，任何常规字体都能通过。
func supportsText(face *canvas.FontFace, text sealText) bool {
	for _, glyph := range face.Glyphs(text.probe()) {
		if glyph.ID == 0 {
			return false
		}
	}
	return true
}
