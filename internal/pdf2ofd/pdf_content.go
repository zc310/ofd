package pdf2ofd

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
)

type pdfName string
type pdfString []byte

type pdfContentTokenizer struct {
	data []byte
	pos  int
}

func newPDFContentTokenizer(data []byte) *pdfContentTokenizer {
	return &pdfContentTokenizer{data: data}
}
func (t *pdfContentTokenizer) next() (any, bool, error) {
	t.skipSpace()
	if t.pos >= len(t.data) {
		return nil, false, nil
	}
	switch t.data[t.pos] {
	case '/':
		value, err := t.name()
		return value, true, err
	case '(':
		value, err := t.literal()
		return pdfString(value), true, err
	case '<':
		if t.pos+1 < len(t.data) && t.data[t.pos+1] == '<' {
			value, err := t.dict()
			return value, true, err
		}
		value, err := t.hex()
		return pdfString(value), true, err
	case '[':
		value, err := t.array()
		return value, true, err
	case ']':
		t.pos++
		return t.next()
	default:
		token := t.word()
		if token == "" {
			t.pos++
			return t.next()
		}
		if number, err := strconv.ParseFloat(token, 64); err == nil {
			return number, true, nil
		}
		return token, true, nil
	}
}
func (t *pdfContentTokenizer) skipSpace() {
	for t.pos < len(t.data) {
		switch t.data[t.pos] {
		case ' ', '\t', '\r', '\n', '\f', 0:
			t.pos++
		case '%':
			for t.pos < len(t.data) && t.data[t.pos] != '\n' && t.data[t.pos] != '\r' {
				t.pos++
			}
		default:
			return
		}
	}
}
func (t *pdfContentTokenizer) word() string {
	start := t.pos
	for t.pos < len(t.data) {
		c := t.data[t.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '[' || c == ']' || c == '(' || c == ')' || c == '<' || c == '>' || c == '/' {
			break
		}
		t.pos++
	}
	return string(t.data[start:t.pos])
}
func (t *pdfContentTokenizer) name() (pdfName, error) {
	t.pos++
	start := t.pos
	var result bytes.Buffer
	for t.pos < len(t.data) {
		c := t.data[t.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '[' || c == ']' || c == '(' || c == ')' || c == '<' || c == '>' || c == '/' {
			break
		}
		if c == '#' && t.pos+2 < len(t.data) {
			value, err := hex.DecodeString(string(t.data[t.pos+1 : t.pos+3]))
			if err != nil {
				return "", err
			}
			result.Write(value)
			t.pos += 3
		} else {
			result.WriteByte(c)
			t.pos++
		}
	}
	if result.Len() == 0 {
		return pdfName(string(t.data[start:t.pos])), nil
	}
	return pdfName(result.String()), nil
}
func (t *pdfContentTokenizer) literal() ([]byte, error) {
	t.pos++
	depth := 1
	var result bytes.Buffer
	for t.pos < len(t.data) {
		c := t.data[t.pos]
		t.pos++
		if c == '\\' && t.pos < len(t.data) {
			escaped := t.data[t.pos]
			t.pos++
			switch escaped {
			case 'n':
				result.WriteByte('\n')
			case 'r':
				result.WriteByte('\r')
			case 't':
				result.WriteByte('\t')
			case 'b':
				result.WriteByte('\b')
			case 'f':
				result.WriteByte('\f')
			case '(', ')', '\\':
				result.WriteByte(escaped)
			default:
				if escaped >= '0' && escaped <= '7' {
					value := escaped - '0'
					for i := 0; i < 2 && t.pos < len(t.data) && t.data[t.pos] >= '0' && t.data[t.pos] <= '7'; i++ {
						value = value*8 + t.data[t.pos] - '0'
						t.pos++
					}
					result.WriteByte(value)
				} else {
					result.WriteByte(escaped)
				}
			}
			continue
		}
		if c == '(' {
			depth++
			result.WriteByte(c)
			continue
		}
		if c == ')' {
			depth--
			if depth == 0 {
				return result.Bytes(), nil
			}
			result.WriteByte(c)
			continue
		}
		result.WriteByte(c)
	}
	return nil, fmt.Errorf("未闭合的 PDF 字符串")
}
func (t *pdfContentTokenizer) hex() ([]byte, error) {
	t.pos++
	start := t.pos
	for t.pos < len(t.data) && t.data[t.pos] != '>' {
		t.pos++
	}
	if t.pos >= len(t.data) {
		return nil, fmt.Errorf("未闭合的 PDF 十六进制字符串")
	}
	raw := bytes.Map(func(c rune) rune {
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			return -1
		}
		return c
	}, t.data[start:t.pos])
	t.pos++
	if len(raw)%2 == 1 {
		raw = append(raw, '0')
	}
	result, err := hex.DecodeString(string(raw))
	return result, err
}

// inlineImage 解析 BI 之后的内联图像字典与数据，直到 EI。
// 调用前 "BI" 已被消费；返回后位置停在 "EI" 之后。
func (t *pdfContentTokenizer) inlineImage() (map[string]any, []byte, error) {
	dict := map[string]any{}
	for {
		t.skipSpace()
		if t.pos >= len(t.data) {
			return nil, nil, fmt.Errorf("未结束的 PDF 内联图像")
		}
		if t.data[t.pos] == 'I' && t.pos+1 < len(t.data) && t.data[t.pos+1] == 'D' && (t.pos+2 >= len(t.data) || isPDFWhitespace(t.data[t.pos+2])) {
			t.pos += 2
			break
		}
		key, ok, err := t.next()
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, fmt.Errorf("未结束的 PDF 内联图像")
		}
		name, ok := key.(pdfName)
		if !ok {
			return nil, nil, fmt.Errorf("PDF 内联图像字典键无效")
		}
		value, ok, err := t.next()
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, fmt.Errorf("PDF 内联图像字典缺少值")
		}
		dict[string(name)] = value
	}
	// ID 之后恰好一个空白字节，然后才是图像数据。
	if t.pos < len(t.data) {
		if t.data[t.pos] == '\r' && t.pos+1 < len(t.data) && t.data[t.pos+1] == '\n' {
			t.pos += 2
		} else if isPDFWhitespace(t.data[t.pos]) {
			t.pos++
		}
	}
	start := t.pos
	for t.pos < len(t.data) {
		if t.data[t.pos] == 'E' && t.pos+1 < len(t.data) && t.data[t.pos+1] == 'I' && t.pos > start && isPDFWhitespace(t.data[t.pos-1]) {
			after := t.pos + 2
			if after >= len(t.data) || isPDFWhitespace(t.data[after]) || isPDFDelimiter(t.data[after]) {
				data := append([]byte(nil), t.data[start:t.pos-1]...)
				t.pos = after
				return dict, data, nil
			}
		}
		t.pos++
	}
	return nil, nil, fmt.Errorf("未结束的 PDF 内联图像数据")
}

func isPDFWhitespace(c byte) bool {
	switch c {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	default:
		return false
	}
}

func isPDFDelimiter(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return false
	}
}

// dict 解析内容流中的 << ... >> 字典，主要用于内联图像的 DecodeParms。
func (t *pdfContentTokenizer) dict() (map[string]any, error) {
	t.pos += 2
	result := map[string]any{}
	for {
		t.skipSpace()
		if t.pos >= len(t.data) {
			return nil, fmt.Errorf("未闭合的 PDF 字典")
		}
		if t.data[t.pos] == '>' && t.pos+1 < len(t.data) && t.data[t.pos+1] == '>' {
			t.pos += 2
			return result, nil
		}
		key, ok, err := t.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("未闭合的 PDF 字典")
		}
		name, ok := key.(pdfName)
		if !ok {
			return nil, fmt.Errorf("PDF 字典键无效")
		}
		value, ok, err := t.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("PDF 字典缺少值")
		}
		result[string(name)] = value
	}
}

func (t *pdfContentTokenizer) array() ([]any, error) {
	t.pos++
	result := []any{}
	for {
		t.skipSpace()
		if t.pos >= len(t.data) {
			return nil, fmt.Errorf("未闭合的 PDF 数组")
		}
		if t.data[t.pos] == ']' {
			t.pos++
			return result, nil
		}
		value, ok, err := t.next()
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, value)
		}
	}
}
