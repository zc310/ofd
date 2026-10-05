package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSchemaWritesMarkdownToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runSchema(nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("退出码 = %d，stderr: %s", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{
		"# ofd-creator manifest 字段参考",
		"## Manifest",
		"## Item",
		"| 字段 | 类型 | 可省略 | 说明 |",
		"`templates[].items[]`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("输出缺少 %q", want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr 非空: %s", stderr.String())
	}
}

// TestRunSchemaTableColumnsAlign 确认表格每行列数一致：说明里的竖线若未转义，
// 会让某一行比其它行多出列，把整张表排版打乱。
func TestRunSchemaTableColumnsAlign(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runSchema(nil, &stdout, &stderr); code != exitOK {
		t.Fatalf("退出码 = %d", code)
	}
	// splitOnUnescaped 按未转义的竖线切分，与 Markdown 解析器的处理一致。
	splitOnUnescaped := func(line string) []string {
		var (
			parts   []string
			current strings.Builder
		)
		for index := 0; index < len(line); index++ {
			if line[index] == '\\' && index+1 < len(line) {
				current.WriteByte(line[index])
				current.WriteByte(line[index+1])
				index++
				continue
			}
			if line[index] == '|' {
				parts = append(parts, current.String())
				current.Reset()
				continue
			}
			current.WriteByte(line[index])
		}
		return append(parts, current.String())
	}
	var header []int
	for _, line := range strings.Split(stdout.String(), "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		columns := len(splitOnUnescaped(line))
		if header == nil {
			header = []int{columns}
			continue
		}
		if columns != header[0] {
			t.Fatalf("表格行列数不一致，期望 %d 列，实际 %d 列: %s", header[0], columns, line)
		}
	}
	if header == nil {
		t.Fatal("输出里没有表格")
	}
}

func TestRunSchemaJSONIsStructured(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runSchema([]string{"--format", "json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("退出码 = %d，stderr: %s", code, stderr.String())
	}
	var docs []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Doc   string `json:"doc"`
		Count int    `json:"field_count"`
		List  []struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			OmitEmpty bool   `json:"omitempty"`
			Doc       string `json:"doc"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &docs); err != nil {
		t.Fatalf("JSON 参考无法解析: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("JSON 参考没有结构体")
	}
	names := make(map[string]bool, len(docs))
	for _, item := range docs {
		names[item.Name] = true
	}
	for _, want := range []string{"Manifest", "Page", "Item", "RadialShading", "Color"} {
		if !names[want] {
			t.Fatalf("JSON 参考缺少结构体 %s", want)
		}
	}
	for _, item := range docs {
		if item.Count != len(item.List) {
			t.Fatalf("%s 的 field_count = %d，实际 %d 个字段", item.Name, item.Count, len(item.List))
		}
	}
}

func TestRunSchemaWritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "schema.md")
	var stdout, stderr bytes.Buffer
	if code := runSchema([]string{"-o", path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("退出码 = %d，stderr: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("写入文件时不应写标准输出，实际 %q", stdout.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	if !strings.Contains(string(data), "## Manifest") {
		t.Fatal("输出文件缺少 Manifest 节")
	}
}

// TestRunSchemaDashWritesStdout 确认 - 与省略 --output 都写标准输出。
func TestRunSchemaDashWritesStdout(t *testing.T) {
	for _, args := range [][]string{nil, {"-o", "-"}, {"--output", "-"}} {
		var stdout, stderr bytes.Buffer
		if code := runSchema(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("参数 %v 退出码 = %d", args, code)
		}
		if !strings.Contains(stdout.String(), "## Manifest") {
			t.Fatalf("参数 %v 未输出到标准输出", args)
		}
	}
}

func TestRunSchemaRejectsUnknownFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSchema([]string{"--format", "xml"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("退出码 = %d，期望 %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "不支持的参考格式") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunSchemaHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		if code := runSchema(args, &stdout, &stderr); code != exitOK {
			t.Fatalf("参数 %v 退出码 = %d", args, code)
		}
		// 与 export、replace、watermark 保持一致，子命令帮助写到 stderr。
		if !strings.Contains(stderr.String(), "ofd-creator schema") {
			t.Fatalf("帮助缺少标题: %q", stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("帮助不应写标准输出，实际 %q", stdout.String())
		}
	}
}
