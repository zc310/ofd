package compare

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestXMLButtonsControlShortAndLongEqualBlocksWithVisibleLayoutChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("短测试跳过浏览器交互")
	}
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		chrome, err = exec.LookPath("chromium")
	}
	if err != nil {
		t.Skip("未安装 Chrome 或 Chromium")
	}
	before, after := &File{Path: "short.xml", Kind: "xml"}, &File{Path: "short.xml", Kind: "xml"}
	shortA, shortB := `<Root><Value>100</Value></Root>`, `<Root><Value>120</Value></Root>`
	longA := `<Root>` + strings.Repeat(`<Same>unchanged</Same>`, 15) + `<Value>100</Value></Root>`
	longB := strings.Replace(longA, ">100<", ">120<", 1)
	changes := []FileChange{
		{Status: "modified", Before: before, After: after, XMLView: xmlViewForTest(t, shortA, shortB)},
		{Status: "modified", Before: before, After: after, XMLView: xmlViewForTest(t, longA, longB)},
		{Status: "added", After: after, XMLView: xmlViewForTest(t, "", shortB)},
	}
	r := &Report{Baseline: 1, Inputs: []Input{{Name: "base.ofd"}, {Name: "target.ofd"}, {Name: "other.ofd"}}, Comparisons: []Comparison{{Target: 2, Files: changes}, {Target: 3, Files: changes}}}
	var html bytes.Buffer
	if err := WriteReport(&html, r, "html"); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(filename, html.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome), chromedp.NoSandbox, chromedp.Flag("disable-dev-shm-usage", true))
	alloc, stop := chromedp.NewExecAllocator(t.Context(), opts...)
	defer stop()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, end := context.WithTimeout(ctx, 30*time.Second)
	defer end()
	if _, err := chromedp.Run(ctx, chromedp.Navigate((&url.URL{Scheme: "file", Path: filename}).String())); err != nil {
		t.Fatal(err)
	}
	_, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`(()=>{
      document.querySelectorAll('#target-2 .file').forEach(el=>el.open=true);
      const short=document.querySelector('#file-2-0 .xml-view'),long=document.querySelector('#file-2-1 .xml-view'),none=document.querySelector('#file-2-2 .xml-view');
      const assert=(value,message)=>{if(!value)throw Error(message);};
      assert(short.querySelectorAll('.xml-common').length>0 && !short.querySelector('[data-default-folded="true"]'),'短片段测试条件无效');
      assert(short.querySelector('.xml-expand').disabled && !short.querySelector('.xml-collapse').disabled,'短片段按钮初始状态错误');
      assert(!long.querySelector('.xml-expand').disabled && !long.querySelector('.xml-collapse').disabled,'长片段初始状态错误');
      assert(none.querySelector('.xml-expand').disabled && none.querySelector('.xml-collapse').disabled && none.querySelector('.xml-fold-status').textContent==='没有相同行可折叠','无相同行时未提示');
      window.xmlShortExpandedHeight=short.getBoundingClientRect().height;
      return true;
    })()`))
	if err != nil {
		t.Fatal(err)
	}
	click := func(selector string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Click(chromedp.CSS(selector))); err != nil {
			t.Fatal(err)
		}
	}
	check := func(expression string) {
		t.Helper()
		if ok, err := chromedp.Run(ctx, chromedp.Poll[bool](expression)); err != nil || !ok {
			t.Fatalf("XML 按钮或布局验证失败：%v %v", ok, err)
		}
	}
	// 使用实际鼠标事件，验证可见布局确实收缩，变化行仍保持显示。
	click("#file-2-0 .xml-collapse")
	check(`(()=>{
      const view=document.querySelector('#file-2-0 .xml-view');
      return Array.from(view.querySelectorAll('.xml-common')).every(el=>!el.open) &&
        view.querySelector('.xml-collapse').disabled && !view.querySelector('.xml-expand').disabled &&
        view.getBoundingClientRect().height<window.xmlShortExpandedHeight-5 &&
        view.querySelector('.xml-changed').getBoundingClientRect().height>0 &&
        document.querySelector('#file-3-0 .xml-expand').disabled;
    })()`)
	click("#file-2-0 .xml-expand")
	check(`(()=>{
      const view=document.querySelector('#file-2-0 .xml-view');
      return Array.from(view.querySelectorAll('.xml-common')).every(el=>el.open) &&
        view.querySelector('.xml-expand').disabled && !view.querySelector('.xml-collapse').disabled &&
        Math.abs(view.getBoundingClientRect().height-window.xmlShortExpandedHeight)<1;
    })()`)
	// 单独切换一个片段后，按钮状态也应跟随更新。
	click("#file-2-0 .xml-common > summary")
	check(`!document.querySelector('#file-2-0 .xml-expand').disabled && !document.querySelector('#file-2-0 .xml-collapse').disabled`)
	click("#file-2-1 .xml-expand")
	check(`Array.from(document.querySelectorAll('#file-2-1 .xml-common')).every(el=>el.open) && document.querySelector('#file-2-1 .xml-expand').disabled`)
	click("#file-2-1 .xml-collapse")
	check(`Array.from(document.querySelectorAll('#file-2-1 .xml-common')).every(el=>!el.open) && document.querySelector('#file-2-1 .xml-collapse').disabled`)
}
