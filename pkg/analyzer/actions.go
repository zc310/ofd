package analyzer

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/parser"
)

// 只沿已知结构引用审计，不把附件中的 XML 或包内孤立 XML 当作文档动作。
func (a *analyzer) analyzeActions(documentIndex int, doc *parser.Document, documentPath string) {
	files := map[string]bool{documentPath: true}
	changed := true
	for changed {
		changed = false
		for _, edge := range a.fileEdges {
			if !files[edge.From] || files[edge.To] {
				continue
			}
			include := false
			switch edge.Type {
			case "page", "public-resource", "document-resource", "page-resource":
				include = true
			case "template":
				include = a.options.IncludeTemplates
			case "annotations", "annotation-page":
				include = a.options.IncludeAnnotations
			}
			if include {
				files[edge.To], changed = true, true
			}
		}
	}
	paths := make([]string, 0, len(files))
	for file := range files {
		paths = append(paths, file)
	}
	sort.Strings(paths)
	for _, file := range paths {
		scanner := actionScanner{documentIndex: documentIndex, file: file}
		for _, page := range a.report.Pages {
			if page.DocumentIndex == documentIndex && page.BaseLoc == file {
				n := page.PageNumber
				scanner.pageNumber = &n
				break
			}
		}
		if err := doc.FileCache.ReadXML(file, &scanner); err != nil {
			a.finding(Diagnostic{Code: "actions.read_failed", Stage: "actions", Severity: "warning", File: file, Message: fmt.Sprintf("动作审计无法读取结构文件(%s): %v", file, err)}, true)
		}
		for _, action := range scanner.actions {
			a.report.Actions = append(a.report.Actions, action)
			code, severity, message := "action.uri", "info", "发现 URI 动作（未访问目标）"
			if action.External {
				code, message = "action.external_uri", "发现外部 URI 动作（未访问目标，不代表文档非法）"
			}
			if action.ResolvedURI == "" {
				code, severity, message = "action.invalid_uri", "warning", "URI 或 Base 无法解析"
			}
			a.finding(Diagnostic{Code: code, Severity: severity, Stage: "actions", File: file, PageNumber: action.PageNumber, ObjectID: action.ObjectID, Location: action.Location,
				Message: message + "：" + action.URI, Basis: "GB/T 33190—2016 第 14.4 节表 56；仅静态审计"}, false)
		}
	}
}

type actionNode struct {
	name                xml.Name
	id, event, location string
	children            map[string]int
}

type actionScanner struct {
	documentIndex int
	file          string
	pageNumber    *int
	actions       []URIActionInfo
}

func ofdActionName(name xml.Name, local string) bool {
	return name.Local == local && (name.Space == "http://www.ofdspec.org/2016" || name.Space == "")
}

func (s *actionScanner) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	stack := []actionNode{}
	push := func(element xml.StartElement) {
		id, event, location := "", "", ""
		index := 1
		if len(stack) > 0 {
			parent := &stack[len(stack)-1]
			parent.children[element.Name.Local]++
			index, id, event, location = parent.children[element.Name.Local], parent.id, parent.event, parent.location
		}
		for _, attr := range element.Attr {
			if attr.Name.Space != "" {
				continue
			}
			if attr.Name.Local == "ID" {
				id = attr.Value
			}
			if attr.Name.Local == "Event" {
				event = attr.Value
			}
		}
		location += fmt.Sprintf("/%s[%d]", element.Name.Local, index)
		if len(stack) >= 2 && ofdActionName(element.Name, "URI") && ofdActionName(stack[len(stack)-1].name, "Action") && ofdActionName(stack[len(stack)-2].name, "Actions") {
			action := URIActionInfo{DocumentIndex: s.documentIndex, PageNumber: s.pageNumber, File: s.file, Location: location, ObjectID: id, Event: event}
			for _, attr := range element.Attr {
				if attr.Name.Space != "" {
					continue
				}
				switch attr.Name.Local {
				case "URI":
					action.URI = attr.Value
				case "Base":
					action.Base = attr.Value
				}
			}
			resolveActionURI(&action)
			s.actions = append(s.actions, action)
		}
		stack = append(stack, actionNode{name: element.Name, id: id, event: event, location: location, children: make(map[string]int)})
	}
	push(start)
	for len(stack) > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			push(element)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	return nil
}

func resolveActionURI(action *URIActionInfo) {
	value, err := url.Parse(action.URI)
	if err != nil || strings.TrimSpace(action.URI) == "" {
		return
	}
	if action.Base != "" {
		base, baseErr := url.Parse(action.Base)
		if baseErr != nil {
			return
		}
		value = base.ResolveReference(value)
	}
	action.ResolvedURI, action.Scheme = value.String(), strings.ToLower(value.Scheme)
	// 无 Base 的相对地址也可能访问外部目标；纯片段引用不算外部地址。
	action.External = value.Scheme != "" || value.Host != "" || value.Path != "" || value.RawQuery != ""
}
