// ofd-viewer-wails 是复用网页界面的实验性原生 OFD 桌面阅读器。
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/zc310/ofd/cmd/ofd-wasm/web"
	readercontract "github.com/zc310/ofd/internal/reader/contract"
	"github.com/zc310/ofd/internal/reader/preview"
	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/reader"
)

//go:embed prototype/native-page.js desktop.js
var assets embed.FS

type Desktop struct {
	app               *application.App
	document          *readersession.Service
	openMu            sync.Mutex
	openDirectory     string
	openDirectoryFile string
	recentFile        string
	recent            []recentDocument
	inputPath         string
	inputPrefix       string
	inputInfo         os.FileInfo
	dropMu            sync.Mutex
	dropToken         string
	dropPath          string
	dropExpires       time.Time
	unlock            *unlockPending
	unlockedView      *crypt.View
	printMu           sync.Mutex
	printing          *printJob
	launchMu          sync.Mutex
	launchPending     []string
	settingsMu        sync.Mutex
	language          string
	languageFile      string
	mainWindow        atomic.Pointer[application.WebviewWindow]
	uiScripts         sync.Map
}

// Version 返回构建注入的发行版本；无需先打开文档。
func (d *Desktop) Version() string { return version.Version }

// AttachmentTextPreview 按导出权限返回受限的 UTF-8 文本附件。
func (d *Desktop) AttachmentTextPreview(ctx context.Context, prefix string, scope int, id string) (string, error) {
	return d.document.AttachmentTextPreview(ctx, prefix, scope, id)
}

// MediaImagePreview 返回受限的光栅资源预览；额外格式由原生端转为静态 PNG。
func (d *Desktop) MediaImagePreview(ctx context.Context, prefix string, scope int, id uint64) (preview.Image, error) {
	return d.document.MediaImagePreview(ctx, prefix, scope, id)
}

// SignatureDetails 返回签名及解析器已有的验证明细。
func (d *Desktop) SignatureDetails(ctx context.Context, prefix string) ([]map[string]any, error) {
	infos, err := d.document.SignatureDetails(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return readercontract.Signatures(infos), nil
}

type openedDocument struct {
	Challenge    string                       `json:"challenge,omitempty"`
	Code         string                       `json:"code,omitempty"`
	Message      string                       `json:"message,omitempty"`
	Name         string                       `json:"name"`
	Size         int64                        `json:"size"`
	LastModified int64                        `json:"last_modified"`
	PagePrefix   string                       `json:"page_prefix"`
	Pages        []reader.PageInfo            `json:"pages"`
	Permissions  []reader.DocumentPermissions `json:"permissions"`
}

// Open 使用原生文件对话框选择文档，不接收前端提供的文件路径。
func (d *Desktop) Open() (*openedDocument, error) {
	return d.chooseAndOpen(func(directory string) (string, error) {
		dialog := d.app.Dialog.OpenFile().SetTitle(d.tr("打开 OFD 文档")).
			CanChooseFiles(true).AddFilter(d.tr("OFD 文档"), "*.ofd")
		if directory != "" {
			dialog.SetDirectory(directory)
		}
		return dialog.PromptForSingleSelection()
	})
}

func (d *Desktop) openPath(path string) (*openedDocument, error) {
	d.unlock = nil
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件信息: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errDocumentNotRegular
	}
	result, err := d.document.Open(path, readersession.Options{})
	if errors.Is(err, crypt.ErrLocked) {
		return d.challenge(path, info)
	}
	if err != nil {
		return nil, err
	}
	if d.unlockedView != nil {
		_ = d.unlockedView.Close()
		d.unlockedView = nil
	}
	return d.acceptOpened(path, info, result), nil
}

func (d *Desktop) acceptOpened(path string, info os.FileInfo, result readersession.OpenResult) *openedDocument {
	d.discardPrint()
	d.inputPath, d.inputPrefix = path, result.PagePrefix
	d.inputInfo = info
	d.rememberRecent(path, info)
	return &openedDocument{Name: filepath.Base(path), Size: info.Size(), LastModified: info.ModTime().UnixMilli(), PagePrefix: result.PagePrefix, Pages: result.Pages, Permissions: readercontract.Permissions(result.Permissions)}
}

// PageInfo 查询当前文档的一页；prefix 防止旧请求读取新文档。
func (d *Desktop) PageInfo(prefix string, index int) (reader.PageInfo, error) {
	return d.document.Page(prefix, index)
}

// Close 关闭指定会话，过期地址不能关闭新文档。
func (d *Desktop) Close(prefix string) error {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	if err := d.document.CloseCurrent(prefix); err != nil {
		return err
	}
	d.inputPath, d.inputPrefix, d.inputInfo = "", "", nil
	d.discardPrint()
	if d.unlockedView != nil {
		err := d.unlockedView.Close()
		d.unlockedView = nil
		return err
	}
	return nil
}

type textReply struct {
	Runs   []readercontract.TextRun `json:"runs"`
	Denied *permissionError         `json:"denied,omitempty"`
}

type permissionError struct {
	Message string `json:"message"`
	Reason  string `json:"reason"`
	Action  string `json:"action"`
	Scope   int    `json:"scope"`
}

// Text 查询当前文档的文字；权限拒绝保留供前端使用的结构化信息。
func (d *Desktop) Text(prefix string, index int) (textReply, error) {
	runs, err := d.document.Text(prefix, index)
	if denied := permissionDetails(err); denied != nil {
		return textReply{Denied: denied}, nil
	}
	if err != nil {
		return textReply{}, err
	}
	return textReply{Runs: readercontract.TextRuns(runs)}, nil
}

func permissionDetails(err error) *permissionError {
	if denied, ok := errors.AsType[*permission.DeniedError](err); ok {
		return &permissionError{Message: denied.Error(), Reason: string(denied.Reason), Action: string(denied.Action), Scope: denied.Scope}
	}
	return nil
}

// Permissions 返回指定会话的权限快照，不代表后续操作始终获准。
func (d *Desktop) Permissions(prefix string) ([]reader.DocumentPermissions, error) {
	permissions, err := d.document.Permissions(prefix)
	if err != nil {
		return nil, err
	}
	return readercontract.Permissions(permissions), nil
}

// Outline 返回指定会话的大纲和书签。
func (d *Desktop) Outline(prefix string) (reader.OutlineTree, error) {
	tree, err := d.document.Outline(prefix)
	if err != nil {
		return reader.OutlineTree{}, err
	}
	return readercontract.Outline(tree), nil
}

// Preferences 返回指定会话的文档显示偏好。
func (d *Desktop) Preferences(prefix string) (reader.ViewPreferences, error) {
	return d.document.Preferences(prefix)
}

type infoReply struct {
	Security    map[string]any               `json:"security"`
	Info        reader.DocumentInfo          `json:"info"`
	Permissions []reader.DocumentPermissions `json:"permissions"`
	Fonts       []reader.FontInfo            `json:"fonts"`
	FontsError  string                       `json:"fonts_error"`
}

// Info 返回指定会话的文档基本信息和权限快照。
func (d *Desktop) Info(prefix string) (infoReply, error) {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	info, permissions, err := d.document.Info(prefix)
	if err != nil {
		return infoReply{}, err
	}
	fonts, fontErr := d.document.FontList(prefix)
	result := infoReply{Info: info, Permissions: readercontract.Permissions(permissions), Fonts: readercontract.Fonts(fonts)}
	state := "none"
	if d.unlockedView != nil {
		state = "unlocked"
	}
	result.Security = map[string]any{"encrypted": d.unlockedView != nil, "state": state}
	if fontErr != nil {
		result.FontsError = fontErr.Error()
	}
	return result, nil
}

// Stats 返回文档声明的资源数量，不加载资源文件内容。
func (d *Desktop) Stats(prefix string) (reader.DocumentStats, error) {
	return d.document.Stats(prefix)
}

// Attachments 返回附件清单，不返回附件内容。
func (d *Desktop) Attachments(ctx context.Context, prefix string) ([]reader.AttachmentInfo, error) {
	return d.document.Attachments(ctx, prefix)
}

// Media 返回多媒体资源清单，不返回资源内容。
func (d *Desktop) Media(ctx context.Context, prefix string) ([]reader.MediaInfo, error) {
	return d.document.Media(ctx, prefix)
}

// AnnotationsForPage 返回单页注解，按页面检查文档访问权限。
func (d *Desktop) AnnotationsForPage(ctx context.Context, prefix string, index int) ([]reader.AnnotationInfo, error) {
	return d.document.AnnotationsForPage(ctx, prefix, index)
}

// PageLinksForPage 返回页面中的链接和动作，是否执行由界面能力边界决定。
func (d *Desktop) PageLinksForPage(ctx context.Context, prefix string, index int) ([]readercontract.PageLink, error) {
	links, err := d.document.PageLinksForPage(ctx, prefix, index)
	if err != nil {
		return nil, err
	}
	return readercontract.PageLinks(links), nil
}

// Layers 返回单页图层列表。
func (d *Desktop) Layers(ctx context.Context, prefix string, index int) ([]reader.LayerInfo, error) {
	return d.document.Layers(ctx, prefix, index)
}

// SignatureSummaries 返回只读签名声明，不提供验签结论。
func (d *Desktop) SignatureSummaries(ctx context.Context, prefix string) ([]readersession.SignatureSummary, error) {
	return d.document.SignatureSummaries(ctx, prefix)
}

// SetHiddenLayers 原子更新指定会话的图层可见性。
func (d *Desktop) SetHiddenLayers(prefix string, index int, ids []string) error {
	return d.document.SetHiddenLayers(prefix, index, ids)
}

// Annotations 有界扫描注解；取消或权限错误时不返回部分结果。
func (d *Desktop) Annotations(ctx context.Context, prefix string) (readersession.AnnotationReport, error) {
	return d.document.Annotations(ctx, prefix)
}

var errInvalidFontUsageOptions = errors.New("字体统计上限无效")
var errInvalidFontScope = errors.New("无效字体作用域")

func fontUsageOptions(maxScan, maxPages int) (reader.FontUsageOptions, error) {
	if maxScan < 1 || maxScan > 10000 || maxPages < 1 || maxPages > 500 {
		return reader.FontUsageOptions{}, errInvalidFontUsageOptions
	}
	return reader.FontUsageOptions{MaxScan: maxScan, MaxPages: maxPages}, nil
}

// FontUsage 返回指定字体的使用页，关闭文档或取消请求时停止扫描。
func (d *Desktop) FontUsage(ctx context.Context, prefix string, scope int, id, templateID uint64, maxScan, maxPages int) (reader.FontUsage, error) {
	if scope < 0 {
		return reader.FontUsage{}, errInvalidFontScope
	}
	options, err := fontUsageOptions(maxScan, maxPages)
	if err != nil {
		return reader.FontUsage{}, err
	}
	result, err := d.document.FontUsage(ctx, prefix, reader.FontRef{Scope: scope, ID: id, TemplateID: templateID}, options)
	if err != nil {
		return reader.FontUsage{}, err
	}
	if result.Pages == nil {
		result.Pages = []int{}
	}
	return result, nil
}

// FontUsageAll 批量统计字体使用页，结果受扫描和每字体页面上限约束。
func (d *Desktop) FontUsageAll(ctx context.Context, prefix string, maxScan, maxPages int) (reader.FontUsageReport, error) {
	options, err := fontUsageOptions(maxScan, maxPages)
	if err != nil {
		return reader.FontUsageReport{}, err
	}
	report, err := d.document.FontUsageAll(ctx, prefix, options)
	if err != nil {
		return reader.FontUsageReport{}, err
	}
	if report.Fonts == nil {
		report.Fonts = []reader.FontUsageSummary{}
	}
	for i := range report.Fonts {
		if report.Fonts[i].Pages == nil {
			report.Fonts[i].Pages = []int{}
		}
	}
	return report, nil
}

type checkPermissionReply struct {
	Denied *permissionError `json:"denied,omitempty"`
}

var errTooManyPermissionPages = errors.New("权限检查页数超过限制")

// CheckPermission 重新检查当前权限，限制单次请求的页数。
func (d *Desktop) CheckPermission(prefix string, action string, indices []int) (checkPermissionReply, error) {
	if len(indices) > 1<<16 {
		return checkPermissionReply{}, errTooManyPermissionPages
	}
	err := d.document.CheckPermission(prefix, reader.PermissionAction(action), indices)
	if denied := permissionDetails(err); denied != nil {
		return checkPermissionReply{Denied: denied}, nil
	}
	return checkPermissionReply{}, err
}

// Search 在当前文档搜索，工作量上限与 WASM 阅读器一致。
func (d *Desktop) Search(ctx context.Context, prefix, query string) (reader.SearchReport, error) {
	report, err := d.document.Search(ctx, prefix, query, reader.SearchOptions{MaxPages: 2000, MaxResults: 5000})
	if err != nil {
		return reader.SearchReport{}, err
	}
	return readercontract.Search(report), nil
}

func main() {
	os.Exit(command(os.Args[1:], os.Stdout, os.Stderr, run))
}

func command(args []string, stdout, stderr io.Writer, start func(string) error) int {
	if len(args) == 1 {
		switch args[0] {
		case "-h", "--help":
			_, _ = fmt.Fprintln(stdout, "用法: ofd-viewer-wails [--help | --version] [--] [文件.ofd]")
			return 0
		case "-v", "--version":
			_, _ = fmt.Fprintln(stdout, "ofd-viewer-wails v"+version.Version)
			return 0
		}
	}
	path, err := launchPath(args, "")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "参数错误：使用 --help 查看用法")
		return 2
	}
	if path != "" {
		if err := validateLaunchFile(path); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if err := start(path); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func run(path string) error {
	doc := &readersession.Service{}
	bridge := &Desktop{document: doc}
	bridge.loadOpenDirectory()
	bridge.loadRecentDocuments()
	bridge.loadLanguage()
	bridge.prunePlatformImports()
	if path != "" {
		bridge.queueLaunch(path)
	}
	app := application.New(application.Options{
		Name:  bridge.tr("OFD 阅读器"),
		Linux: application.LinuxOptions{ApplicationID: "io.github.zc310.ofd.wails"},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.zc310.ofd.wails",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				args := data.Args
				if len(args) > 0 {
					args = args[1:]
				}
				path, err := launchPath(args, data.WorkingDir)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return
				}
				if path != "" {
					bridge.queueLaunch(path)
				}
				if window := bridge.mainWindow.Load(); window != nil {
					window.Show().Focus()
				}
			},
		},
		Services: []application.Service{application.NewService(bridge)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(web.Assets),
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/" || r.URL.Path == "/index.html" {
						index, _ := web.Assets.ReadFile("index.html")
						w.Header().Set("Content-Type", "text/html; charset=utf-8")
						_, _ = io.WriteString(w, strings.Replace(string(index), `<script src="viewer.js"></script>`, `<script src="localization.js"></script><script type="module" src="desktop.js"></script>`, 1))
						return
					}
					if r.URL.Path == "/localization.js" {
						bridge.serveLocalization(w)
						return
					}
					if r.URL.Path == "/viewer.js" {
						data, _ := web.Assets.ReadFile("viewer.js")
						bridge.serveUIScript(w, r.URL.Path, data)
						return
					}
					if r.URL.Path == "/desktop.js" || r.URL.Path == "/prototype/native-page.js" {
						data, err := assets.ReadFile(strings.TrimPrefix(r.URL.Path, "/"))
						if err != nil {
							http.NotFound(w, r)
							return
						}
						bridge.serveUIScript(w, r.URL.Path, data)
						return
					}
					if strings.HasPrefix(r.URL.Path, "/reader/") {
						doc.ServeHTTP(w, r)
						return
					}
					if strings.HasPrefix(r.URL.Path, "/reader-print/") {
						bridge.servePrint(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		OnShutdown: bridge.shutdown,
	})
	bridge.app = app
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{Title: bridge.tr("OFD 阅读器"), Width: 1100, Height: 800, URL: "/", EnableFileDrop: true})
	bridge.mainWindow.Store(window)
	app.Event.OnApplicationEvent(events.Common.ApplicationOpenedWithFile, func(event *application.ApplicationEvent) {
		bridge.queueLaunch(event.Context().Filename())
		window.Show().Focus()
	})
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		token, err := bridge.prepareDroppedFiles(event.Context().DroppedFiles())
		if err != nil {
			app.Event.Emit("ofd-file-drop", map[string]string{"error": err.Error()})
			return
		}
		app.Event.Emit("ofd-file-drop", map[string]string{"token": token})
	})
	return app.Run()
}
