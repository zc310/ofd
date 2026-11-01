package reader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/permission"
)

func TestPermissionActionsUseIndependentDocumentDeclarations(t *testing.T) {
	denied, allowed := false, true
	data, err := creator.Marshal(creator.Document{ID: "actions", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{Edit: &denied, Annot: &allowed, Export: &allowed, Signature: &denied, Watermark: &denied, PrintScreen: &denied}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, action := range []PermissionAction{PermissionEdit, PermissionSignature, PermissionWatermark, PermissionPrintScreen} {
		err := r.CheckDocumentPermission(action, 0)
		var detail *permission.DeniedError
		if !errors.Is(err, ErrPermissionDenied) || !errors.As(err, &detail) || detail.Reason != permission.Forbidden || detail.Action != action {
			t.Fatalf("%s检查错误：%v", action, err)
		}
	}
	for _, action := range []PermissionAction{PermissionAccess, PermissionAnnot, PermissionExport, PermissionCopy, PermissionPrint} {
		if err := r.CheckPermission(action, []int{0}); err != nil {
			t.Fatalf("%s被Edit=false错误阻止：%v", action, err)
		}
	}
}

func periodPermissionsPackage(t *testing.T) []byte {
	t.Helper()
	input := permissionsPackage(t)
	archive, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for _, file := range archive.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		period := `<ValidPeriod StartDate="2026-01-01T00:00:00Z" EndDate="2026-01-02T00:00:00Z"/>`
		switch file.Name {
		case "Restricted.xml", "Allowed.xml":
			data = []byte(strings.Replace(string(data), `</Permissions>`, period+`</Permissions>`, 1))
		case "History.xml":
			data = []byte(strings.Replace(string(data), `</Document>`, `<Permissions><ValidPeriod StartDate="2026-01-03T00:00:00Z"/></Permissions></Document>`, 1))
		}
		entry, err := w.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestPermissionPolicyOpenEnforcesPeriodAndExplicitIgnoreIsLocal(t *testing.T) {
	data := periodPermissionsPackage(t)
	for _, hour := range []int{0, 72} {
		now := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC).Add(time.Duration(hour) * time.Hour)
		r, err := Open(data, OpenOptions{PermissionPolicy: permission.Policy{Now: func() time.Time { return now }}})
		if r != nil || !errors.Is(err, ErrPermissionDenied) {
			if r != nil {
				_ = r.Close()
			}
			t.Fatalf("过期或未生效文档被打开：%v", err)
		}
	}
	r, err := Open(data, OpenOptions{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if runs, err := r.Text(0); err != nil || len(runs) == 0 {
		t.Fatalf("显式诊断策略未生效：%v", err)
	}
	if _, err := Open(data, OpenOptions{PermissionPolicy: permission.Policy{Mode: permission.Mode(99)}}); !errors.Is(err, permission.ErrMode) {
		t.Fatalf("无效策略被接受：%v", err)
	}
}

func TestPermissionPeriodRecheckedBeforeCachedContentAndResources(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).UnixNano())
	r, err := Open(periodPermissionsPackage(t), OpenOptions{PermissionPolicy: permission.Policy{Now: func() time.Time { return time.Unix(0, clock.Load()) }}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// 先填充文字、搜索及元数据缓存，不能仅对首次读取门控。
	if _, err := r.Text(1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Search(t.Context(), "Secret", SearchOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Fonts(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Annotations(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Outline(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PageLinks(); err != nil {
		t.Fatal(err)
	}
	clock.Store(time.Date(2026, 1, 2, 0, 0, 1, 0, time.UTC).UnixNano())
	checks := []struct {
		name string
		call func() error
	}{
		{"文字缓存", func() error { _, e := r.Text(1); return e }},
		{"搜索缓存", func() error { _, e := r.Search(t.Context(), "Secret", SearchOptions{}); return e }},
		{"字体缓存", func() error { _, e := r.Fonts(); return e }},
		{"字体清单", func() error { _, e := r.FontList(); return e }},
		{"字体使用页", func() error { _, e := r.FontUsageAll(FontUsageOptions{}); return e }},
		{"注解缓存", func() error { _, e := r.Annotations(); return e }},
		{"逐页注解", func() error { _, e := r.AnnotationsForPage(1); return e }},
		{"大纲缓存", func() error { _, e := r.Outline(); return e }},
		{"页面链接缓存", func() error { _, e := r.PageLinks(); return e }},
		{"逐页链接", func() error { _, e := r.PageLinksForPage(1); return e }},
		{"附件", func() error { _, e := r.AttachmentData(1, "note", 0); return e }},
		{"媒体", func() error { _, e := r.MediaData(1, 99, 0); return e }},
		{"页面尺寸", func() error { _, e := r.Page(1); return e }},
		{"逐页动作", func() error { _, e := r.PageEntryActionsForPage(1); return e }},
		{"元数据", func() error { _, e := r.Info(); return e }},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("到期后仍返回内容：%v", err)
			}
		})
	}
	for _, purpose := range []PermissionAction{"", PermissionExport, PermissionPrint} {
		var output bytes.Buffer
		err := r.RenderPage(t.Context(), &output, 1, RenderOptions{Purpose: purpose, DPI: 12})
		if !errors.Is(err, ErrPermissionDenied) || output.Len() != 0 {
			t.Fatalf("用途%s：err=%v bytes=%d", purpose, err, output.Len())
		}
	}
	var output bytes.Buffer
	if err := r.RenderPDF(t.Context(), &output, []int{1}, RenderOptions{}); !errors.Is(err, ErrPermissionDenied) || output.Len() != 0 {
		t.Fatalf("到期PDF：%v", err)
	}
	snapshot, err := r.Permissions()
	if err != nil || snapshot[1].Accessible || snapshot[1].AccessReason != permission.Expired || r.PageCount() != 2 {
		t.Fatalf("诊断快照应可读取：%+v %v", snapshot, err)
	}
}

func TestPermissionPeriodCheckedBetweenBatchPagesAndBeforeVersionPublish(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).UnixNano())
	r, err := Open(periodPermissionsPackage(t), OpenOptions{PermissionPolicy: permission.Policy{Now: func() time.Time { return time.Unix(0, clock.Load()) }}})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.SelectVersion(0, "old"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("未生效历史版本被发布：%v", err)
	}
	if err := r.CheckPermission(PermissionExport, []int{0}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("版本失败破坏原权限：%v", err)
	}
	calls := 0
	err = r.RenderPages(t.Context(), []int{1, 1}, RenderOptions{DPI: 12}, func(_ context.Context, _, _ int, _ []byte) error {
		calls++
		clock.Store(time.Date(2026, 1, 2, 0, 0, 1, 0, time.UTC).UnixNano())
		return nil
	})
	if !errors.Is(err, ErrPermissionDenied) || calls != 1 {
		t.Fatalf("长任务跨有效期继续输出：calls=%d %v", calls, err)
	}
}
