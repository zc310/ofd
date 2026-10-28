package main

import (
	"net/http"
	"sort"

	"github.com/zc310/ofd/pkg/converter"
)

// formatInfo 是 /v1/formats 里的一种格式。
type formatInfo struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
	MIME       string   `json:"mime,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	// Stream 仅对输出格式有意义：逐页图像必须落盘，不能用 stream。
	Stream bool `json:"stream,omitempty"`
}

type formatsView struct {
	Input  []formatInfo `json:"input"`
	Output []formatInfo `json:"output"`
}

// handleFormats 返回服务实际注册的输入/输出格式，而不是 README 里那张可能
// 过时的静态表。转换库通过 init() 注册格式，这里读的是同一份注册表。
func (s *Server) handleFormats(w http.ResponseWriter, _ *http.Request) {
	view := formatsView{
		Input:  []formatInfo{{Name: "ofd", Extensions: []string{".ofd"}, MIME: "application/ofd", Kind: "document"}},
		Output: []formatInfo{{Name: "ofd", Extensions: []string{".ofd"}, MIME: "application/ofd", Kind: "document", Stream: true}},
	}
	inputSeen := map[string]bool{"ofd": true}
	for _, name := range converter.ImportFormats() {
		imp, ok := converter.ImporterByName(name)
		if !ok || inputSeen[imp.Name()] {
			continue
		}
		inputSeen[imp.Name()] = true
		view.Input = append(view.Input, formatInfo{
			Name:       imp.Name(),
			Extensions: imp.Extensions(),
			MIME:       imp.MIME(),
			Kind:       "document",
		})
	}
	outputSeen := map[string]bool{"ofd": true}
	for _, format := range converter.Formats() {
		if outputSeen[format.Name] {
			continue
		}
		outputSeen[format.Name] = true
		kind := "document"
		stream := true
		if format.Kind == converter.KindImage {
			kind = "image"
			stream = false
		}
		view.Output = append(view.Output, formatInfo{
			Name:       format.Name,
			Extensions: format.Extensions,
			MIME:       format.MIME,
			Kind:       kind,
			Stream:     stream,
		})
	}
	sort.Slice(view.Input, func(i, j int) bool { return view.Input[i].Name < view.Input[j].Name })
	sort.Slice(view.Output, func(i, j int) bool { return view.Output[i].Name < view.Output[j].Name })
	writeJSON(w, http.StatusOK, view)
}
