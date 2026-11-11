package contract

import "github.com/zc310/ofd/pkg/reader"

// TextRun 使用阅读器字段契约，遮蔽网页文字层不暴露的来源字段。
type TextRun struct {
	reader.TextRun

	Source string `json:"source,omitempty"`
}

// TextRuns 返回网页文字层契约；空文字和字符列表均为空数组。
func TextRuns(runs []reader.TextRun) []TextRun {
	result := make([]TextRun, len(runs))
	for i, run := range runs {
		if run.Glyphs == nil {
			run.Glyphs = []reader.Glyph{}
		}
		result[i] = TextRun{TextRun: run}
	}
	return result
}
