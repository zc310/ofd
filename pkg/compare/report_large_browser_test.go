package compare

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestLargeFileListReviewFlowAndNarrowLayout(t *testing.T) {
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
	base := Input{Name: strings.Repeat("long-directory/", 20) + "base.ofd", Metadata: []DocumentInfo{{ID: strings.Repeat("identifier", 30), Title: strings.Repeat("LongTitle", 30)}}}
	target := Input{Name: strings.Repeat("long-directory/", 20) + "target.ofd", Metadata: base.Metadata}
	for i := 0; i < 5000; i++ {
		f := File{Path: fmt.Sprintf("Doc/Pages/Page_%04d/Content.xml", i), SHA256: "same", Kind: "xml", Size: 100}
		base.Files = append(base.Files, f)
		target.Files = append(target.Files, f)
	}
	target.Files[2500].SHA256 = "changed"
	a := `<Root value="` + strings.Repeat("long-attribute", 100) + `">` + strings.Repeat(`<Same>unchanged</Same>`, 20) + `<Mixed> keep <B/> spaces </Mixed><Value>100</Value></Root>`
	b := strings.Replace(a, ">100<", ">120<", 1)
	changes := []FileChange{{Status: "modified", Before: &base.Files[2500], After: &target.Files[2500], XMLView: xmlViewForTest(t, a, b)}}
	r := &Report{Baseline: 1, Inputs: []Input{base, target, target}, Comparisons: []Comparison{{Target: 2, Files: changes}, {Target: 3, Files: changes}}}
	r.Comparisons[0].Pages = []PageChange{{Status: "modified", Before: 1, After: 1, Match: PageMatchInfo{Method: "sequence", SearchLimited: true}}}
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
	ctx, end := context.WithTimeout(ctx, 45*time.Second)
	defer end()
	if _, err := chromedp.Run(ctx, chromedp.Navigate((&url.URL{Scheme: "file", Path: filename}).String())); err != nil {
		t.Fatal(err)
	}
	metrics, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(()=>{
      const list=document.querySelector('#target-2 .file-list');list.open=true;
      document.querySelector('main > section details').open=true;
      const search=list.querySelector('.file-search');
      const page=document.querySelector('#target-2 .page');
      if(page.querySelector('.page-match').dataset.matchMethod!=='sequence' || !page.querySelector('.match-review').textContent.includes('请核对页面对应关系') || !page.textContent.includes('相似度搜索达到上限'))throw Error('顺序配对或搜索回退未提示');
      const start=performance.now();search.value='PAGE_2500';search.dispatchEvent(new Event('input'));
      const count=list.querySelectorAll('.file-row:not(.hidden)').length;
      const searchMs=performance.now()-start;
      if(count!==1 || list.querySelector('.file-count').textContent!=='显示 1 / 5000 个文件')throw Error('大清单搜索结果错误');
      const row=list.querySelector('.file-row:not(.hidden)');row.querySelector('.file-detail-link').click();
      const detail=document.querySelector('#target-2 .file.active');
      detail.querySelector('.xml-collapse').click();
      if(Array.from(detail.querySelectorAll('.xml-common')).some(el=>el.open))throw Error('复杂 XML 折叠失败');
      detail.querySelector('.xml-expand').click();
      if(Array.from(detail.querySelectorAll('.xml-common')).some(el=>!el.open))throw Error('复杂 XML 展开失败');
      detail.querySelector('.file-back').click();
      if(document.activeElement!==row || search.value!=='PAGE_2500')throw Error('大清单返回定位丢失');
      const selector=document.getElementById('target');selector.value='target-3';selector.dispatchEvent(new Event('change'));
      const other=document.querySelector('#target-3 .file-list');other.open=true;
      if(other.querySelector('.file-search').value!=='' || other.querySelector('.file-count').textContent!=='显示 5000 / 5000 个文件')throw Error('目标筛选状态串用');
      selector.value='target-2';selector.dispatchEvent(new Event('change'));
      if(search.value!=='PAGE_2500')throw Error('切回目标丢失筛选条件');
      return JSON.stringify({rows:5000,searchMs});
    })()`))
	if err != nil {
		t.Fatal(err)
	}
	t.Log(metrics)
	for _, width := range []int64{1280, 640, 360} {
		if _, err := chromedp.Run(ctx, chromedp.EmulateViewport(width, 900)); err != nil {
			t.Fatal(err)
		}
		for _, zoom := range []string{"1", "1.25", "1.5"} {
			if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool]("document.body.style.zoom='"+zoom+"';true")); err != nil {
				t.Fatal(err)
			}
			layout, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(()=>{
              const view=document.querySelector('#target-2 .xml-view');
              const bar=view.querySelector('.xml-view-actions');
              const rects=Array.from(bar.children).map(el=>el.getBoundingClientRect());
              const centers=rects.map(rect=>rect.top+rect.height/2);
              const spread=Math.max(...centers)-Math.min(...centers);
              const overflow=document.documentElement.scrollWidth>innerWidth+1 ? Array.from(document.querySelectorAll('body *')).filter(el=>{const r=el.getBoundingClientRect();return r.width>0 && r.right>innerWidth+1 && !el.closest('.xml-view-actions');}).slice(0,8).map(el=>({tag:el.tagName,class:el.className,right:el.getBoundingClientRect().right,text:el.textContent.slice(0,50)})) : [];
              return JSON.stringify({width:innerWidth,pageWidth:document.documentElement.scrollWidth,centersSpread:spread,toolbarRight:bar.getBoundingClientRect().right,overflow});
            })()`))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("width=%d zoom=%s %s", width, zoom, layout)
			ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(()=>{
              const bar=document.querySelector('#target-2 .xml-view-actions');
              const centers=Array.from(bar.children).map(el=>{const r=el.getBoundingClientRect();return r.top+r.height/2;});
              return document.documentElement.scrollWidth<=innerWidth+1 && Math.max(...centers)-Math.min(...centers)<1 && bar.getBoundingClientRect().right<=innerWidth+1;
            })()`))
			if err != nil || !ok {
				t.Fatalf("大清单或 XML 布局溢出：%v %v", layout, err)
			}
		}
	}
}
