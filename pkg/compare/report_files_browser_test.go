package compare

import (
	"bytes"
	"context"
	"image"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestFileListFiltersAndDetailNavigationStayWithinTarget(t *testing.T) {
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
	base := Input{Name: "before.ofd", Files: []File{
		{Path: "Same.xml", SHA256: "same"},
		{Path: `Doc/修改["x"].xml`, SHA256: "old"},
		{Path: "removed.dat", SHA256: "removed"},
		{Path: "old/image.png", SHA256: "image"},
		{Path: "font.ttf", SHA256: "font"},
		{Path: `<script>alert('x')</script>.xml`, SHA256: "escaped"},
	}}
	target := Input{Name: "after.ofd", Files: []File{
		base.Files[0],
		{Path: base.Files[1].Path, SHA256: "new"},
		{Path: "new/image.dat", SHA256: "image"},
		{Path: "added.xml", SHA256: "added"},
		{Path: "font.ttf", SHA256: "new-font"},
		{Path: base.Files[5].Path, SHA256: "new-escaped"},
	}}
	before, after := "100", "120"
	preview, err := encodePreview(t.Context(), image.NewRGBA(image.Rect(0, 0, 2, 2)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	changes := []FileChange{
		{Status: "modified", Before: &base.Files[1], After: &target.Files[1], XMLChanges: []ValueChange{{Path: "/Amount", Before: &before, After: &after}}},
		{Status: "removed", Before: &base.Files[2]},
		{Status: "moved", Before: &base.Files[3], After: &target.Files[2], BeforePreview: preview, AfterPreview: preview},
		{Status: "added", After: &target.Files[3]},
		{Status: "modified", Before: &base.Files[4], After: &target.Files[4]},
		{Status: "modified", Before: &base.Files[5], After: &target.Files[5]},
	}
	xmlBefore := `<Root>` + strings.Repeat(`<Same>unchanged</Same>`, 15) + `<Amount>100</Amount><TextCode>&lt;script&gt;alert(1)&lt;/script&gt;</TextCode></Root>`
	xmlAfter := strings.Replace(xmlBefore, ">100<", ">120<", 1)
	changes[0].XMLView = xmlViewForTest(t, xmlBefore, xmlAfter)
	report := &Report{Baseline: 1, Inputs: []Input{base, target, target}, Comparisons: []Comparison{{Target: 2, Files: changes}, {Target: 3, Files: changes}}}
	var html bytes.Buffer
	if err := WriteReport(&html, report, "html"); err != nil {
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
      const section=document.getElementById('target-2');
      const list=section.querySelector('.file-list');list.open=true;
      const search=list.querySelector('.file-search'),changed=list.querySelector('.files-changed-only');
      const status=list.querySelector('.file-status'),type=list.querySelector('.file-type');
      const rows=()=>Array.from(list.querySelectorAll('.file-row:not(.hidden)'));
      const set=(control,value)=>{if(control.type==='checkbox')control.checked=value;else control.value=value;control.dispatchEvent(new Event(control===search?'input':'change'));};
      const assert=(condition,message)=>{if(!condition)throw Error(message);};
      const reset=()=>list.querySelector('.file-filter-reset').click();
      assert(rows().length===7 && list.querySelector('.file-count').textContent==='显示 7 / 7 个文件','初始计数错误');
      assert(!rows().find(row=>row.dataset.status==='unchanged').querySelector('a'),'相同文件不应跳转');
      set(changed,true);assert(rows().length===6,'只看不同错误');
      set(type,'xml');set(status,'modified');assert(rows().length===2,'组合筛选错误');
      set(search,'DOC/修改');assert(rows().length===1,'中文与大小写搜索错误');
      const xmlRow=rows()[0];xmlRow.cells[0].click();
      const xmlDetail=document.getElementById(xmlRow.querySelector('a').hash.slice(1));
      assert(xmlDetail.open && xmlDetail.classList.contains('active') && document.activeElement===xmlDetail,'XML 详情未展开或未聚焦');
      assert(xmlDetail.textContent.includes('/Amount') && xmlDetail.textContent.includes('120'),'跳转到错误的 XML 详情');
      const xmlView=xmlDetail.querySelector('.xml-view');
      assert(xmlView && xmlView.open && xmlDetail.querySelector('.xml-field-changes').open,'对照和原字段明细不能同时查看');
      const folds=Array.from(xmlView.querySelectorAll('.xml-common'));
      assert(folds.some(el=>el.dataset.defaultFolded==='true') && folds.every(el=>el.open===(el.dataset.defaultFolded!=='true')),'相同行未按默认策略展开或折叠');
      xmlView.querySelector('.xml-expand').click();assert(folds.every(el=>el.open),'无法展开全部相同行');
      xmlView.querySelector('.xml-collapse').click();assert(folds.every(el=>!el.open),'无法折叠相同部分');
      const deleted=xmlView.querySelector('.xml-delete'),inserted=xmlView.querySelector('.xml-insert');
      assert(deleted.textContent.includes('100') && inserted.textContent.includes('120'),'XML 修改行未对齐');
      assert(getComputedStyle(deleted).backgroundColor==='rgb(255, 225, 227)' && getComputedStyle(inserted).backgroundColor==='rgb(213, 244, 223)','XML 修改未标色');
      assert(!xmlView.querySelector('script') && xmlView.textContent.includes('&lt;script&gt;'),'XML 内容未安全转义');
      list.open=false;xmlDetail.querySelector('.file-back').click();
      assert(list.open && document.activeElement===xmlRow && xmlRow.classList.contains('file-returned'),'未返回原文件行');
      assert(search.value==='DOC/修改' && changed.checked && status.value==='modified' && type.value==='xml' && rows().length===1,'返回后丢失筛选条件');
      set(search,'nothing matches');assert(rows().length===0 && !list.querySelector('.file-empty').classList.contains('hidden'),'缺少空结果提示');
      assert(list.querySelector('.file-count').textContent==='显示 0 / 7 个文件','空结果计数错误');
      reset();assert(rows().length===7 && document.activeElement===search,'重置失败');
      for(const [value,count] of [['xml',4],['image',1],['font',1],['binary',2]]) {set(type,value);assert(rows().length===count,'类型筛选错误 '+value);}
      reset();
      for(const [value,count] of [['unchanged',1],['modified',3],['added',1],['removed',1],['moved',1]]) {set(status,value);assert(rows().length===count,'状态筛选错误 '+JSON.stringify({value,count,actual:rows().length,search:search.value,type:type.value,status:status.value,changed:changed.checked,options:status.outerHTML}));}
      set(status,'unchanged');set(changed,true);assert(rows().length===0,'条件应同时满足');
      reset();set(search,'OLD/IMAGE');assert(rows().length===1,'移动前路径不可搜索');
      set(search,'new/image');assert(rows().length===1,'移动后路径不可搜索');
      const moved=rows()[0];moved.cells[1].click();
      const imageDetail=document.getElementById(moved.querySelector('a').hash.slice(1));
      assert(imageDetail.open && imageDetail.querySelectorAll('.preview').length===2 && !xmlDetail.classList.contains('active'),'资源详情未正确定位');
      reset();set(search,'<script>');assert(rows().length===1,'特殊字符路径搜索失败');
      rows()[0].querySelector('a').click();
      assert(section.querySelector('.file.active').textContent.includes("<script>alert('x')</script>.xml"),'特殊字符路径详情未正确定位');
      const selector=document.getElementById('target');selector.value='target-3';selector.dispatchEvent(new Event('change'));
      const other=document.getElementById('target-3').querySelector('.file-list');other.open=true;
      assert(other.querySelector('.file-search').value==='' && other.querySelectorAll('.file-row:not(.hidden)').length===7,'目标之间共享了筛选状态');
      const otherRow=other.querySelector('.file-row[data-status="added"]');otherRow.querySelector('a').click();
      assert(document.querySelector('.file.active').closest('.comparison').id==='target-3','跳转到了其他目标');
      document.querySelector('.file.active .file-back').click();
      assert(otherRow.classList.contains('file-returned') && document.activeElement===otherRow,'返回到其他目标的清单');
      selector.value='target-2';selector.dispatchEvent(new Event('change'));
      assert(search.value==='<script>' && rows().length===1,'切回目标丢失筛选状态');
      reset();set(status,'added');rows()[0].querySelector('a').focus();
      return true;
    })()`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Enter)); err != nil {
		t.Fatal(err)
	}
	ok, err := chromedp.Run(ctx, chromedp.Poll[bool](`(()=>{
      const section=document.getElementById('target-2');
      const detail=section.querySelector('.file.active');
      const list=section.querySelector('.file-list');
      return detail && detail.open && detail.querySelector('.badge').textContent==='新增' &&
        document.activeElement===detail &&
        list.querySelector('.file-status').value==='added' &&
        list.querySelectorAll('.file-row:not(.hidden)').length===1;
    })()`))
	if err != nil || !ok {
		t.Fatalf("键盘无法打开文件差异：%v %v", ok, err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(()=>{document.querySelector('#target-2 .file.active .file-back').focus();return true;})()`)); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Enter)); err != nil {
		t.Fatal(err)
	}
	ok, err = chromedp.Run(ctx, chromedp.Poll[bool](`(()=>{
      const list=document.querySelector('#target-2 .file-list');
      const row=list.querySelector('.file-returned');
      return list.open && row && row.dataset.status==='added' && document.activeElement===row && list.querySelector('.file-status').value==='added';
    })()`))
	if err != nil || !ok {
		t.Fatalf("键盘无法返回原文件行：%v %v", ok, err)
	}
}
