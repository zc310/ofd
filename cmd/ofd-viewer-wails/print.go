package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/reader"
)

var errPrintSelection = errors.New("打印请选择 1-200 个不重复页面")
var errPrintExpired = errors.New("打印预览已关闭、过期或文档已切换")
var errAndroidPrint = errors.New("当前 Android 版暂不支持原生打印；文档允许导出时可另存为 PDF 后自行打印")

type printWindow interface {
	Print() error
	Close()
}

type printJob struct {
	token, prefix string
	indices       []int
	pages         []reader.PageInfo
	images        [][]byte
	expires       time.Time
	window        printWindow
	timer         *time.Timer
	maxCopies     int
}

// PrintPages 按打印而非导出权限渲染独立预览，再通过 Wails 调用系统打印对话框。
func (d *Desktop) PrintPages(ctx context.Context, prefix string, indices []int) (bool, error) {
	if runtime.GOOS == "android" {
		return false, errAndroidPrint
	}
	d.openMu.Lock()
	defer d.openMu.Unlock()
	job, err := d.preparePrint(ctx, prefix, indices)
	if err != nil {
		return false, err
	}
	token := job.token
	err = d.publishPrint(ctx, job, func(url string) printWindow {
		window := d.app.Window.NewWithOptions(application.WebviewWindowOptions{Title: d.tr("OFD 打印预览"), Width: 900, Height: 1000, URL: url})
		window.OnWindowEvent(events.Common.WindowClosing, func(_ *application.WindowEvent) { d.releasePrint(token) })
		return window
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// 调用者持有 openMu。窗口可能在创建期间关闭，不能让迟到窗口复活已撤销的任务。
func (d *Desktop) publishPrint(ctx context.Context, job *printJob, create func(string) printWindow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.discardPrint()
	d.printMu.Lock()
	// 有效期从预览发布开始计算，不扣除前面的页面准备时间。
	job.expires = time.Now().Add(5 * time.Minute)
	d.printing = job
	d.printMu.Unlock()
	window := create("/reader-print/" + job.token + "/index.html")
	d.printMu.Lock()
	if err := ctx.Err(); err != nil {
		if d.printing == job {
			d.printing = nil
		}
		d.printMu.Unlock()
		window.Close()
		return err
	}
	if d.printing != job {
		d.printMu.Unlock()
		window.Close()
		return errPrintExpired
	}
	job.window = window
	token := job.token
	job.timer = time.AfterFunc(time.Until(job.expires), func() { d.ClosePrint(token) })
	d.printMu.Unlock()
	return nil
}

func (d *Desktop) preparePrint(ctx context.Context, prefix string, indices []int) (*printJob, error) {
	if len(indices) < 1 || len(indices) > 200 {
		return nil, errPrintSelection
	}
	seen := make(map[int]bool, len(indices))
	for _, index := range indices {
		if index < 0 || seen[index] {
			return nil, errPrintSelection
		}
		seen[index] = true
	}
	if prefix != d.inputPrefix {
		return nil, readersession.ErrStaleDocument
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.document.CheckPermission(prefix, reader.PermissionPrint, indices); err != nil {
		return nil, err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	job := &printJob{token: hex.EncodeToString(token[:]), prefix: prefix, indices: append([]int(nil), indices...), expires: time.Now().Add(5 * time.Minute)}
	permissions, err := d.document.Permissions(prefix)
	if err != nil {
		return nil, err
	}
	for _, permission := range permissions {
		if permission.PrintCopies <= 0 {
			continue
		}
		for _, page := range permission.Pages {
			if seen[page] && (job.maxCopies == 0 || permission.PrintCopies < job.maxCopies) {
				job.maxCopies = permission.PrintCopies
			}
		}
	}
	total := 0
	for _, index := range indices {
		page, err := d.document.Page(prefix, index)
		if err != nil {
			return nil, err
		}
		pixels := math.Ceil(page.Width*150/25.4) * math.Ceil(page.Height*150/25.4)
		if math.IsNaN(pixels) || math.IsInf(pixels, 0) || pixels <= 0 || pixels > 25000000 {
			return nil, errPageExportLimit
		}
		var output pageExportBuffer
		if err := d.document.RenderPage(ctx, prefix, &output, index, reader.RenderOptions{Format: "png", DPI: 150, Background: color.White, Purpose: reader.PermissionPrint, PNGCompressionLevel: 1}); err != nil {
			return nil, err
		}
		total += output.data.Len()
		if total > 64<<20 {
			return nil, errPageExportLimit
		}
		job.images = append(job.images, output.data.Bytes())
		job.pages = append(job.pages, page)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.document.CheckPermission(prefix, reader.PermissionPrint, indices); err != nil {
		return nil, err
	}
	return job, nil
}

// PrintPrepared 在图片加载后重新授权；预览不会凭借旧权限快照发起系统打印。
func (d *Desktop) PrintPrepared(token string) error {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	d.printMu.Lock()
	job := d.printing
	var window printWindow
	if job != nil {
		window = job.window
	}
	d.printMu.Unlock()
	if job == nil || job.token != token || time.Now().After(job.expires) {
		if job != nil && job.token == token {
			d.ClosePrint(token)
		}
		return errPrintExpired
	}
	if err := d.document.CheckPermission(job.prefix, reader.PermissionPrint, job.indices); err != nil {
		d.ClosePrint(token)
		return err
	}
	if window == nil {
		return errPrintExpired
	}
	return window.Print()
}

// ClosePrint 关闭指定打印窗口并释放内存，不接受网页提供的窗口标识。
func (d *Desktop) ClosePrint(token string) {
	job := d.removePrint(token)
	if job != nil && job.window != nil {
		job.window.Close()
	}
}

func (d *Desktop) releasePrint(token string) {
	// 原生窗口已在关闭，不重复调用 Close。
	d.removePrint(token)
}

func (d *Desktop) removePrint(token string) *printJob {
	d.printMu.Lock()
	defer d.printMu.Unlock()
	if d.printing != nil && d.printing.token == token {
		job := d.printing
		d.printing = nil
		if job.timer != nil {
			job.timer.Stop()
		}
		return job
	}
	return nil
}

func (d *Desktop) discardPrint() {
	d.printMu.Lock()
	old := d.printing
	d.printing = nil
	var window printWindow
	if old != nil {
		if old.timer != nil {
			old.timer.Stop()
		}
		window = old.window
	}
	d.printMu.Unlock()
	if window != nil {
		window.Close()
	}
}

func (d *Desktop) servePrint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/reader-print/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	d.printMu.Lock()
	job := d.printing
	d.printMu.Unlock()
	if job == nil || job.token != parts[0] {
		http.NotFound(w, r)
		return
	}
	if time.Now().After(job.expires) {
		d.ClosePrint(job.token)
		http.NotFound(w, r)
		return
	}
	if err := d.document.CheckPermission(job.prefix, reader.PermissionPrint, job.indices); err != nil {
		d.ClosePrint(job.token)
		http.Error(w, d.tr("打印权限失效或文档已切换"), http.StatusForbidden)
		return
	}
	switch parts[1] {
	case "index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := d.tr(job.html())
		if d.tr("关闭") == "Close" {
			page = strings.Replace(page, `lang="zh-CN"`, `lang="en"`, 1)
		}
		_, _ = w.Write([]byte(page))
	case "print.js":
		d.serveUIScript(w, "print.js", []byte(printScript))
	default:
		index, err := strconv.Atoi(strings.TrimSuffix(parts[1], ".png"))
		if err != nil || index < 0 || index >= len(job.images) || parts[1] != strconv.Itoa(index)+".png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(job.images[index])
	}
}

func (j *printJob) html() string {
	var styles, pages strings.Builder
	note := "打印对话框取消或关闭后，可关闭此预览释放图片。"
	if j.maxCopies > 0 {
		note = fmt.Sprintf("所选文档最多打印 %d 份，请在系统对话框中设置；应用无法可靠控制或累计系统打印份数。", j.maxCopies)
	}
	for i, page := range j.pages {
		fmt.Fprintf(&styles, "@page p%d{size:%gmm %gmm;margin:0}.p%d{page:p%d;width:%gmm;height:%gmm}", i, page.Width, page.Height, i, i, page.Width, page.Height)
		fmt.Fprintf(&pages, "<section class=\"page p%d\"><img src=\"%d.png\" alt=\"第 %d 页\"></section>", i, i, j.indices[i]+1)
	}
	return `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>OFD 打印预览</title><style>body{margin:0;background:#eee}.page{background:white;break-after:page;overflow:hidden}.page:last-child{break-after:auto}.page img{width:100%;height:100%;display:block}nav{padding:12px} @media print{nav{display:none}body{background:white}}` + styles.String() + `</style><nav><button id="print" disabled>正在加载…</button> <button id="close">关闭</button><span id="status">` + note + `</span></nav>` + pages.String() + `<script type="module" src="print.js"></script></html>`
}

const printScript = `import '/wails/runtime.js';
const button = document.querySelector('#print'), status = document.querySelector('#status');
document.querySelector('#close').onclick = () => wails.Call.ByName('main.Desktop.ClosePrint', location.pathname.split('/')[2]);
document.addEventListener('keydown', event => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'p') { event.preventDefault(); if (!button.disabled) button.click(); }
});
try {
  await Promise.all([...document.images].map(image => image.decode()));
  button.disabled = false; button.textContent = '系统打印…';
} catch (_) { status.textContent = '图片加载失败，请关闭后重试'; }
button.onclick = async () => {
  button.disabled = true;
  try { await wails.Call.ByName('main.Desktop.PrintPrepared', location.pathname.split('/')[2]); }
  catch (error) { status.textContent = error.message || String(error); }
  finally { button.disabled = false; }
};`
