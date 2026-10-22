package compare

import (
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
	comparelocalization "github.com/zc310/ofd/internal/localization/compare"
)

//go:embed templates/report.html
var reportHTML string

//go:embed templates/report.css
var reportCSS string

//go:embed templates/report.js
var reportJS string

// WriteReport 以 text、markdown、json 或 html 格式输出报告。
// HTML 默认内嵌预览图；使用 ImageWriter 生成的报告保留外部图片相对引用。
func WriteReport(w io.Writer, report *Report, format string) error {
	if report == nil {
		return errors.New("对比报告为空")
	}
	if report.Baseline < 1 || report.Baseline > len(report.Inputs) {
		return errors.New("报告的基准文件序号无效")
	}
	for _, c := range report.Comparisons {
		if c.Target < 1 || c.Target > len(report.Inputs) {
			return errors.New("报告的目标文件序号无效")
		}
		for _, f := range c.Files {
			if f.Before == nil && f.After == nil {
				return errors.New("文件差异没有前后文件信息")
			}
		}
	}
	limit := report.Options.MaxReportBytes
	if limit == 0 {
		limit = 256 << 20
	}
	if limit < 0 {
		return errors.New("报告字节限制必须为正数")
	}
	w = budgetWriter{w, &byteBudget{remaining: limit, label: "报告输出"}}
	format = strings.ToLower(strings.TrimSpace(format))
	view := reportView{report: report}
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	case "html":
		localizer, err := comparelocalization.New(report.Options.Language)
		if err != nil {
			return err
		}
		view.localizer = localizer
		return view.writeHTML(w)
	case "text", "markdown":
		view.markdown = format == "markdown"
		localizer, err := comparelocalization.New(report.Options.Language)
		if err != nil {
			return err
		}
		view.localizer = localizer
		return view.writeText(w)
	default:
		return fmt.Errorf("不支持的报告格式 %q", format)
	}
}

type reportView struct {
	report    *Report
	markdown  bool
	localizer *comparelocalization.Localizer
}

func (v reportView) writeText(w io.Writer) error {
	r := v.report
	output := &reportTextWriter{w: w}
	b := reportTextWriter{w: output}
	if v.markdown {
		output.WriteString("# OFD 对比报告\n\n<pre>")
		b.w = escapedReportWriter{output}
	} else {
		b.WriteString("OFD 对比报告\n")
	}
	fmt.Fprintf(&b, "基准：%d · %s\n视觉对比：%v；页面匹配：%s\n", r.Baseline, r.Inputs[r.Baseline-1].Name, r.Options.Visual, r.Options.PageMatch)
	fmt.Fprintf(&b, "页面范围：%s（分别应用于各输入；包内文件仍按整包比较）\n", pageScope(r.Options.Pages))
	if r.Options.PageMap != "" {
		fmt.Fprintf(&b, "手工页映射：%s\n", r.Options.PageMap)
	}
	for i, input := range r.Inputs {
		if output.err != nil {
			return output.err
		}
		fmt.Fprintf(&b, "输入 %d：%s；%d B；%d 个文档体；%d 页\n", i+1, input.Name, input.Size, input.Documents, len(input.Pages))
		fmt.Fprintf(&b, "  参与比较：%d 页\n", selectedPageCount(input))
		for _, meta := range input.Metadata {
			fmt.Fprintf(&b, "  文档体 %d：标题=%q 作者=%q 主题=%q 生成软件=%q\n", meta.Number, meta.Title, meta.Author, meta.Subject, meta.Creator)
		}
	}
	for _, c := range r.Comparisons {
		if output.err != nil {
			return output.err
		}
		fmt.Fprintf(&b, "\n对比：%s\n整体相同：%v\n文件：新增 %d，删除 %d，修改 %d，移动 %d\n页面：新增 %d，删除 %d，修改 %d，页码变化 %d，画面变化 %d\n", r.Inputs[c.Target-1].Name, c.Equal, c.Summary.FilesAdded, c.Summary.FilesRemoved, c.Summary.FilesModified, c.Summary.FilesMoved, c.Summary.PagesAdded, c.Summary.PagesRemoved, c.Summary.PagesModified, c.Summary.PagesMoved, c.Summary.VisualPagesChanged)
		fmt.Fprintf(&b, "所选页面相同：%v\n", c.PagesEqual)
		for _, d := range c.DirectoriesAdded {
			fmt.Fprintf(&b, "  + 目录 %s/\n", d)
		}
		for _, d := range c.DirectoriesRemoved {
			fmt.Fprintf(&b, "  - 目录 %s/\n", d)
		}
		for _, f := range c.Files {
			if output.err != nil {
				return output.err
			}
			before, after := "", ""
			if f.Before != nil {
				before = f.Before.Path
			}
			if f.After != nil {
				after = f.After.Path
			}
			fmt.Fprintf(&b, "  [%s] %s → %s\n", f.Status, before, after)
			if f.XMLView == nil && f.Before != nil && f.After != nil && f.Before.XMLSHA256 != "" && f.Before.XMLSHA256 == f.After.XMLSHA256 {
				b.WriteString("    XML 结构相同，原始文件字节不同\n")
			}
			if f.XMLView != nil {
				fmt.Fprintf(&b, "    XML 比较：%s\n", xmlViewKind(f.XMLView.Kind))
				if f.XMLView.Note != "" {
					fmt.Fprintf(&b, "    %s\n", f.XMLView.Note)
				}
				if f.XMLView.Coarse {
					b.WriteString("    XML 行修改较多，已回退为完整变化段落对照\n")
				}
			}
			for _, side := range []*File{f.Before, f.After} {
				if side != nil && side.XMLNote != "" {
					fmt.Fprintf(&b, "    %s: %s\n", side.Path, side.XMLNote)
				}
			}
			for _, v := range f.XMLChanges {
				fmt.Fprintf(&b, "    %s: %s → %s\n", v.Path, quoted(v.Before), quoted(v.After))
			}
			if f.XMLTruncated {
				b.WriteString("    XML 差异明细达到条数上限，其余明细省略\n")
			}
		}
		for _, p := range c.Pages {
			if output.err != nil {
				return output.err
			}
			if p.Match.Method != "" {
				fmt.Fprintf(&b, "  %s %d → %d：%s\n", v.localizer.Message("compare.page_match.heading", nil), p.Before, p.After, v.pageMatchDescription(p.Match))
				if p.Match.Method == "sequence" {
					b.WriteString("    按顺序兜底配对，请核对页面对应关系；必要时使用手工页映射。\n")
				}
			}
			if p.Status == "unchanged" && !p.Moved {
				continue
			}
			fmt.Fprintf(&b, "  页面 %d → %d [%s] 页码变化=%v 文字变化=%v 尺寸变化=%v\n", p.Before, p.After, p.Status, p.Moved, p.TextChanged, p.SizeChanged)
			if p.TextChanged || p.Before == 0 || p.After == 0 {
				before, after := pageText(r.Inputs[r.Baseline-1], p.Before), pageText(r.Inputs[c.Target-1], p.After)
				fmt.Fprintf(&b, "    原文: %q\n    新文: %q\n", before, after)
				for _, span := range p.TextDiff {
					switch span.Kind {
					case "delete":
						fmt.Fprintf(&b, "    - %q\n", span.Text)
					case "insert":
						fmt.Fprintf(&b, "    + %q\n", span.Text)
					}
				}
				if p.TextDiffCoarse {
					b.WriteString("    文字修改较多，已回退为完整变化段落对照\n")
				}
			}
			if p.Visual != nil {
				fmt.Fprintf(&b, "    变化像素 %d/%d (%.4f%%)，范围 %v\n", p.Visual.ChangedPixels, p.Visual.TotalPixels, p.Visual.Ratio*100, p.Visual.Bounds)
				for i, region := range p.Visual.Regions {
					fmt.Fprintf(&b, "    区域 %d: %v，变化像素 %d\n", i+1, region.Bounds, region.ChangedPixels)
				}
				if p.Visual.IgnoredRegionPixels > 0 {
					fmt.Fprintf(&b, "    小区域过滤像素：%d（原始变化统计保留）\n", p.Visual.IgnoredRegionPixels)
				}
				if p.Visual.RegionsTruncated {
					fmt.Fprintf(&b, "    共 %d 个区域，仅展示变化像素最多的 %d 个\n", p.Visual.RegionCount, len(p.Visual.Regions))
				}
			}
		}
	}
	if v.markdown {
		// 明细按原文展示，避免文件名、XML 或文字被解释为 Markdown 标记。
		output.WriteString("</pre>\n")
	}
	return output.err
}

// 文本报告直接流式写入预算检查器，不额外积累整份报告或转义副本。
type reportTextWriter struct {
	w   io.Writer
	err error
}

func (w *reportTextWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.w.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (w *reportTextWriter) WriteString(value string) (int, error) { return w.Write([]byte(value)) }

type escapedReportWriter struct{ w *reportTextWriter }

func (w escapedReportWriter) Write(p []byte) (int, error) {
	template.HTMLEscape(w.w, p)
	if w.w.err != nil {
		return 0, w.w.err
	}
	return len(p), nil
}

func quoted(s *string) string {
	if s == nil {
		return "∅"
	}
	return strconv.Quote(*s)
}
func pageText(input Input, n int) string {
	if n <= 0 || n > len(input.Pages) {
		return ""
	}
	return input.Pages[n-1].Text
}

func pageScope(spec string) string {
	if strings.TrimSpace(spec) == "" {
		return "全部"
	}
	return spec
}

func selectedPageCount(input Input) int {
	count := 0
	for _, p := range input.Pages {
		if p.Selected {
			count++
		}
	}
	return count
}

type fileListRow struct {
	FileChange

	DetailID string
}

// 完整清单按路径对齐，并复用已识别的移动关系，确保每侧文件只出现一次。
func fileListRows(base, target Input, comparison Comparison) []fileListRow {
	afterByPath := make(map[string]*File, len(target.Files))
	for i := range target.Files {
		f := &target.Files[i]
		afterByPath[f.Path] = f
	}
	moves := map[string]string{}
	details := map[string]string{}
	for i, change := range comparison.Files {
		details[changePath(change)] = fmt.Sprintf("file-%d-%d", comparison.Target, i)
		if change.Status == "moved" && change.Before != nil && change.After != nil {
			moves[change.Before.Path] = change.After.Path
		}
	}
	used := map[string]bool{}
	rows := make([]fileListRow, 0, len(base.Files)+len(target.Files))
	appendRow := func(change FileChange) {
		row := fileListRow{FileChange: change}
		if change.Status != "unchanged" {
			row.DetailID = details[changePath(change)]
		}
		rows = append(rows, row)
	}
	for i := range base.Files {
		before := &base.Files[i]
		after := afterByPath[before.Path]
		status := "unchanged"
		if after == nil {
			after = afterByPath[moves[before.Path]]
			if after == nil {
				status = "removed"
			} else {
				status = "moved"
			}
		} else if before.SHA256 != after.SHA256 {
			status = "modified"
		}
		if after != nil {
			used[after.Path] = true
		}
		appendRow(FileChange{Status: status, Before: before, After: after})
	}
	for i := range target.Files {
		after := &target.Files[i]
		if !used[after.Path] {
			appendRow(FileChange{Status: "added", After: after})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return changePath(rows[i].FileChange) < changePath(rows[j].FileChange) })
	return rows
}

func (v reportView) writeHTML(w io.Writer) error {
	r := v.report
	t, err := template.New("compare").Funcs(template.FuncMap{
		"input": func(n int) Input {
			if n <= 0 || n > len(r.Inputs) {
				return Input{}
			}
			return r.Inputs[n-1]
		},
		"text":             pageText,
		"scope":            pageScope,
		"selectedCount":    selectedPageCount,
		"fileRows":         fileListRows,
		"xmlViewKind":      xmlViewKind,
		"matchDescription": v.pageMatchDescription,
		"selectedNumbers": func(input Input) string {
			numbers := make([]string, 0, len(input.Pages))
			for _, p := range input.Pages {
				if p.Selected {
					numbers = append(numbers, strconv.Itoa(p.Number))
				}
			}
			return strings.Join(numbers, ",")
		},
		"fileKind": func(f *File) string {
			if f == nil {
				return ""
			}
			if f.Kind != "" {
				return f.Kind
			}
			return fileKind(f.Path)
		},
		"inc": func(v int) int { return v + 1 },
		"sub": func(a, b int) int { return a - b },
		// 样式与脚本来自随程序嵌入的源码，不包含输入文档中的字符串。
		"style":   func() template.CSS { return template.CSS(reportCSS) },
		"script":  func() template.JS { return template.JS(reportJS) },
		"value":   quoted,
		"path":    changePath,
		"percent": func(v float64) string { return fmt.Sprintf("%.3f%%", v*100) },
		"imageURL": func(uri string) template.URL {
			if relativeImageURL(uri) {
				return template.URL(uri)
			}
			if !strings.HasPrefix(uri, "data:image/png;base64,") {
				return ""
			}
			if _, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.TrimPrefix(uri, "data:image/png;base64,")))); err != nil {
				return ""
			}
			return template.URL(uri)
		},
		"status": func(s string) string {
			switch s {
			case "added":
				return "新增"
			case "removed":
				return "删除"
			case "modified":
				return "修改"
			case "moved":
				return "移动"
			default:
				return "相同"
			}
		},
	}).Parse(reportHTML)
	if err != nil {
		return err
	}
	return t.Execute(w, r)
}

func (v reportView) pageMatchDescription(info PageMatchInfo) string {
	id := "compare.page_match.unknown"
	switch info.Method {
	case "manual":
		id = "compare.page_match.manual"
	case "content":
		id = "compare.page_match.content"
	case "text":
		id = "compare.page_match.text"
	case "text_similarity":
		id = "compare.page_match.text_similarity"
	case "sequence":
		id = "compare.page_match.sequence"
	case "number":
		id = "compare.page_match.number"
	case "unmatched":
		id = "compare.page_match.unmatched"
	}
	label := v.localizer.Message(id, map[string]string{"Similarity": fmt.Sprintf("%.1f", info.TextSimilarity*100)})
	if info.SearchLimited {
		label += v.localizer.Message("compare.page_match.limited", nil)
	}
	return label
}

func xmlViewKind(kind string) string {
	switch kind {
	case "format":
		return "仅格式变化（XML 结构相同）"
	case "structure":
		return "XML 结构或内容变化"
	case "added":
		return "新增 XML"
	case "removed":
		return "删除 XML"
	default:
		return "XML 结构分析不可用"
	}
}
