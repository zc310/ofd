package compare

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestHTMLPageMapEditorValidatesExportsAndKeepsTargetsIndependent(t *testing.T) {
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
	a := testOFD(t, "skip", "Alpha", "Beta", "Gamma")
	b := testOFD(t, "skip", "insert", "Alpha", "Beta", "Gamma")
	c := testOFD(t, "skip", "Alpha", "Beta", "Gamma")
	r, err := CompareFiles(t.Context(), []string{b, a, c}, Options{Baseline: 1, Pages: "2-4"})
	if err != nil {
		t.Fatal(err)
	}
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
	click := func(selector string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool]("document.querySelector("+strconv.Quote(selector)+").scrollIntoView({block:'center',behavior:'instant'});true")); err != nil {
			t.Fatal(err)
		}
		if _, err := chromedp.Run(ctx, chromedp.Click(chromedp.CSS(selector))); err != nil {
			t.Fatal(err)
		}
	}
	eval := func(script string) {
		t.Helper()
		if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](script)); err != nil {
			t.Fatal(err)
		}
	}
	check := func(script string) {
		t.Helper()
		if ok, err := chromedp.Run(ctx, chromedp.Poll[bool](script)); err != nil || !ok {
			t.Fatalf("页映射交互失败：%v %v", ok, err)
		}
	}
	click("#target-1 .page-map-editor > summary")
	check(`document.querySelector('#target-1 .page-map-output').value==='--page-match auto --pages "2-4" --page-map "2:3,3:4"'`)
	eval(`(()=>{
      const editor=document.querySelector('#target-1 .page-map-editor');
      const field=before=>editor.querySelector('.page-map-row[data-before="'+before+'"] .page-map-after');
      const output=editor.querySelector('.page-map-output');
      const assert=(condition,message)=>{if(!condition)throw Error(message);};
      const set=(before,value)=>{field(before).value=value;field(before).dispatchEvent(new Event('input',{bubbles:true}));};
      assert(editor.querySelectorAll('.page-map-row').length===3 && !field(1),'编辑器未保留原始选页范围');
      set(2,'4');
      assert(field(2).getAttribute('aria-invalid')==='true' && field(3).getAttribute('aria-invalid')==='true','重复目标页未同时标错');
      assert(output.value==='' && editor.querySelector('.page-map-copy').disabled && editor.querySelector('.page-map-download').disabled,'错误映射仍可导出');
      set(3,'2');set(4,'3');
      assert(output.value.endsWith('--page-map "2:4,3:2,4:3"'),'修正映射后未恢复导出');
      const original=document.querySelector('#target-1 .page h3').textContent;
      assert(original.includes('基准页 2') && original.includes('当前页 3'),'编辑草稿改变了原报告结果');
      for(const value of ['1','5','0','-1','1.5','2e0','NaN','9007199254740992']) {
        set(4,value);assert(field(4).getAttribute('aria-invalid')==='true' && output.value==='' && editor.querySelector('.page-map-download').disabled,'无效目标页被接受 '+value);
      }
      set(4,'');assert(output.value.endsWith('--page-map "2:4,3:2"'),'清空一行未交回自动匹配');
      editor.dataset.pages='2-4";bad';set(4,'');assert(output.value==='' && editor.querySelector('.page-map-copy').disabled,'无效范围被拼接到导出参数');
      editor.dataset.pages='2-4';set(4,'');
      Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.copiedPageMap=text;}}});
      return true;
    })()`)
	click("#target-1 .page-map-copy")
	check(`window.copiedPageMap==='--page-match auto --pages "2-4" --page-map "2:4,3:2"' && document.querySelector('#target-1 .page-map-message').textContent.includes('已复制')`)
	// 捕获下载的 Blob，确认文件内容与可复制参数一致，避免在测试中写入下载目录。
	eval(`(()=>{
      URL.createObjectURL=blob=>{window.pageMapBlob=blob;return 'blob:page-map-test';};
      URL.revokeObjectURL=()=>{};
      HTMLAnchorElement.prototype.click=function(){window.pageMapDownloadName=this.download;};
      return true;
    })()`)
	click("#target-1 .page-map-download")
	download, err := chromedp.Run(ctx, chromedp.Evaluate[string](`window.pageMapBlob.text()`, chromedp.EvalAwaitPromise))
	if err != nil || download != "--page-match auto --pages \"2-4\" --page-map \"2:4,3:2\"\n" {
		t.Fatalf("下载参数错误：%q %v", download, err)
	}
	check(`window.pageMapDownloadName==='ofd-page-map-target-1.txt'`)
	_, spec, ok := strings.Cut(strings.TrimSpace(download), `--page-map "`)
	if !ok {
		t.Fatal("缺少 --page-map")
	}
	spec = strings.TrimSuffix(spec, `"`)
	if _, err := ParsePageMap(spec); err != nil {
		t.Fatal(err)
	}
	remapped, err := CompareFiles(t.Context(), []string{a, b}, Options{Pages: "2-4", PageMap: spec})
	if err != nil {
		t.Fatal(err)
	}
	if p := remapped.Comparisons[0].Pages; p[0].After != 4 || p[1].After != 2 || p[0].Match.Method != "manual" || p[1].Match.Method != "manual" {
		t.Fatalf("导出参数重新运行未生效：%+v", p)
	}
	eval(`(()=>{
      const selector=document.getElementById('target');selector.value='target-3';selector.dispatchEvent(new Event('change'));
      document.querySelector('#target-3 .page-map-editor').open=true;return true;
    })()`)
	check(`document.querySelector('#target-3 .page-map-output').value.endsWith('--page-map "2:2,3:3,4:4"')`)
	eval(`(()=>{const selector=document.getElementById('target');selector.value='target-1';selector.dispatchEvent(new Event('change'));return true;})()`)
	check(`document.querySelector('#target-1 .page-map-output').value.endsWith('--page-map "2:4,3:2"')`)
	// 剪贴板不可用时保留参数选区，支持离线手动复制。
	eval(`(()=>{
      Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async()=>{throw Error('unavailable');}}});
      document.execCommand=()=>false;return true;
    })()`)
	click("#target-1 .page-map-copy")
	check(`(()=>{const output=document.querySelector('#target-1 .page-map-output');return document.activeElement===output && output.selectionStart===0 && output.selectionEnd===output.value.length && document.querySelector('#target-1 .page-map-message').textContent.includes('参数已选中');})()`)
	// 异步复制完成时若草稿已变化，不覆盖新的校验结果。
	eval(`(()=>{Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:()=>new Promise(resolve=>window.finishPageMapCopy=resolve)}});return true;})()`)
	click("#target-1 .page-map-copy")
	eval(`(()=>{const editor=document.querySelector('#target-1 .page-map-editor');const input=editor.querySelector('.page-map-after');input.value='0';input.dispatchEvent(new Event('input',{bubbles:true}));window.finishPageMapCopy();return true;})()`)
	check(`!document.querySelector('#target-1 .page-map-errors').classList.contains('hidden') && document.querySelector('#target-1 .page-map-message').textContent===''`)
	click("#target-1 .page-map-reset")
	check(`document.querySelector('#target-1 .page-map-output').value.endsWith('--page-map "2:3,3:4"') && document.querySelectorAll('#target-1 .page-map-after[aria-invalid="true"]').length===0`)
	click("#target-1 .page-map-clear")
	check(`document.querySelector('#target-1 .page-map-output').value==='' && document.querySelector('#target-1 .page-map-copy').disabled && document.querySelector('#target-1 .page-map-download').disabled && document.querySelector('#target-1 .page-map-message').textContent.includes('全部自动匹配')`)
	click("#target-1 .page-map-reset")
	check(`document.querySelector('#target-1 .page-map-output').value.endsWith('--page-map "2:3,3:4"')`)
	for _, width := range []int64{1280, 640, 360} {
		if _, err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900)); err != nil {
			t.Fatal(err)
		}
		check(`document.documentElement.scrollWidth<=innerWidth+1 && document.querySelector('#target-1 .page-map-output').getBoundingClientRect().right<=innerWidth+1`)
	}
}
