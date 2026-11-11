package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/cmd/ofd-wasm/web"
	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
	"golang.org/x/image/font/gofont/goregular"
)

func TestDesktopSignatureSummariesAndStaleSession(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, content := range map[string]string{
		"OFD.xml":            `<OFD Version="1.0"><DocBody><DocRoot>Doc/Document.xml</DocRoot><Signatures>Doc/Signatures.xml</Signatures></DocBody></OFD>`,
		"Doc/Document.xml":   `<Document><CommonData><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc/Page.xml":       `<Page><Content><Layer ID="2"/></Content></Page>`,
		"Doc/Signatures.xml": `<Signatures><Signature ID="s1" BaseLoc="Signature.xml"/></Signatures>`,
		"Doc/Signature.xml":  `<Signature><SignedInfo><Provider ProviderName="测试提供者"/><StampAnnot ID="stamp" PageRef="1" Boundary="10 20 30 40"/></SignedInfo></Signature>`,
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data.Bytes(), readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := &Desktop{document: service}
	items, err := d.SignatureSummaries(t.Context(), opened.PagePrefix)
	if err != nil || len(items) != 1 || items[0].Provider != "测试提供者" || len(items[0].Stamps) != 1 || items[0].Stamps[0].Page != 0 || items[0].Stamps[0].ID != "stamp" {
		t.Fatalf("签名声明 = %+v, %v", items, err)
	}
	details, err := d.SignatureDetails(t.Context(), opened.PagePrefix)
	if err != nil || len(details) != 1 || details[0]["id"] != "s1" || details[0]["provider"] != "测试提供者" {
		t.Fatalf("签名详情 = %+v, %v", details, err)
	}
	if _, err := service.Open(data.Bytes(), readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SignatureSummaries(t.Context(), opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话 = %v", err)
	}
	if _, err := d.SignatureDetails(t.Context(), opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话签名详情 = %v", err)
	}
}

func TestDesktopEmbedsSharedPageWithoutWASM(t *testing.T) {
	for _, name := range []string{"index.html", "viewer.js", "material-symbols-outlined-subset.woff2"} {
		if _, err := web.Assets.ReadFile(name); err != nil {
			t.Fatalf("共享资源 %s: %v", name, err)
		}
	}
	for _, name := range []string{"ofd.wasm", "wasm_exec.js", "worker.js"} {
		if _, err := web.Assets.ReadFile(name); err == nil {
			t.Fatalf("不应打包 %s", name)
		}
	}
}

func TestDesktopOpenReturnsFileIdentityAndPermissions(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "identity", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "测试.ofd")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	result, err := (&Desktop{document: service}).openPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "测试.ofd" || result.Size != info.Size() || result.LastModified != info.ModTime().UnixMilli() {
		t.Fatalf("文件标识不一致: %+v", result)
	}
	if len(result.Pages) != 1 || len(result.Permissions) == 0 || !result.Permissions[0].Accessible {
		t.Fatalf("页面或权限快照错误: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"accessible":true`) || !strings.Contains(string(encoded), `"pages":[0]`) {
		t.Fatalf("打开结果 JSON = %s, %v", encoded, err)
	}
}

func TestCommandHelpVersionAndErrors(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		startErr error
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{name: "帮助", args: []string{"--help"}, wantOut: "用法"},
		{name: "版本", args: []string{"-v"}, wantOut: "ofd-viewer-wails v"},
		{name: "参数错误", args: []string{"--invalid"}, wantCode: 2, wantErr: "参数错误"},
		{name: "启动失败", startErr: errors.New("启动失败"), wantCode: 1, wantErr: "启动失败"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := command(test.args, &stdout, &stderr, func(string) error { return test.startErr })
			if code != test.wantCode || !strings.Contains(stdout.String(), test.wantOut) || !strings.Contains(stderr.String(), test.wantErr) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if test.wantErr == "" && stderr.Len() != 0 || test.wantOut == "" && stdout.Len() != 0 {
				t.Fatalf("输出流错误：stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestDesktopTextPreservesPermissionDenial(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "permissions", Pages: []creator.Page{{}},
		Permissions: &creator.Permissions{Export: &denied}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	document, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := (&Desktop{document: service}).Text(document.PagePrefix, 0)
	if err != nil || reply.Denied == nil || reply.Denied.Action != "copy" || reply.Denied.Reason != "forbidden" || reply.Denied.Scope != 0 || len(reply.Runs) != 0 {
		t.Fatalf("权限拒绝结果 = %+v, %v", reply, err)
	}
	desktop := &Desktop{document: service}
	check, err := desktop.CheckPermission(document.PagePrefix, "copy", []int{0})
	if err != nil || check.Denied == nil || check.Denied.Action != "copy" {
		t.Fatalf("复制检查结果 = %+v, %v", check, err)
	}
	if _, err := desktop.CheckPermission(document.PagePrefix, "copy", make([]int, (1<<16)+1)); !errors.Is(err, errTooManyPermissionPages) {
		t.Fatalf("超限页面检查错误 = %v", err)
	}
	if _, err := desktop.CheckPermission(document.PagePrefix, "copy", nil); err == nil {
		t.Fatal("空页面检查不应通过")
	}
}

func TestDesktopTextEmptyAndStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "text", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	reply, err := desktop.Text(opened.PagePrefix, 0)
	if err != nil || reply.Runs == nil || len(reply.Runs) != 0 || reply.Denied != nil {
		t.Fatalf("空文字 = %+v, %v", reply, err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.Text(opened.PagePrefix, 0); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopSearchPreservesCancellationAndStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "search", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	report, err := desktop.Search(t.Context(), opened.PagePrefix, "未命中")
	if err != nil || report.Scanned != 1 || report.Results == nil || len(report.Results) != 0 {
		t.Fatalf("空搜索结果 = %+v, %v", report, err)
	}
	encoded, err := json.Marshal(report)
	if err != nil || !strings.Contains(string(encoded), `"results":[]`) {
		t.Fatalf("Wails 搜索 JSON = %s, %v", encoded, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := desktop.Search(ctx, opened.PagePrefix, "未命中"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.Search(t.Context(), opened.PagePrefix, "未命中"); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopPreferencesPreservesZoomAndRejectsStaleSession(t *testing.T) {
	zoom := 1.75
	data, err := creator.Marshal(creator.Document{ID: "preferences", Pages: []creator.Page{{}},
		Preferences: &creator.ViewPreferences{Zoom: &zoom}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	preferences, err := desktop.Preferences(opened.PagePrefix)
	if err != nil || preferences.Zoom == nil || *preferences.Zoom != zoom {
		t.Fatalf("显示偏好 = %+v, %v", preferences, err)
	}
	encoded, err := json.Marshal(preferences)
	if err != nil || !strings.Contains(string(encoded), `"zoom":1.75`) || !strings.Contains(string(encoded), `"page_layout":`) {
		t.Fatalf("Wails 显示偏好 JSON = %s, %v", encoded, err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.Preferences(opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopOutlineEmptyArraysAndStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "outline", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	tree, err := desktop.Outline(opened.PagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(tree)
	if err != nil || !strings.Contains(string(encoded), `"nodes":[]`) || !strings.Contains(string(encoded), `"bookmarks":[]`) {
		t.Fatalf("Wails 大纲 JSON = %s, %v", encoded, err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.Outline(opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopInfoSerializesFontDeclarations(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "fonts", Pages: []creator.Page{{}},
		Fonts: []creator.Font{{Name: "ExampleFont", FixedWidth: true}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := (&Desktop{document: service}).Info(opened.PagePrefix)
	if err != nil || info.FontsError != "" || len(info.Fonts) != 1 || info.Fonts[0].Name != "ExampleFont" {
		t.Fatalf("字体声明 = %+v, %v", info, err)
	}
	encoded, err := json.Marshal(info)
	if err != nil || !strings.Contains(string(encoded), `"fixed_width":true`) || strings.Contains(string(encoded), `"data":`) {
		t.Fatalf("字体声明 JSON = %s, %v", encoded, err)
	}
}

func TestDesktopStatsUsesCurrentDocument(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "stats", Pages: []creator.Page{{}},
		Fonts: []creator.Font{{Name: "ExampleFont"}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	stats, err := desktop.Stats(opened.PagePrefix)
	if err != nil || stats.Fonts != 1 {
		t.Fatalf("资源统计 = %+v, %v", stats, err)
	}
	encoded, err := json.Marshal(stats)
	if err != nil || !strings.Contains(string(encoded), `"fonts":1`) || !strings.Contains(string(encoded), `"annotation_pages":0`) {
		t.Fatalf("资源统计 JSON = %s, %v", encoded, err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.Stats(opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopAttachmentsListAndRejectStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "attachments", Pages: []creator.Page{{}},
		Attachments: []creator.Attachment{{ID: "attach7", Name: "说明.txt", FileName: "note.txt", Data: []byte("内容")}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	list, err := desktop.Attachments(t.Context(), opened.PagePrefix)
	if err != nil || len(list) != 1 || list[0].Name != "说明.txt" || !list[0].Exists {
		t.Fatalf("附件清单 = %+v, %v", list, err)
	}
	encoded, err := json.Marshal(list)
	if err != nil || !strings.Contains(string(encoded), `"actual_size":`) || strings.Contains(string(encoded), `"data":`) {
		t.Fatalf("附件清单 JSON = %s, %v", encoded, err)
	}
	list[0].Name = "修改后的副本"
	again, err := desktop.Attachments(t.Context(), opened.PagePrefix)
	if err != nil || len(again) != 1 || again[0].Name != "说明.txt" {
		t.Fatalf("附件缓存被更改 = %+v, %v", again, err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.Attachments(t.Context(), opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopAttachmentTextPreviewChecksExportAndSession(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(strconv.FormatBool(allowed), func(t *testing.T) {
			data, err := creator.Marshal(creator.Document{ID: "text-preview", Pages: []creator.Page{{}},
				Permissions: &creator.Permissions{Export: &allowed},
				Attachments: []creator.Attachment{{ID: "a", Name: "说明.txt", FileName: "note.txt", Data: []byte("中文<script/>")}}})
			if err != nil {
				t.Fatal(err)
			}
			service := &readersession.Service{}
			t.Cleanup(func() { _ = service.Close() })
			opened, err := service.Open(data, readersession.Options{})
			if err != nil {
				t.Fatal(err)
			}
			desktop := &Desktop{document: service}
			text, err := desktop.AttachmentTextPreview(t.Context(), opened.PagePrefix, 0, "a")
			if allowed {
				if err != nil || text != "中文<script/>" {
					t.Fatalf("文本预览 = %q, %v", text, err)
				}
			} else if !errors.Is(err, reader.ErrPermissionDenied) || text != "" {
				t.Fatalf("禁止导出的预览 = %q, %v", text, err)
			}
			if _, err := service.Open(data, readersession.Options{}); err != nil {
				t.Fatal(err)
			}
			if _, err := desktop.AttachmentTextPreview(t.Context(), opened.PagePrefix, 0, "a"); !errors.Is(err, readersession.ErrStaleDocument) {
				t.Fatalf("旧会话错误 = %v", err)
			}
		})
	}
}

func TestDesktopMediaListSnapshotAndStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "media-list", Pages: []creator.Page{{}},
		Media: []creator.Media{{ID: 200, Type: "Audio", Format: "wav", Name: "sound.wav", Data: []byte("audio")}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	items, err := desktop.Media(t.Context(), opened.PagePrefix)
	if err != nil || len(items) != 1 || items[0].Name != "sound.wav" || !items[0].Exists || items[0].Size != 5 {
		t.Fatalf("资源清单 = %+v, %v", items, err)
	}
	encoded, err := json.Marshal(items)
	if err != nil || !strings.Contains(string(encoded), `"type":"Audio"`) || strings.Contains(string(encoded), `"data":`) {
		t.Fatalf("资源清单 JSON = %s, %v", encoded, err)
	}
	items[0].Name = "修改后的副本"
	again, err := desktop.Media(t.Context(), opened.PagePrefix)
	if err != nil || len(again) != 1 || again[0].Name != "sound.wav" {
		t.Fatalf("资源缓存被更改 = %+v, %v", again, err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.Media(t.Context(), opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopMediaEmptyListSerializesAsArray(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "empty-media", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	items, err := (&Desktop{document: service}).Media(t.Context(), opened.PagePrefix)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(items)
	if err != nil || string(encoded) != "[]" {
		t.Fatalf("空资源清单 JSON = %s, %v", encoded, err)
	}
}

func TestDesktopMediaImagePreviewAndStaleSession(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data, err := creator.Marshal(creator.Document{ID: "media-preview", Pages: []creator.Page{{}},
		Media: []creator.Media{{ID: 200, Type: "Image", Format: "png", Name: "image.png", Data: picture.Bytes()}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	result, err := desktop.MediaImagePreview(t.Context(), opened.PagePrefix, 0, 200)
	if err != nil || result.MIME != "image/png" || !bytes.Equal(result.Data, picture.Bytes()) {
		t.Fatalf("图片预览 = %+v, %v", result, err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.MediaImagePreview(t.Context(), opened.PagePrefix, 0, 200); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话预览 = %v", err)
	}
}

func TestDesktopAnnotationsForPage(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "remarks", Pages: []creator.Page{{}, {}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{ID: 42, Type: "Stamp", Creator: "测试", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Remark: "测试备注", Boundary: &creator.Box{X: 5, Y: 6, Width: 10, Height: 10}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	items, err := desktop.AnnotationsForPage(t.Context(), opened.PagePrefix, 0)
	if err != nil || len(items) != 1 || items[0].Remark != "测试备注" || items[0].Boundary == nil {
		t.Fatalf("单页注解 = %+v, %v", items, err)
	}
	report, err := desktop.Annotations(t.Context(), opened.PagePrefix)
	if err != nil || report.Scanned != 2 || report.Truncated || len(report.Items) != 1 {
		t.Fatalf("注解报告 = %+v, %v", report, err)
	}
	encoded, err := json.Marshal(items)
	if err != nil || !strings.Contains(string(encoded), `"boundary":{"x":5`) || !strings.Contains(string(encoded), `"dest":null`) {
		t.Fatalf("注解 JSON = %s, %v", encoded, err)
	}
	empty, err := desktop.AnnotationsForPage(t.Context(), opened.PagePrefix, 1)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("空页注解 = %+v, %v", empty, err)
	}
	if _, err := desktop.AnnotationsForPage(t.Context(), opened.PagePrefix, -1); err == nil {
		t.Fatal("负页码应失败")
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.AnnotationsForPage(t.Context(), opened.PagePrefix, 0); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
	if _, err := desktop.Annotations(t.Context(), opened.PagePrefix); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧报告会话错误 = %v", err)
	}
}

func TestDesktopPageLinksForPageAndStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "links", Pages: []creator.Page{
		{Items: []creator.Item{creator.Path{X: 1, Y: 2, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true,
			Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &creator.GotoAction{Page: 1}}}}}}, {},
	}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	links, err := desktop.PageLinksForPage(t.Context(), opened.PagePrefix, 0)
	if err != nil || len(links) != 1 || links[0].TargetPage != 1 || links[0].Boundary == nil {
		t.Fatalf("页面链接 = %+v, %v", links, err)
	}
	empty, err := desktop.PageLinksForPage(t.Context(), opened.PagePrefix, 1)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("空链接 = %+v, %v", empty, err)
	}
	if _, err := desktop.PageLinksForPage(t.Context(), opened.PagePrefix, -1); err == nil {
		t.Fatal("负页码应失败")
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.PageLinksForPage(t.Context(), opened.PagePrefix, 0); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestDesktopLayerVisibilityUpdatesRenderTextSearchAndLinks(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "desktop-layers", Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}},
		Pages: []creator.Page{{Layers: []creator.Layer{
			{Type: "Body", Items: []creator.Item{creator.Text{X: 1, Y: 1, Width: 30, Height: 8, Font: "Go", Size: 4, Value: "HiddenText",
				Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &creator.GotoAction{Page: 1}}}}}},
			{Type: "Foreground", Items: []creator.Item{creator.Path{X: 1, Y: 15, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true}}},
		}}, {}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := &Desktop{document: service}
	layers, err := d.Layers(t.Context(), opened.PagePrefix, 0)
	if err != nil || len(layers) != 2 || !layers[0].Visible {
		t.Fatalf("图层列表 = %+v, %v", layers, err)
	}
	encoded, err := json.Marshal(layers)
	if err != nil || !strings.Contains(string(encoded), `"visible":true`) {
		t.Fatalf("图层 JSON = %s, %v", encoded, err)
	}
	render := func(format string) []byte {
		t.Helper()
		response := httptest.NewRecorder()
		service.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, opened.PagePrefix+"0?format="+format+"&dpi=24", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s 渲染状态 = %d", format, response.Code)
		}
		return response.Body.Bytes()
	}
	beforePNG, beforeSVG := render("png"), render("svg")
	if err := d.SetHiddenLayers(opened.PagePrefix, 0, []string{layers[0].Key}); err != nil {
		t.Fatal(err)
	}
	after, err := d.Layers(t.Context(), opened.PagePrefix, 0)
	if err != nil || after[0].Visible || !after[1].Visible {
		t.Fatalf("隐藏后图层 = %+v, %v", after, err)
	}
	if bytes.Equal(beforePNG, render("png")) || bytes.Equal(beforeSVG, render("svg")) {
		t.Fatal("图层切换未更新 PNG 或 SVG")
	}
	runs, err := service.Text(opened.PagePrefix, 0)
	if err != nil || len(runs) != 0 {
		t.Fatalf("隐藏后的文字 = %+v, %v", runs, err)
	}
	results, err := service.Search(t.Context(), opened.PagePrefix, "HiddenText", reader.SearchOptions{})
	if err != nil || len(results.Results) != 0 {
		t.Fatalf("隐藏后的搜索 = %+v, %v", results, err)
	}
	links, err := d.PageLinksForPage(t.Context(), opened.PagePrefix, 0)
	if err != nil || len(links) != 0 {
		t.Fatalf("隐藏后的链接 = %+v, %v", links, err)
	}
	if err := d.SetHiddenLayers(opened.PagePrefix, 0, []string{"invalid"}); err == nil {
		t.Fatal("未知图层应失败")
	}
	if err := d.SetHiddenLayers(opened.PagePrefix, 0, nil); err != nil {
		t.Fatal(err)
	}
	runs, err = service.Text(opened.PagePrefix, 0)
	if err != nil || len(runs) == 0 {
		t.Fatalf("恢复后的文字 = %+v, %v", runs, err)
	}
	if err := d.SetHiddenLayers(opened.PagePrefix, 0, []string{layers[0].Key}); err != nil {
		t.Fatal(err)
	}
	replacement, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetHiddenLayers(opened.PagePrefix, 0, nil); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话写入错误 = %v", err)
	}
	newLayers, err := d.Layers(t.Context(), replacement.PagePrefix, 0)
	if err != nil || len(newLayers) != 2 || !newLayers[0].Visible {
		t.Fatalf("新会话图层状态 = %+v, %v", newLayers, err)
	}
}

func TestDesktopFontUsageBoundsCancellationAndStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "font-usage", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	opened, err := service.Open(data, readersession.Options{})
	if err != nil {
		t.Fatal(err)
	}
	desktop := &Desktop{document: service}
	usage, err := desktop.FontUsage(t.Context(), opened.PagePrefix, 0, 7, 3, 1, 10)
	if err != nil || usage.Scanned != 1 || usage.Pages == nil || len(usage.Pages) != 0 {
		t.Fatalf("字体统计 = %+v, %v", usage, err)
	}
	report, err := desktop.FontUsageAll(t.Context(), opened.PagePrefix, 1, 10)
	if err != nil || report.Scanned != 1 || report.Fonts == nil || len(report.Fonts) != 0 {
		t.Fatalf("批量字体统计 = %+v, %v", report, err)
	}
	encoded, err := json.Marshal(report)
	if err != nil || !strings.Contains(string(encoded), `"fonts":[]`) {
		t.Fatalf("字体统计 JSON = %s, %v", encoded, err)
	}
	if _, err := desktop.FontUsageAll(t.Context(), opened.PagePrefix, 10001, 10); !errors.Is(err, errInvalidFontUsageOptions) {
		t.Fatalf("超限错误 = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := desktop.FontUsageAll(ctx, opened.PagePrefix, 1, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
	if _, err := service.Open(data, readersession.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := desktop.FontUsage(t.Context(), opened.PagePrefix, 0, 7, 3, 1, 10); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}
