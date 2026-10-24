package xmlcodec

import (
	"bytes"
	"strings"
)

func readProcInst(raw []byte) (target, instruction string, size int, ok bool) {
	end := bytes.Index(raw[2:], []byte("?>"))
	if end < 0 {
		return "", "", 0, false
	}
	end += 2
	body := raw[2:end]
	nameEnd := 0
	for nameEnd < len(body) && !xmlSpace(body[nameEnd]) {
		nameEnd++
	}
	if !xmlName(body[:nameEnd], false) {
		return "", "", 0, false
	}
	start := nameEnd
	for start < len(body) && xmlSpace(body[start]) {
		start++
	}
	return string(body[:nameEnd]), xmlLines(body[start:]), end + 2, true
}

// XML 1.0 §2.8/§4.3.3：声明中的属性有固定顺序，可使用任意 XML 空白和单双引号。
func xmlDeclaration(instruction string) bool {
	stage := 0
	for len(instruction) > 0 {
		instruction = strings.TrimLeft(instruction, " \t\r\n")
		if instruction == "" {
			break
		}
		index := strings.IndexAny(instruction, "= \t\r\n")
		if index < 0 {
			return false
		}
		name := instruction[:index]
		rest := strings.TrimLeft(instruction[index:], " \t\r\n")
		if len(rest) == 0 || rest[0] != '=' {
			return false
		}
		rest = strings.TrimLeft(rest[1:], " \t\r\n")
		if len(rest) < 2 || rest[0] != '\'' && rest[0] != '"' {
			return false
		}
		end := strings.IndexByte(rest[1:], rest[0])
		if end < 0 {
			return false
		}
		value := rest[1 : end+1]
		switch name {
		case "version":
			if stage != 0 || value != "1.0" {
				return false
			}
			stage = 1
		case "encoding":
			if stage != 1 || !strings.EqualFold(value, "UTF-8") {
				return false
			}
			stage = 2
		case "standalone":
			if stage < 1 || stage > 2 || value != "yes" && value != "no" {
				return false
			}
			stage = 3
		default:
			return false
		}
		instruction = rest[end+2:]
		if len(instruction) > 0 && !xmlSpace(instruction[0]) {
			return false
		}
	}
	return stage > 0
}
