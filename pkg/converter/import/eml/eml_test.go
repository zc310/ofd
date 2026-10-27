package eml

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func prepareHTML(input any, conv *converter.Converter) ([]byte, error) {
	document, err := prepareDocument(input, conv)
	return document.HTML, err
}

func TestPrepareEmailDecodesHeadersAndPlainText(t *testing.T) {
	text, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("中文正文\n<script>只是文字</script>"))
	if err != nil {
		t.Fatal(err)
	}
	input := "From: " + mime.BEncoding.Encode("utf-8", "项目组") + " <team@example.org>\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", "进度 & 通知") + "\r\n" +
		"To: reader@example.org\r\nDate: Tue, 27 Oct 2026 09:00:00 +0800\r\n" +
		"Content-Type: text/plain; charset=gb18030\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString(text)
	output, err := prepareHTML([]byte(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"项目组", "team@example.org", "进度 &amp; 通知", "中文正文", "&lt;script&gt;只是文字&lt;/script&gt;", "&#43;0800"} {
		if !bytes.Contains(output, []byte(value)) {
			t.Fatalf("邮件头或正文缺少 %q: %s", value, output)
		}
	}
}

// 正文在图片之前出现，须先完成所有 MIME 部分解析再解析 CID；附件文本不可混入正文。
func TestPrepareEmailChoosesHTMLResolvesCIDAndListsAttachments(t *testing.T) {
	input := "Subject: MIME test\nContent-Type: multipart/mixed; boundary=outer\n\n" +
		"--outer\nContent-Type: multipart/related; boundary=related; start=\"<root>\"\n\n" +
		"--related\nContent-Type: image/png\nContent-ID: <logo@example.org>\nContent-Disposition: inline; filename=logo.png\nContent-Transfer-Encoding: base64\n\naGVsbG8=\n" +
		"--related\nContent-ID: <root>\nContent-Type: multipart/alternative; boundary=alternative\n\n" +
		"--alternative\nContent-Type: text/plain\n\nPLAIN_ALTERNATIVE\n" +
		"--alternative\nContent-Type: text/html; charset=utf-8\nContent-Transfer-Encoding: quoted-printable\n\n" +
		"<html><head><style>body{display:none}</style></head><body><p style=3D\"color:red\">HTML_BODY</p><img src=3D\"cid:logo%40example.org\"><script>BAD_SCRIPT</script></body></html>\n" +
		"--alternative--\n--related--\n" +
		"--outer\nContent-Type: text/plain\nContent-Disposition: attachment; filename*=utf-8''%E6%B8%85%E5%8D%95.txt\n\nATTACHMENT_SECRET\n--outer--\n"
	output, err := prepareHTML(strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"HTML_BODY", "color:red", "data:image/png;base64,aGVsbG8=", "清单.txt", "Attachments"} {
		if !bytes.Contains(output, []byte(value)) {
			t.Fatalf("HTML 邮件缺少 %q: %s", value, output)
		}
	}
	for _, value := range []string{"PLAIN_ALTERNATIVE", "ATTACHMENT_SECRET", "BAD_SCRIPT", "body{display:none}", "<li>logo.png"} {
		if bytes.Contains(output, []byte(value)) {
			t.Fatalf("不应输出的内容 %q 进入邮件: %s", value, output)
		}
	}
}

func TestEmailBodyRemovesDocumentControlsAndHonorsRemoteOption(t *testing.T) {
	input := []byte("Subject: Render\nContent-Type: text/html; charset=utf-8\n\n" +
		`<p onclick="bad()">Content</p><img src="https://example.org/logo.png"><img src="file:///tmp/private.png"><a href="javascript:bad()">bad</a><a href="https://example.org/">link</a><iframe src="file:///tmp/private"></iframe><form>FORM_CONTENT</form>`)
	for _, remote := range []bool{false, true} {
		conv := &converter.Converter{}
		converter.WithAllowRemoteResources(remote)(conv)
		output, err := prepareHTML(input, conv)
		if err != nil {
			t.Fatal(err)
		}
		for _, unwanted := range []string{"onclick=", "javascript:", "file:///", "<iframe", "FORM_CONTENT"} {
			if bytes.Contains(output, []byte(unwanted)) {
				t.Fatalf("正文保留了 %q", unwanted)
			}
		}
		if bytes.Contains(output, []byte(`src="https://example.org/logo.png"`)) != remote {
			t.Fatalf("remote=%t 没有生效: %s", remote, output)
		}
		if !bytes.Contains(output, []byte(`href="https://example.org/"`)) {
			t.Fatal("普通链接丢失")
		}
	}
}

func TestEmailRejectsMalformedMIMEAndMissingInlineContent(t *testing.T) {
	for _, input := range []string{
		"not a message", "Content-Type: multipart/mixed\n\nbody",
		"Content-Type: multipart/mixed; boundary=x\n\n--x\nContent-Type: text/plain\n\nmissing end",
		"Content-Type: text/plain; charset=unknown\n\nbody",
		"Content-Transfer-Encoding: base64\n\n!!not base64!!",
		"Content-Transfer-Encoding: unsupported\n\nbody",
		"Content-Type: multipart/encrypted; boundary=x\n\n--x--\n",
		"Content-Type: application/pkcs7-mime\n\nencrypted",
		"Content-Type: text/html\n\n<img src=\"cid:missing\">",
		"Content-Type: multipart/related; boundary=x; start=\"<missing>\"\n\n--x\n\nbody\n--x--\n",
	} {
		if _, err := prepareHTML([]byte(input), nil); err == nil {
			t.Fatalf("无效邮件未报错: %q", input)
		}
	}
	var input strings.Builder
	input.WriteString("Content-Type: multipart/mixed; boundary=x\n\n")
	for range 257 {
		input.WriteString("--x\nContent-Type: text/plain\n\nx\n")
	}
	input.WriteString("--x--\n")
	if _, err := prepareHTML([]byte(input.String()), nil); err == nil {
		t.Fatal("未执行 MIME 部分数量限制")
	}
	var nested string = "Content-Type: text/plain\n\nbody\n"
	for index := range 18 {
		boundary := fmt.Sprintf("nested%d", index)
		nested = "Content-Type: multipart/mixed; boundary=" + boundary + "\n\n--" + boundary + "\n" + nested + "--" + boundary + "--\n"
	}
	if _, err := prepareHTML([]byte(nested), nil); err == nil {
		t.Fatal("未执行 MIME 嵌套上限")
	}
}

func TestEmailReaderCancellationIsPreserved(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader := &cancelReader{cancel: cancel}
	var output bytes.Buffer
	if err := converter.Convert(ctx, "eml", "ofd", reader, &output); err != context.Canceled || output.Len() != 0 {
		t.Fatalf("取消处理错误: %v bytes=%d", err, output.Len())
	}
	if _, err := prepareHTML(123, nil); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("无效输入类型未报错: %v", err)
	}
}

type cancelReader struct{ cancel context.CancelFunc }

func (r *cancelReader) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, "Subject: test\n\nbody"), io.EOF
}
