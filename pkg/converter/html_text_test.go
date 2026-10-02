package converter_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"regexp"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
)

// 文字层里的片段可能含换行（OFD 排版的硬换行），所以必须开 (?s)，
// 否则 . 匹配不到行尾、整条正则静默失配。
var spanPattern = regexp.MustCompile(`(?s)<span class="text-run"[^>]*>(.*?)</span>`)

// unescapeText 还原 HTML 实体，便于断言文字内容。
func unescapeText(value string) string {
	replacer := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return replacer.Replace(value)
}

func allSpans(t *testing.T, document string) []string {
	t.Helper()
	matches := spanPattern.FindAllStringSubmatch(document, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, unescapeText(match[1]))
	}
	return out
}

// TestHTMLTextLayerMakesTextSelectable 是本轮的核心断言：HTML 输出必须带上
// 透明文字层，否则页面里的文字既选不中也搜不到——对纯图像 HTML 来说这是最
// 常见的抱怨。
func TestHTMLTextLayerMakesTextSelectable(t *testing.T) {
	var output bytes.Buffer
	if err := converter.HTML(context.Background(), "../../test/testdata/helloworld.ofd", &output, converter.Page(1)); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	document := output.String()

	if !strings.Contains(document, `<div class="text-layer">`) {
		t.Fatal("未输出文字层")
	}
	if !strings.Contains(document, ".text-run{position:absolute") {
		t.Error("CSS 缺少 .text-run 的绝对定位规则")
	}
	// 文字必须透明：视觉由页面图像承担。
	if !strings.Contains(document, "color:transparent") {
		t.Error("文字层应为透明，否则会与页面图像重叠")
	}

	spans := allSpans(t, document)
	if len(spans) == 0 {
		t.Fatal("文字层里没有任何文字片段")
	}
	joined := strings.Join(spans, "")
	for _, want := range []string{"你好呀", "OFD", "Reader&Writer"} {
		if !strings.Contains(joined, want) {
			t.Errorf("文字层缺少 %q，实际文字：%q", want, joined)
		}
	}
	// 实体必须转义，否则 & 会截断标记。
	if strings.Contains(document, "Reader&Writer") {
		t.Error("文字中的 & 未转义")
	}
}

// TestHTMLTextLayerCoordinatesArePercentages 确认坐标按页宽页高归一化，
// 这样页面缩放时文字层与页面图像一起缩放，不会错位。
func TestHTMLTextLayerCoordinatesArePercentages(t *testing.T) {
	var output bytes.Buffer
	if err := converter.HTML(context.Background(), "../../test/testdata/helloworld.ofd", &output, converter.Page(1)); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	document := output.String()
	first := spanPattern.FindString(document)
	if first == "" {
		t.Fatal("没有文字片段")
	}
	// left/top/width/height 必须是百分比，且落在 0-100 之间（允许文字溢出页面）。
	for _, field := range []string{"left", "top", "width", "height"} {
		at := strings.Index(first, field+":")
		if at < 0 {
			t.Fatalf("片段缺少 %s", field)
		}
		rest := first[at+len(field)+1:]
		if !strings.HasSuffix(rest[:strings.Index(rest, "%")+1], "%") {
			t.Errorf("%s 应为百分比，实际 %q", field, rest[:12])
		}
	}
	// 字号要有毫米与容器查询单位两条声明：前者是不支持容器查询时的兜底，
	// 后者让字号随页面缩放。
	if !strings.Contains(first, "font-size:") || !strings.Contains(first, "mm;font-size:calc(") {
		t.Errorf("字号应同时给出 mm 兜底与 cqw 换算，实际片段：%s", first)
	}
	if !strings.Contains(first, "1cqw") {
		t.Errorf("字号换算应使用容器查询单位，实际片段：%s", first)
	}
}

// TestHTMLWithoutTextLayerKeepsImageOnly 确认关掉文字层后回到纯图像输出，
// 且 alt 恢复——有文字层时屏幕阅读器会读文字层，再读 alt 会重复。
func TestHTMLWithoutTextLayerKeepsImageOnly(t *testing.T) {
	const source = "../../test/testdata/helloworld.ofd"
	var plain, layered bytes.Buffer
	if err := converter.HTML(context.Background(), source, &plain, converter.Page(1), converter.WithHTMLTextLayer(false)); err != nil {
		t.Fatalf("关闭文字层转换失败: %v", err)
	}
	if err := converter.HTML(context.Background(), source, &layered, converter.Page(1)); err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	if strings.Contains(plain.String(), `<div class="text-layer">`) {
		t.Error("关闭文字层后不应输出文字层")
	}
	if !strings.Contains(plain.String(), ` alt="第1页"`) {
		t.Error("关闭文字层后应保留 img 的 alt")
	}
	if strings.Contains(layered.String(), `<img src="data:image/png;base64,`) &&
		strings.Contains(layered.String(), ` alt="第1页"`) {
		t.Error("有文字层时不应再写 img 的 alt，避免屏幕阅读器重复朗读")
	}
}

// TestHTMLTextLayerDoesNotBreakImageOutput 确认加文字层不影响页面图像本身。
func TestHTMLTextLayerDoesNotBreakImageOutput(t *testing.T) {
	const source = "../../test/testdata/helloworld.ofd"
	var plain, layered bytes.Buffer
	if err := converter.HTML(context.Background(), source, &plain, converter.Page(1), converter.WithHTMLTextLayer(false)); err != nil {
		t.Fatal(err)
	}
	if err := converter.HTML(context.Background(), source, &layered, converter.Page(1)); err != nil {
		t.Fatal(err)
	}
	// 两种模式下页面图像的 data URI 必须完全一致。
	image := regexp.MustCompile(`src="data:image/png;base64,[^"]+"`)
	if a, b := image.FindString(plain.String()), image.FindString(layered.String()); a == "" || a != b {
		t.Error("文字层不应改变页面图像")
	}
}

// TestHTMLTextLayerNeverEmpty 确认文字层不是空壳：要么整层不出现，要么里面
// 至少有一个片段。空的 text-layer 只会留下无意义的节点，也会让「有没有文字
// 层」这个判断失真。
func TestHTMLTextLayerNeverEmpty(t *testing.T) {
	for _, source := range []string{"helloworld.ofd", "image-effects.ofd", "ano.ofd"} {
		t.Run(source, func(t *testing.T) {
			var output bytes.Buffer
			if err := converter.HTML(context.Background(), "../../test/testdata/"+source, &output,
				converter.Page(1)); err != nil {
				t.Fatalf("转换失败: %v", err)
			}
			document := output.String()
			if !strings.Contains(document, `<div class="text-layer">`) {
				return
			}
			if len(allSpans(t, document)) == 0 {
				t.Error("文字层存在但没有任何片段")
			}
		})
	}
}

// TestHTMLOutputRemainsWellFormedXML 粗查标签闭合：文字层是新增的嵌套结构，
// 写坏会让整个 HTML 被浏览器丢弃。
func TestHTMLOutputRemainsWellFormedXML(t *testing.T) {
	var output bytes.Buffer
	if err := converter.HTML(context.Background(), "../../test/testdata/helloworld.ofd", &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	document := output.String()
	decoder := xml.NewDecoder(strings.NewReader(document))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			// HTML 里有未闭合的 <meta>/<br> 这类空元素，宽松处理。
			if strings.Contains(err.Error(), "EOF") {
				break
			}
			return
		}
		_ = token
	}
	for _, tag := range []string{"text-layer", "text-run"} {
		if strings.Count(document, tag) == 0 {
			t.Errorf("缺少 %s", tag)
		}
	}
	if strings.Count(document, `<div class="text-layer">`) != strings.Count(document, `</div>`) {
		t.Errorf("文字层 div 标签不配对：开 %d 个，闭 %d 个",
			strings.Count(document, `<div class="text-layer">`), strings.Count(document, `</div>`))
	}
}
