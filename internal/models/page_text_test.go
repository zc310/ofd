package models

import (
	"strings"
	"testing"

	txml "github.com/tdewolff/parse/v2/xml"
	"github.com/zc310/ofd/internal/fastxml"
)

// bufferedTextCodeValue 用优化前的累积方式读取正文，验证单段延迟解码的语义。
func bufferedTextCodeValue(data []byte) (string, error) {
	p := newPageLexer(data)
	p.Next()
	if _, err := p.SkipAttrs(); err != nil {
		return "", err
	}
	var text []byte
	for {
		tt, buf, start := p.Next()
		switch tt {
		case txml.ErrorToken:
			return "", p.Err()
		case txml.TextToken:
			text = fastxml.AppendUnescape(text, buf)
		case txml.CDATAToken:
			text = append(text, p.CData()...)
		case txml.StartTagToken:
			if _, err := p.SpanElement(start); err != nil {
				return "", err
			}
		case txml.EndTagToken:
			return decodeTextEscapes(string(text)), nil
		}
	}
}

func FuzzTextCodeMatchesBufferedParser(f *testing.F) {
	for _, body := range []string{
		"", "正文", "甲&amp;乙", "<![CDATA[&lt;]]>",
		"甲&amp;<![CDATA[&lt;]]><!--split-->&#x1F600;",
		`\D83D<!--split--><![CDATA[\DE00]]>`,
		"&am<!--split-->p;", "<![CDATA[]]>正文", "甲<Unknown>ignored</Unknown>",
		strings.Repeat("正文&amp;", 100),
	} {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body string) {
		data := []byte("<TextCode>" + body + "</TextCode>")
		want, wantErr := bufferedTextCodeValue(data)
		p := newPageLexer(data)
		p.Next()
		got := TextCode{Value: "unchanged"}
		gotErr := p.parseTextCode(&got)
		if (gotErr == nil) != (wantErr == nil) || gotErr != nil && gotErr.Error() != wantErr.Error() {
			t.Fatalf("正文 %q: got %v, want %v", body, gotErr, wantErr)
		}
		if gotErr == nil && got.Value != want {
			t.Fatalf("正文 %q: got %q, want %q", body, got.Value, want)
		}
		if gotErr != nil && got.Value != "unchanged" {
			t.Fatal("解析失败修改了正文")
		}
	})
}
