// Package ocr 提供 OCR 引擎的统一接口与结果标准化。
package ocr

import (
	"context"
	"image"
	"sort"
	"strings"
	"unicode"
)

// TextBlock 是 OCR 返回的一行文字。Bounds 使用图片像素坐标，原点在左上角。
type TextBlock struct {
	Text       string
	Bounds     image.Rectangle
	Confidence float64
}

// Engine 是 OCR 后端接口。实现必须尊重 ctx 的取消信号。
type Engine interface {
	Recognize(ctx context.Context, image image.Image) ([]TextBlock, error)
}

// Normalize 清理 OCR 结果并按从上到下、从左到右稳定排序。
func Normalize(blocks []TextBlock, bounds image.Rectangle) []TextBlock {
	result := make([]TextBlock, 0, len(blocks))
	for _, block := range blocks {
		block.Text = strings.Join(strings.Fields(block.Text), " ")
		if block.Text == "" {
			continue
		}
		block.Bounds = block.Bounds.Intersect(bounds)
		if block.Bounds.Empty() {
			continue
		}
		block.Confidence = min(100, max(0, block.Confidence))
		result = append(result, block)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i].Bounds, result[j].Bounds
		if a.Min.Y != b.Min.Y {
			return a.Min.Y < b.Min.Y
		}
		if a.Min.X != b.Min.X {
			return a.Min.X < b.Min.X
		}
		return result[i].Text < result[j].Text
	})
	return result
}

// JoinWords 合并同一行的 OCR 单词。汉字相邻 token 不插空格，拉丁字母/数字间插空格。
func JoinWords(words []string) string {
	var b strings.Builder
	var previous rune
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		first, _ := firstRune(word)
		if b.Len() > 0 && needSpace(previous, first) {
			b.WriteByte(' ')
		}
		b.WriteString(word)
		previous, _ = lastRune(word)
	}
	return b.String()
}

func needSpace(previous, next rune) bool {
	if isCJK(previous) && isCJK(next) {
		return false
	}
	wordRune := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	return wordRune(previous) && wordRune(next)
}

func firstRune(value string) (rune, bool) {
	for _, r := range value {
		return r, true
	}
	return 0, false
}

func lastRune(value string) (rune, bool) {
	var last rune
	found := false
	for _, r := range value {
		last, found = r, true
	}
	return last, found
}

func isCJK(r rune) bool {
	return (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0xF900 && r <= 0xFAFF)
}
