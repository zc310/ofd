package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
	"github.com/zc310/ofd/cmd/ofd-wasm/web"
	viewerlocalization "github.com/zc310/ofd/internal/localization/viewer"
)

//go:embed localization.js
var localizationScript string

//go:embed locales/en.json
var englishMessages []byte

var hanPhrase = regexp.MustCompile(`[\p{Han}]+`)
var messages = sync.OnceValue(func() map[string]string { //nolint:gochecknoglobals // 只读内嵌词典仅解码一次。
	var result map[string]string
	if err := json.Unmarshal(englishMessages, &result); err != nil {
		panic(err)
	}
	return result
})
var englishLocalizer = sync.OnceValue(func() *viewerlocalization.Localizer { //nolint:gochecknoglobals // 复用旧版只读词典。
	localizer, err := viewerlocalization.New("en")
	if err != nil {
		panic(err)
	}
	return localizer
})

var errUnsupportedLanguage = errors.New("不支持的界面语言")

func translateUI(source string) string {
	if text, ok := messages()[source]; ok {
		return text
	}
	if text := englishLocalizer().Message(source, nil); text != source {
		return text
	}
	return hanPhrase.ReplaceAllStringFunc(source, func(phrase string) string {
		if text, ok := messages()[phrase]; ok {
			return text
		}
		return englishLocalizer().Message(phrase, nil)
	})
}

func (d *Desktop) loadLanguage() {
	d.language = "zh"
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	d.languageFile = filepath.Join(dir, "ofd-viewer-wails", "language")
	data, err := os.ReadFile(d.languageFile)
	if err == nil && strings.TrimSpace(string(data)) == "en" {
		d.language = "en"
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "读取界面语言失败: %v\n", err)
	}
}

func (d *Desktop) tr(source string) string {
	d.settingsMu.Lock()
	language := d.language
	d.settingsMu.Unlock()
	if language != "en" {
		return source
	}
	return translateUI(source)
}

// SetLanguage 保存明确的语言选择；刷新界面后重新打开当前文件，不保存解锁凭据。
func (d *Desktop) SetLanguage(language string) error {
	if language != "zh" && language != "en" {
		return errUnsupportedLanguage
	}
	d.settingsMu.Lock()
	if d.languageFile != "" {
		if err := saveOpenDirectory(d.languageFile, language); err != nil {
			d.settingsMu.Unlock()
			return err
		}
	}
	d.language = language
	d.settingsMu.Unlock()
	if window := d.mainWindow.Load(); window != nil {
		window.SetTitle(d.tr("OFD 阅读器"))
	}
	d.openMu.Lock()
	defer d.openMu.Unlock()
	d.discardPrint()
	if d.inputPath != "" {
		d.queueLaunch(d.inputPath)
	}
	return nil
}

// 在原生端编译英文界面资源，仅翻译源码中的文案常量及模板静态片段。
// 文档文字、文件名、属性、附件内容与模板插值不经过翻译器；也不修改共享源码。
type uiScriptLocalizer struct{}

func (v uiScriptLocalizer) Enter(node js.INode) js.IVisitor { //nolint:ireturn // 实现解析库要求的访问器接口。
	switch node := node.(type) {
	case *js.LiteralExpr:
		if node.TokenType == js.StringToken && len(node.Data) >= 2 {
			var encoded strings.Builder
			var literal string
			if err := node.JSON(&encoded); err != nil || json.Unmarshal([]byte(encoded.String()), &literal) != nil {
				return v
			}
			// 这是引擎取消协议，不是界面文案。
			if literal != "请求已取消" {
				translated := translateUI(literal)
				if literal == "zh-CN" {
					translated = "en"
				}
				if translated != literal {
					if data, err := json.Marshal(translated); err == nil {
						node.Data = data
					}
				}
			}
		}
	case *js.TemplateExpr:
		if translateTemplate(node) {
			break
		}
		for i := range node.List {
			node.List[i].Value = []byte(translateScriptText(string(node.List[i].Value)))
		}
		node.Tail = []byte(translateScriptText(string(node.Tail)))
	}
	return v
}

func (uiScriptLocalizer) Exit(js.INode) {}

var templateSlot = regexp.MustCompile(`\{([0-9]+)\}`)

func translateTemplate(node *js.TemplateExpr) bool {
	if node.Tag != nil || len(node.List) == 0 {
		return false
	}
	var key strings.Builder
	for i, part := range node.List {
		if len(part.Value) < 3 {
			return false
		}
		key.Write(part.Value[1 : len(part.Value)-2])
		fmt.Fprintf(&key, "{%d}", i)
	}
	if len(node.Tail) < 2 {
		return false
	}
	key.Write(node.Tail[1 : len(node.Tail)-1])
	translated, ok := messages()[key.String()]
	if !ok {
		return false
	}
	// 复用原表达式且保持求值顺序，绝不翻译插值结果或重复求值。
	old := node.List
	parts := make([]js.TemplatePart, 0, len(old))
	last := 0
	for _, match := range templateSlot.FindAllStringSubmatchIndex(translated, -1) {
		index := 0
		if _, err := fmt.Sscanf(translated[match[2]:match[3]], "%d", &index); err != nil || index != len(parts) || index >= len(old) {
			return false
		}
		prefix := "}"
		if len(parts) == 0 {
			prefix = "`"
		}
		parts = append(parts, js.TemplatePart{Value: []byte(prefix + escapeTemplateText(translated[last:match[0]]) + "${"), Expr: old[index].Expr})
		last = match[1]
	}
	if len(parts) != len(old) {
		return false
	}
	node.List = parts
	node.Tail = []byte("}" + escapeTemplateText(translated[last:]) + "`")
	return true
}

func escapeTemplateText(text string) string {
	return strings.NewReplacer(`\`, `\\`, "`", "\\`", "${", `\${`).Replace(text)
}

func translateScriptText(source string) string {
	// 只替换汉字片段，保留原来的引号、转义、HTML 和模板边界。
	return hanPhrase.ReplaceAllStringFunc(source, func(phrase string) string {
		text := translateUI(phrase)
		if text == phrase {
			return phrase
		}
		return strings.NewReplacer(`\`, `\u005c`, `'`, `\u0027`, `"`, `\u0022`, "`", `\u0060`, "$", `\u0024`, "\n", `\n`, "\r", `\r`).Replace(text)
	})
}

func localizeJavaScript(source []byte) ([]byte, error) {
	ast, err := js.Parse(parse.NewInputBytes(source), js.Options{})
	if err != nil {
		return nil, fmt.Errorf("编译英文界面: %w", err)
	}
	js.Walk(uiScriptLocalizer{}, ast)
	var result strings.Builder
	ast.JS(&result)
	return []byte(result.String()), nil
}

func (d *Desktop) serveLocalization(w http.ResponseWriter) {
	d.settingsMu.Lock()
	language := d.language
	d.settingsMu.Unlock()
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	encoded, err := json.Marshal(language)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	catalog := make(map[string]string, len(messages()))
	maps.Copy(catalog, messages())
	index, _ := web.Assets.ReadFile("index.html")
	for _, phrase := range hanPhrase.FindAllString(string(index), -1) {
		catalog[phrase] = translateUI(phrase)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = fmt.Fprintf(w, "globalThis.ofdReaderLanguage = %s;\nglobalThis.ofdReaderAndroid = %t;\nglobalThis.ofdReaderMessages = %s;\n%s", encoded, d.Platform().Android, data, localizationScript)
}

func (d *Desktop) serveUIScript(w http.ResponseWriter, name string, source []byte) {
	d.settingsMu.Lock()
	english := d.language == "en"
	d.settingsMu.Unlock()
	if english {
		cached, _ := d.uiScripts.Load(name)
		if cached, ok := cached.([]byte); ok {
			source = cached
		} else {
			data, err := localizeJavaScript(source)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			d.uiScripts.Store(name, data)
			source = data
		}
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(source)
}
