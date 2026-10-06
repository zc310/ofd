package sealimg

import (
	"fmt"
	"strings"

	"github.com/tdewolff/canvas"
)

// cjkProbe 是用来检测字体覆盖所有固定文案的样字。它由印章上出现的全部文字
// 拼接而成，任何一个字符缺字形都会让对应位置渲染成方框。
var cjkProbe = TopText + BottomText + CenterText

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

// loadFace 按家族名取字体并返回指定字号的合成粗体 FontFace。使用合成粗体而不是
// 依赖系统是否安装独立粗体字面，可以保持不同平台上的字宽和弧形布局稳定。
//
// 显式指定 FontName 时只试这一个名字：调用方点名要某个字体，静默换成另一个
// 会让"我指定的字体没生效"变成一件查不出来的事。只在未指定时才按候选列表逐个
// 尝试，并要求候选字体真的含中文字形——底字是中文，换成不含中文字形的字体等于
// 交付一张带方框的图。
func loadFace(name string, size float64) (*canvas.FontFace, error) {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		face, err := loadNamedFace(trimmed, size)
		if err != nil {
			return nil, fmt.Errorf("加载字体 %q 失败: %w", trimmed, err)
		}
		if !supportsCJK(face) {
			return nil, fmt.Errorf("字体 %q 不含中文字形，底字会渲染成方框", trimmed)
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
		if supportsCJK(face) {
			return face, nil
		}
	}
	return nil, fmt.Errorf("未找到覆盖中文的系统字体（已尝试 %s）；"+
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

// supportsCJK 判断字体是否真的有中文字形。
//
// 字体能被加载不等于有汉字：Noto Sans CJK 这类字体的 Latin 子集没问题，但换成
// 只装了 Latin 的字体时，canvas 不会报错，只会把字形替换成 .notdef 方框。只能
// 靠字形 ID 判断。
func supportsCJK(face *canvas.FontFace) bool {
	for _, glyph := range face.Glyphs(cjkProbe) {
		if glyph.ID == 0 {
			return false
		}
	}
	return true
}
