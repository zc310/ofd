// Package eml 注册 EML 邮件→OFD 与 EML→PDF 转换，依赖 Chrome/Chromium。
// 邮件头、正文、CID 内嵌图片和附件清单先排版为 HTML，再复用浏览器转换链。
//
//	import _ "github.com/zc310/ofd/pkg/converter/import/eml"
//
// 使用 converter.Convert(ctx, "eml", "ofd", input, output)。附件原文件写入 OFD 附件区。
package eml

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	stdhtml "html"
	"html/template"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/internal/convert/htmlbridge"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const maxDecodedBytes = 64 << 20

type attachment struct {
	Name, Type string
	Size       int
}

type body struct {
	markup  string
	isHTML  bool
	present bool
}

type message struct {
	subject     string
	headers     [][2]string
	attachments []attachment
	files       []creator.Attachment
	images      map[string]string
	parts       int
	decoded     int64
}

var headerDecoder = mime.WordDecoder{CharsetReader: charset.NewReaderLabel}

func prepareDocument(input any, conv *converter.Converter) (htmlbridge.Document, error) {
	ctx := conv.Context()
	data, err := htmlbridge.ReadInput(ctx, input, 32<<20)
	if err != nil {
		return htmlbridge.Document{}, err
	}
	parsed, err := mail.ReadMessage(contextio.Reader(ctx, bytes.NewReader(data)))
	if err != nil {
		return htmlbridge.Document{}, fmt.Errorf("EML 邮件解析失败: %w", err)
	}
	m := message{images: make(map[string]string)}
	m.subject, err = decodeHeader(parsed.Header.Get("Subject"))
	if err != nil {
		return htmlbridge.Document{}, err
	}
	if m.subject == "" {
		m.subject = "Email"
	}
	for _, name := range []string{"From", "To", "Cc", "Bcc", "Date", "Reply-To", "Message-ID"} {
		value, err := decodeHeader(strings.Join(parsed.Header[textproto.CanonicalMIMEHeaderKey(name)], ", "))
		if err != nil {
			return htmlbridge.Document{}, err
		}
		if value != "" {
			m.headers = append(m.headers, [2]string{name, value})
		}
	}
	body, err := m.readPart(ctx, textproto.MIMEHeader(parsed.Header), parsed.Body, 0)
	if err != nil {
		return htmlbridge.Document{}, err
	}
	markup, err := m.cleanHTML(ctx, body.markup, conv.AllowRemoteResources())
	if err != nil {
		return htmlbridge.Document{}, err
	}
	if !body.present {
		markup = "<p>No message body.</p>"
	}
	imagesPolicy := "data:"
	if conv.AllowRemoteResources() {
		imagesPolicy += " http: https:"
	}
	var output bytes.Buffer
	err = messageTemplate.Execute(&output, struct {
		Subject     string
		Headers     [][2]string
		Body        template.HTML
		Attachments []attachment
		Policy      string
	}{m.subject, m.headers, template.HTML(markup), m.attachments,
		"default-src 'none'; img-src " + imagesPolicy + "; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'"})
	if ctx.Err() != nil {
		return htmlbridge.Document{}, ctx.Err()
	}
	return htmlbridge.Document{HTML: output.Bytes(), Attachments: m.files}, err
}

func decodeHeader(value string) (string, error) {
	decoded, err := headerDecoder.DecodeHeader(value)
	if err != nil || !utf8.ValidString(decoded) {
		return "", fmt.Errorf("EML 邮件头编码无效: %q (%v)", value, err)
	}
	return decoded, nil
}

// readPart 按 MIME 层次选择正文：alternative 优先 HTML，related 按 start 选择根，
// mixed 依次保留正文部分；带 attachment 处置或文件名的文本不误作为正文。
func (m *message) readPart(ctx context.Context, header textproto.MIMEHeader, reader io.Reader, depth int) (body, error) {
	if err := ctx.Err(); err != nil {
		return body{}, err
	}
	m.parts++
	if depth > 16 || m.parts > 256 {
		return body{}, fmt.Errorf("EML 超过 MIME 上限（16 层或 256 个部分）")
	}
	mediaType, parameters, err := parseMediaType(header.Get("Content-Type"), "text/plain")
	if err != nil {
		return body{}, err
	}
	if mediaType == "multipart/encrypted" || mediaType == "application/pkcs7-mime" || mediaType == "application/x-pkcs7-mime" {
		return body{}, fmt.Errorf("EML 暂不支持加密邮件或 S/MIME 封装，请先导出已解密的邮件")
	}
	disposition, dispositionParams, err := parseMediaType(header.Get("Content-Disposition"), "")
	if err != nil {
		return body{}, err
	}
	filename := dispositionParams["filename"]
	if filename == "" {
		filename = parameters["name"]
	}
	filename, err = decodeHeader(filename)
	if err != nil {
		return body{}, err
	}
	cid := contentID(header.Get("Content-ID"))
	inlineImage := cid != "" && strings.HasPrefix(mediaType, "image/")
	isAttachment := disposition == "attachment" || filename != "" && !inlineImage
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "", "7bit", "8bit", "binary":
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, reader)
	case "quoted-printable":
		reader = quotedprintable.NewReader(reader)
	default:
		return body{}, fmt.Errorf("EML 不支持传输编码 %q", header.Get("Content-Transfer-Encoding"))
	}
	if strings.HasPrefix(mediaType, "multipart/") && !isAttachment {
		boundary := parameters["boundary"]
		if boundary == "" {
			return body{}, fmt.Errorf("EML multipart 缺少 boundary")
		}
		parts := multipart.NewReader(contextio.Reader(ctx, reader), boundary)
		var result body
		start := contentID(parameters["start"])
		rootFound := false
		for {
			part, err := parts.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return body{}, fmt.Errorf("EML MIME 分段失败: %w", err)
			}
			child, err := m.readPart(ctx, part.Header, part, depth+1)
			if err != nil {
				return body{}, err
			}
			switch mediaType {
			case "multipart/alternative":
				if child.present && (!result.isHTML || child.isHTML) {
					result = child
				}
			case "multipart/related":
				if start != "" {
					if contentID(part.Header.Get("Content-ID")) == start {
						result, rootFound = child, true
					}
				} else if !result.present && child.present {
					result = child
				}
			default:
				result.markup += child.markup
				result.present = result.present || child.present
				result.isHTML = result.isHTML || child.isHTML
			}
		}
		if mediaType == "multipart/related" && start != "" && !rootFound {
			return body{}, fmt.Errorf("EML related 找不到 start 指定的正文 %q", start)
		}
		return result, nil
	}
	data, err := io.ReadAll(io.LimitReader(contextio.Reader(ctx, reader), maxDecodedBytes-m.decoded+1))
	if err != nil {
		return body{}, fmt.Errorf("EML 内容解码失败: %w", err)
	}
	m.decoded += int64(len(data))
	if m.decoded > maxDecodedBytes {
		return body{}, fmt.Errorf("EML 解码内容超过 64 MiB")
	}
	if inlineImage {
		if _, exists := m.images[cid]; exists {
			return body{}, fmt.Errorf("EML Content-ID 重复: %q", cid)
		}
		m.images[cid] = "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	if isAttachment || mediaType != "text/plain" && mediaType != "text/html" && !inlineImage {
		if filename == "" {
			filename = fmt.Sprintf("attachment-%d", len(m.attachments)+1)
		}
		m.attachments = append(m.attachments, attachment{filename, mediaType, len(data)})
		m.addFile(filename, mediaType, cid, data, true)
		return body{}, nil
	}
	if inlineImage {
		if filename == "" {
			filename = cid
		}
		m.addFile(filename, mediaType, cid, data, false)
		return body{}, nil
	}
	text, err := decodeText(ctx, data, parameters["charset"])
	if err != nil {
		return body{}, err
	}
	if mediaType == "text/html" {
		return body{markup: text, isHTML: true, present: true}, nil
	}
	return body{markup: "<pre>" + stdhtml.EscapeString(text) + "</pre>", present: true}, nil
}

// addFile 用独立包内文件名保存附件，保留原显示名，包括同名附件和零字节文件。
// CID 文件另存为隐藏附件，正文仍以内嵌图片显示；原始文件可通过附件 API 读取。
func (m *message) addFile(name, mediaType, cid string, data []byte, visible bool) {
	id := fmt.Sprintf("eml-attachment-%03d", len(m.files)+1)
	if strings.TrimSpace(name) == "" {
		name = id
	}
	usage := "EML; Content-Type: " + mediaType
	if cid != "" {
		usage += "; Content-ID: " + cid
	}
	m.files = append(m.files, creator.Attachment{
		ID: id, Name: name,
		Format: strings.TrimPrefix(path.Ext(strings.ReplaceAll(name, "\\", "/")), "."),
		Usage:  usage, Visible: &visible, FileName: id + ".bin", Source: creator.BytesDataSource(data),
	})
}

func parseMediaType(value, fallback string) (string, map[string]string, error) {
	if value == "" {
		return fallback, map[string]string{}, nil
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return "", nil, fmt.Errorf("EML MIME 头无效: %w", err)
	}
	return mediaType, parameters, nil
}

func decodeText(ctx context.Context, data []byte, label string) (string, error) {
	if label == "" || strings.EqualFold(label, "utf-8") {
		if !utf8.Valid(data) {
			return "", fmt.Errorf("EML 正文不是有效 UTF-8，需在 Content-Type 中声明字符集")
		}
		return string(data), nil
	}
	reader, err := charset.NewReaderLabel(label, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("EML 字符集 %q 不支持: %w", label, err)
	}
	decoded, err := io.ReadAll(io.LimitReader(contextio.Reader(ctx, reader), maxDecodedBytes+1))
	if len(decoded) > maxDecodedBytes {
		return "", fmt.Errorf("EML 字符集解码超过 64 MiB")
	}
	return string(decoded), err
}

func contentID(value string) string { return strings.Trim(strings.TrimSpace(value), "<>") }

// cleanHTML 保留正文与内联样式，删除文档级控制、脚本及交互内容；内嵌图片在所有
// MIME 部分解析完成后解析 CID。文档级样式不进入外层，图片仅引用内嵌或允许的网络资源。
func (m *message) cleanHTML(ctx context.Context, markup string, remote bool) (string, error) {
	document, err := html.Parse(contextio.Reader(ctx, strings.NewReader(markup)))
	if err != nil {
		return "", err
	}
	var walk func(*html.Node, int) error
	walk = func(node *html.Node, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 256 {
			return fmt.Errorf("EML HTML 嵌套超过 256 层")
		}
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.ElementNode {
				switch child.Data {
				case "head", "script", "style", "base", "meta", "link", "iframe", "frame", "frameset", "object", "embed", "form", "input", "button", "textarea", "select", "svg", "math", "video", "audio":
					node.RemoveChild(child)
					child = next
					continue
				}
				attributes := child.Attr[:0]
				for _, attr := range child.Attr {
					if attr.Namespace != "" {
						continue
					}
					switch attr.Key {
					case "style", "class", "title", "alt", "width", "height", "align", "valign", "bgcolor", "color", "size", "face", "border", "cellpadding", "cellspacing", "colspan", "rowspan", "dir", "lang":
						attributes = append(attributes, attr)
					case "href":
						if child.Data == "a" && safeLink(attr.Val) {
							attributes = append(attributes, attr)
						}
					case "src":
						if child.Data != "img" {
							continue
						}
						value := strings.TrimSpace(attr.Val)
						if len(value) >= 4 && strings.EqualFold(value[:4], "cid:") {
							id, err := url.PathUnescape(value[4:])
							if err != nil {
								return err
							}
							image, ok := m.images[contentID(id)]
							if !ok {
								return fmt.Errorf("EML 缺少 CID 内嵌图片 %q", id)
							}
							attr.Val = image
							attributes = append(attributes, attr)
						} else if imageURL(value, remote) {
							attr.Val = value
							attributes = append(attributes, attr)
						}
					}
				}
				child.Attr = attributes
			}
			if err := walk(child, depth+1); err != nil {
				return err
			}
			child = next
		}
		return nil
	}
	if err := walk(document, 0); err != nil {
		return "", err
	}
	var output bytes.Buffer
	for root := document.FirstChild; root != nil; root = root.NextSibling {
		if root.Type == html.ElementNode && root.Data == "html" {
			for node := root.FirstChild; node != nil; node = node.NextSibling {
				if node.Type == html.ElementNode && node.Data == "body" {
					for child := node.FirstChild; child != nil; child = child.NextSibling {
						if err := html.Render(&output, child); err != nil {
							return "", err
						}
					}
				}
			}
		}
	}
	return output.String(), nil
}

func safeLink(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto", "tel":
		return true
	}
	return strings.HasPrefix(value, "#")
}

func imageURL(value string, remote bool) bool {
	if strings.HasPrefix(strings.ToLower(value), "data:image/") {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && remote && (u.Scheme == "http" || u.Scheme == "https")
}

var messageTemplate = template.Must(template.New("email").Parse(`<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="{{.Policy}}"><title>{{.Subject}}</title>
<style>@page{margin:15mm}body{font:11pt/1.5 sans-serif;color:#23303e;overflow-wrap:anywhere}
h1{font-size:20pt;margin:0 0 12pt}table.headers{border-collapse:collapse;width:100%;margin-bottom:16pt}
.headers th{text-align:left;vertical-align:top;white-space:nowrap;padding-right:12pt;width:1%}.headers td{white-space:pre-wrap}
.message{border-top:1px solid #ccd3da;padding-top:12pt}.message pre{font:inherit;white-space:pre-wrap}
img{max-width:100%;height:auto}table{max-width:100%}.attachments{margin-top:18pt;border-top:1px solid #ccd3da}
.attachments h2{font-size:13pt}.attachments li{break-inside:avoid}</style></head><body>
<h1>{{.Subject}}</h1><table class="headers">{{range .Headers}}<tr><th>{{index . 0}}</th><td>{{index . 1}}</td></tr>{{end}}</table>
<section class="message">{{.Body}}</section>{{if .Attachments}}<section class="attachments"><h2>Attachments</h2><ul>
{{range .Attachments}}<li>{{.Name}} ({{.Type}}, {{.Size}} bytes)</li>{{end}}</ul></section>{{end}}</body></html>`))

func init() {
	importer := &htmlbridge.Importer{Format: "eml", MIMEType: "message/rfc822", Prepare: prepareDocument}
	converter.RegisterImporter(importer)
	converter.RegisterTransformer(htmlbridge.PDFTransformer{Importer: importer})
}
