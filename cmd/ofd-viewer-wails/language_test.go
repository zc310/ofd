package main

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
	"github.com/zc310/ofd/cmd/ofd-wasm/web"
)

type collectUIChinese struct{ phrases map[string]bool }

func (v collectUIChinese) Enter(node js.INode) js.IVisitor { //nolint:ireturn // 实现解析库要求的访问器接口。
	add := func(data []byte) {
		for _, phrase := range hanPhrase.FindAllString(string(data), -1) {
			v.phrases[phrase] = true
		}
	}
	switch node := node.(type) {
	case *js.LiteralExpr:
		if node.TokenType == js.StringToken {
			add(node.Data)
		}
	case *js.TemplateExpr:
		for _, part := range node.List {
			add(part.Value)
		}
		add(node.Tail)
	}
	return v
}

func (collectUIChinese) Exit(js.INode) {}

func TestLanguagePersistsAndQueuesCurrentFileWithoutCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	d := &Desktop{}
	d.loadLanguage()
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if d.languageFile != filepath.Join(config, "ofd-viewer-wails", "language") {
		t.Fatalf("语言配置路径未使用程序名: %q", d.languageFile)
	}
	if d.tr("关闭") != "关闭" {
		t.Fatal("初始语言不是中文")
	}
	d.inputPath = filepath.Join(t.TempDir(), "当前.ofd")
	if err := d.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if !d.PendingLaunch() || d.tr("关闭") != "Close" {
		t.Fatal("语言切换没有保留当前文件或更新原生文案")
	}
	other := &Desktop{}
	other.loadLanguage()
	if other.language != "en" {
		t.Fatal("重启未恢复语言选择")
	}
	if err := d.SetLanguage("invalid"); err == nil || d.language != "en" {
		t.Fatal("非法语言覆盖了设置")
	}
}

func TestEnglishUIScriptsCompileAndPreserveDocumentData(t *testing.T) {
	node, nodeErr := exec.LookPath("node")
	for _, name := range []string{"viewer.js", "desktop.js", "prototype/native-page.js", "print.js"} {
		var source []byte
		var err error
		switch name {
		case "print.js":
			source = []byte(printScript)
		case "viewer.js":
			source, err = web.Assets.ReadFile(name)
		default:
			source, err = assets.ReadFile(name)
		}
		if err != nil {
			t.Fatal(err)
		}
		localized, err := localizeJavaScript(source)
		if err != nil {
			t.Fatalf("编译 %s: %v", name, err)
		}
		ast, err := js.Parse(parse.NewInputBytes(localized), js.Options{})
		if err != nil {
			t.Fatalf("英文 %s 不可解析: %v", name, err)
		}
		if nodeErr == nil {
			args := []string{"--check"}
			if name != "viewer.js" {
				args = append(args, "--input-type=module")
			}
			check := exec.CommandContext(t.Context(), node, args...)
			check.Stdin = strings.NewReader(string(localized))
			if output, err := check.CombinedOutput(); err != nil {
				t.Fatalf("英文 %s 不符合 JavaScript 语法: %v, %s", name, err, output)
			}
		}
		// 仅允许取消协议、语言自身名称及不能翻译的字体匹配别名保留中文。
		allowed := strings.Fields("请求已取消 简体中文 语言 仿宋 华文仿宋 华文宋体 华文楷体 华文黑体 宋体 得意黑 微软正黑 微软雅黑 思源宋体 思源黑体 方正书宋 方正仿宋 方正小标宋 方正楷体 方正黑体 楷体 等线 黑体")
		phrases := make(map[string]bool)
		js.Walk(collectUIChinese{phrases: phrases}, ast)
		for _, phrase := range allowed {
			delete(phrases, phrase)
		}
		if len(phrases) != 0 {
			t.Errorf("英文 %s 有未翻译文案: %v", name, phrases)
		}
		if name == "viewer.js" && (!strings.Contains(string(localized), "请求已取消") || !strings.Contains(string(localized), "微软雅黑") || !strings.Contains(string(localized), "黑体gb2312")) {
			t.Fatal("界面编译破坏了取消协议或字体匹配常量")
		}
	}
	if nodeErr != nil {
		t.Skip("未安装 Node，跳过英文模板运行检查")
	}
	source := []byte("const name=process.argv[1]; const output = `打开文件：${name}`; if(output !== 'Open file：' + name) throw new Error(output);")
	localized, err := localizeJavaScript(source)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), node, "--input-type=module", "-e", string(localized), "打开文件 <script>用户内容</script>").CombinedOutput(); err != nil {
		t.Fatalf("模板插值被改动: %v, %s", err, output)
	}
}

func TestLanguageSwitchDoesNotReopenClosedDocument(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	if err := d.Close(opened.PagePrefix); err != nil {
		t.Fatal(err)
	}
	if err := d.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if d.PendingLaunch() {
		t.Fatal("切换语言不能重新打开已经关闭的文档")
	}
}

func TestTemplateTranslationsPreserveExpressionOrderAndEvaluateOnce(t *testing.T) {
	for source, translated := range messages() {
		slots := templateSlot.FindAllString(source, -1)
		if len(slots) == 0 {
			continue
		}
		if fmt.Sprint(slots) != fmt.Sprint(templateSlot.FindAllString(translated, -1)) {
			t.Errorf("模板插值顺序被改变: %q -> %q", source, translated)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未安装 Node，跳过模板求值检查")
	}
	localized, err := localizeJavaScript([]byte("let n=0; const label=`第 ${++n} 页`; if(label !== 'Page 1' || n !== 1) throw new Error(label);"))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), node, "-e", string(localized)).CombinedOutput(); err != nil {
		t.Fatalf("模板表达式求值次数或页码格式错误: %v, %s", err, output)
	}
}

func TestLocalizationAssetDoesNotCacheLanguageOrContainLocalPaths(t *testing.T) {
	d := &Desktop{language: "en", inputPath: "/private/document.ofd"}
	w := httptest.NewRecorder()
	d.serveLocalization(w)
	if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `ofdReaderLanguage = "en"`) || strings.Contains(w.Body.String(), d.inputPath) {
		t.Fatal("本地化资源缓存错误或泄露文件路径")
	}
}
