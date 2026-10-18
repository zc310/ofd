package compare

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestRegionLoadingSurvivesTargetSwitchAndLatestSelectionWins(t *testing.T) {
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
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprintf("external=%v", external), func(t *testing.T) {
			a, b, c := testOFD(t, "Amount 100"), testOFD(t, "Amount 200"), testOFD(t, "Amount 300")
			dir := t.TempDir()
			options := Options{Visual: true}
			imageIndex := 0
			if external {
				options.ImageWriter = func(data []byte) (string, error) {
					name := fmt.Sprintf("image-%d.png", imageIndex)
					imageIndex++
					return name, os.WriteFile(filepath.Join(dir, name), data, 0600)
				}
			}
			r, err := CompareFiles(t.Context(), []string{a, b, c}, options)
			if err != nil {
				t.Fatal(err)
			}
			// 给首个视图增加另一处裁图位置，区分异步请求先后顺序。
			v := r.Comparisons[0].Pages[0].Visual
			v.Regions = append(v.Regions, VisualRegion{Bounds: [4]int{0, 0, 4, 4}, ChangedPixels: 1})
			var html bytes.Buffer
			if err := WriteReport(&html, r, "html"); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(dir, "report.html")
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
			aligned, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(()=>{
              const section=document.querySelector('.comparison:not(.hidden)');
              section.querySelector('.file-list').open=true;
              const rows=Array.from(section.querySelectorAll('.file-comparison .file-row'));
              const same=rows.find(row=>row.dataset.status==='unchanged');
              const changed=rows.find(row=>row.dataset.status==='modified');
              return same && changed &&
                same.cells[0].querySelector('code').textContent===same.cells[1].querySelector('code').textContent &&
                same.cells[0].getBoundingClientRect().top===same.cells[1].getBoundingClientRect().top &&
                getComputedStyle(changed).color==='rgb(166, 27, 43)' &&
                getComputedStyle(same).color!==getComputedStyle(changed).color;
            })()`))
			if err != nil || !aligned {
				t.Fatalf("清单未对齐或差异未标红：%v %v", aligned, err)
			}
			matching, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(()=>{
              const page=document.querySelector('.comparison:not(.hidden) .page');
              const basis=page.querySelector('.page-match');
              return basis.dataset.matchMethod==='text_similarity' && basis.textContent.includes('文字特征相似度') &&
                page.textContent.includes('不代表页面内容或画面相同');
            })()`))
			if err != nil || !matching {
				t.Fatalf("HTML 页面配对依据未正确展示：%v %v", matching, err)
			}
			_, err = chromedp.Run(ctx, chromedp.Evaluate[bool](`(()=>{
	  const decode=HTMLImageElement.prototype.decode;
	  const gates=[];
	  HTMLImageElement.prototype.decode=function(){
	    const loaded=decode.call(this);
	    return new Promise(resolve=>gates.push(()=>loaded.then(resolve)));
	  };
	  const selector=document.getElementById('target');
	  const page=document.querySelector('.comparison:not(.hidden) .page');
	  const first=page.querySelector('.region-button');
	  first.click();
	  selector.selectedIndex=1;selector.dispatchEvent(new Event('change'));
	  document.querySelector('.comparison:not(.hidden) .region-button').click();
	  selector.selectedIndex=0;selector.dispatchEvent(new Event('change'));
	  // 同一视图的第二次选择必须胜出，其他目标的请求可独立完成。
	  page.querySelector('.region-button:last-child').click();
	  gates.reverse().forEach(release=>release());
	  HTMLImageElement.prototype.decode=decode;
	  return true;
	})()`))
			if err != nil {
				t.Fatal(err)
			}
			ok, err := chromedp.Run(ctx, chromedp.Poll[bool](`Array.from(document.querySelectorAll('.region-view')).every(view=>
	    view.querySelector('.region-position').textContent.startsWith('像素范围') &&
	    view.querySelector('canvas').width>0 && view.querySelector('canvas').height>0) &&
	  document.querySelector('.region-view .region-position').textContent==='像素范围 [0, 0, 4, 4]'`))
			if err != nil || !ok {
				t.Fatalf("切换后局部视图未完成加载: %v %v", ok, err)
			}
		})
	}
}
