package converter_test

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/convert/browser"
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/eml"
	_ "github.com/zc310/ofd/pkg/converter/import/svg"
	"github.com/zc310/ofd/pkg/validator"
	"github.com/zc310/ofd/pkg/webreader"
)

func browserImportOptions(t *testing.T) []converter.Option {
	t.Helper()
	chrome, err := browser.FindChrome("")
	if err != nil {
		t.Fatal(err)
	}
	if chrome == "" {
		t.Skip("未安装 Chrome/Chromium，跳过浏览器导入集成测试")
	}
	return []converter.Option{converter.WithChrome(chrome), converter.WithChromeNoSandbox(true)}
}

func browserFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少样本 %s", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func browserOFDObjects(t *testing.T, data []byte) map[string]int {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for _, file := range archive.File {
		if !strings.HasSuffix(file.Name, "Content.xml") {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		decoder := xml.NewDecoder(r)
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				r.Close()
				t.Fatal(err)
			}
			if start, ok := token.(xml.StartElement); ok {
				counts[start.Name.Local]++
			}
		}
		r.Close()
	}
	return counts
}

func assertBrowserOFDValid(t *testing.T, data []byte) {
	t.Helper()
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(data), "browser-import.ofd")
	if report.Summary.Errors != 0 || report.Summary.Warnings != 0 {
		t.Fatalf("生成 OFD 严格校验失败: %+v", report)
	}
}

func TestSVGImportKeepsVectorPathsTextAndLandscapePaper(t *testing.T) {
	options := browserImportOptions(t)
	options = append(options, converter.WithLandscape(true))
	source := browserFixture(t, "../../testdata/svg/workflow.svg")
	var output bytes.Buffer
	if err := converter.Convert(t.Context(), "svg", "ofd", source, &output, options...); err != nil {
		t.Fatal(err)
	}
	counts := browserOFDObjects(t, output.Bytes())
	if counts["PathObject"] == 0 || counts["TextObject"] == 0 || counts["ImageObject"] != 0 {
		t.Fatalf("纯矢量 SVG 被栅格化或丢失图元: %v", counts)
	}
	r, err := webreader.Open(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	info, err := r.Page(0)
	// Chrome 输出的纸张边界会取整，允许一个 PDF 点的尺寸差异。
	if err != nil || r.PageCount() != 1 || math.Abs(info.Width-297) > 25.4/72 || math.Abs(info.Height-210) > 25.4/72 {
		t.Fatalf("SVG 纸张或页数不正确: %+v pages=%d error=%v", info, r.PageCount(), err)
	}
	var text bytes.Buffer
	if err := converter.Text(t.Context(), output.Bytes(), &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Import", "Review", "Archive", "文档处理流程"} {
		if !strings.Contains(strings.Join(strings.Fields(text.String()), ""), strings.Join(strings.Fields(want), "")) {
			t.Fatalf("SVG 文字无法提取 %q: %s", want, &text)
		}
	}
	assertBrowserOFDValid(t, output.Bytes())
}

func TestEMLImportKeepsHeadersSelectedBodyAndAttachmentList(t *testing.T) {
	options := browserImportOptions(t)
	source := browserFixture(t, "../../testdata/eml/project-update.eml")
	var output, text bytes.Buffer
	if err := converter.Convert(t.Context(), "eml", "ofd", bytes.NewReader(source), &output, options...); err != nil {
		t.Fatal(err)
	}
	if err := converter.Text(t.Context(), output.Bytes(), &text); err != nil {
		t.Fatal(err)
	}
	// PDF 可按字形拆分文字对象；按字符核对导入内容，不依赖提取器的空格推断。
	normalized := strings.Join(strings.Fields(text.String()), "")
	for _, want := range []string{"项目进展", "team@example.org", "archive@example.org", "+0800", "PROJECT", "资料审核通知", "review-checklist.txt", "17 bytes"} {
		if !strings.Contains(normalized, strings.Join(strings.Fields(want), "")) {
			t.Fatalf("邮件缺少 %q: %s", want, normalized)
		}
	}
	if strings.Count(normalized, "Projectdocumentsarereadyforreview.") != 1 || strings.Contains(normalized, "Reviewchecklist") {
		t.Fatalf("重复输出备选正文或把附件内容当作正文: %s", normalized)
	}
	r, err := webreader.Open(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	attachments, err := r.Attachments()
	if err != nil || len(attachments) != 2 {
		t.Fatalf("未保留邮件附件和 CID 原文件: %+v error=%v", attachments, err)
	}
	for _, info := range attachments {
		if !info.Exists {
			t.Fatalf("附件缺少包内原文件: %+v", info)
		}
		if info.Name == "review-checklist.txt" {
			data, err := r.AttachmentData(info.Scope, info.ID, 0)
			if err != nil || string(data) != "Review checklist\n" || !info.Visible || info.Format != "txt" || !strings.Contains(info.Usage, "text/plain") {
				t.Fatalf("附件不能完整下载或元数据错误: %+v data=%q error=%v", info, data, err)
			}
		} else if info.Name != "project-logo.svg" || info.Visible {
			t.Fatalf("CID 原文件没有作为隐藏附件保留: %+v", info)
		}
	}
	assertBrowserOFDValid(t, output.Bytes())
}

func TestEMLImportRendersEmbeddedPNG(t *testing.T) {
	options := browserImportOptions(t)
	image := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			image.SetRGBA(x, y, color.RGBA{R: 220, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image); err != nil {
		t.Fatal(err)
	}
	source := "Subject: Inline PNG\nContent-Type: multipart/related; boundary=img\n\n" +
		"--img\nContent-Type: text/html\n\n<p>Image</p><img src=\"cid:pixel\" width=\"80\" height=\"80\">\n" +
		"--img\nContent-Type: image/png\nContent-ID: <pixel>\nContent-Transfer-Encoding: base64\n\n" +
		base64.StdEncoding.EncodeToString(encoded.Bytes()) + "\n--img--\n"
	var output bytes.Buffer
	if err := converter.Convert(t.Context(), "eml", "ofd", []byte(source), &output, options...); err != nil {
		t.Fatal(err)
	}
	if counts := browserOFDObjects(t, output.Bytes()); counts["ImageObject"] != 1 {
		t.Fatalf("CID PNG 未进入页面: %v", counts)
	}
	r, err := webreader.Open(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	attachments, err := r.Attachments()
	if err != nil || len(attachments) != 1 || attachments[0].Visible {
		t.Fatalf("CID 图片原文件缺失: %+v error=%v", attachments, err)
	}
	original, err := r.AttachmentData(attachments[0].Scope, attachments[0].ID, 0)
	if err != nil || !bytes.Equal(original, encoded.Bytes()) {
		t.Fatalf("CID 图片原文件被重新编码: %v", err)
	}
}

func TestEMLImportPreservesDuplicateNamesBinaryAndEmptyAttachments(t *testing.T) {
	options := browserImportOptions(t)
	payloads := [][]byte{{0, 1, 0xFF, 0xFE, '\n'}, []byte("second file"), {}}
	var source strings.Builder
	source.WriteString("Subject: Original attachments\nContent-Type: multipart/mixed; boundary=files\n\n--files\nContent-Type: text/plain\n\nBody\n")
	for index, payload := range payloads {
		name := "../../same.bin"
		if index == 2 {
			name = "empty.txt"
		}
		source.WriteString("--files\nContent-Type: application/octet-stream\nContent-Disposition: attachment; filename=\"" + name + "\"\nContent-Transfer-Encoding: base64\n\n" + base64.StdEncoding.EncodeToString(payload) + "\n")
	}
	source.WriteString("--files--\n")
	var output bytes.Buffer
	if err := converter.Convert(t.Context(), "eml", "ofd", []byte(source.String()), &output, options...); err != nil {
		t.Fatal(err)
	}
	r, err := webreader.Open(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	attachments, err := r.Attachments()
	if err != nil || len(attachments) != len(payloads) {
		t.Fatalf("同名附件被覆盖或空附件丢失: %+v error=%v", attachments, err)
	}
	ids := make(map[string]bool)
	for index, attachment := range attachments {
		data, err := r.AttachmentData(attachment.Scope, attachment.ID, 0)
		if err != nil || !attachment.Exists || !attachment.Visible || ids[attachment.ID] || !bytes.Equal(data, payloads[index]) {
			t.Fatalf("附件未原样保留: index=%d attachment=%+v data=%x error=%v", index, attachment, data, err)
		}
		ids[attachment.ID] = true
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if strings.Contains(file.Name, "..") || strings.Contains(file.Name, "same.bin") {
			t.Fatalf("邮件文件名被用作包内路径: %s", file.Name)
		}
	}
	assertBrowserOFDValid(t, output.Bytes())
}

func TestBrowserDocumentPDFBypassesOFDAndOFDHonorsPDFLimits(t *testing.T) {
	options := browserImportOptions(t)
	options = append(options, converter.WithPDFImportOptions(converter.PDFImportOptions{Limits: converter.PDFImportLimits{MaxInputBytes: 1}}))
	for _, tc := range []struct{ format, input string }{
		{"eml", "Subject: Minimal\n\nEmail body"},
		{"svg", `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="60"><text x="5" y="30">Vector</text></svg>`},
	} {
		var output bytes.Buffer
		if err := converter.Convert(t.Context(), tc.format, "pdf", []byte(tc.input), &output, options...); err != nil || !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
			t.Fatalf("%s→PDF 未使用直接转换器: %v", tc.format, err)
		}
		output.Reset()
		if err := converter.Convert(t.Context(), tc.format, "ofd", []byte(tc.input), &output, options...); !errors.Is(err, converter.ErrPDFImportLimitExceeded) || output.Len() != 0 {
			t.Fatalf("%s→OFD 未传递 PDF 预算: %v bytes=%d", tc.format, err, output.Len())
		}
	}
}
