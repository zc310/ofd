package converter

import (
	"bytes"
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/merge"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/spec"
)

func permissionInput(t *testing.T, value *creator.Permissions, pages int) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "permissions", Pages: make([]creator.Page, pages), Permissions: value})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConvertersDenyBeforeOutputAcrossFormatsAndParsedInputs(t *testing.T) {
	denied := false
	data := permissionInput(t, &creator.Permissions{Export: &denied}, 1)
	for _, format := range []string{"text", "markdown", "docx", "html", "pdf", "png", "svg", "tiff"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			err := Encode(t.Context(), format, data, &output, DPI(10))
			if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
				t.Fatalf("拒绝前写出%s：%v bytes=%d", format, err, output.Len())
			}
		})
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	var output bytes.Buffer
	if err := Text(t.Context(), ofd.Documents, &output); !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("已解析文字入口绕过权限：%v", err)
	}
	if err := Markdown(t.Context(), ofd.Documents, &output); !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("已解析 Markdown 入口绕过权限：%v", err)
	}
	document := render.NewDocument(nil, ofd.Documents[0], 0)
	defer document.Close()
	if err := Encode(t.Context(), "pdf", []*render.Document{document}, &output); !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("已解析渲染入口绕过权限：%v", err)
	}
	if err := Text(t.Context(), data, &output, WithPermissionPolicy(permission.Policy{Mode: permission.IgnoreDeclarations})); err != nil {
		t.Fatal(err)
	}
}

func TestConverterPreflightChecksOnlySelectedScopesAndNoWriterOnDenial(t *testing.T) {
	denied := false
	allowed := permissionInput(t, nil, 1)
	restricted := permissionInput(t, &creator.Permissions{Export: &denied}, 1)
	var input bytes.Buffer
	// 构造受限多文档样本需显式授权，不能让fixture准备绕过新合并门控。
	if err := merge.Bytes([][]byte{allowed, restricted}, &input, merge.Options{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	options := make([]Option, 0, 3)
	options = append(options, DPI(10), ImageWriter(func(int, image.Image) error { calls++; return nil }))
	if err := Image(t.Context(), input.Bytes(), options...); !errors.Is(err, permission.ErrDenied) || calls != 0 {
		t.Fatalf("未在首个输出前预检全部页：%v calls=%d", err, calls)
	}
	options = append(options, Page(1))
	if err := Image(t.Context(), input.Bytes(), options...); err != nil || calls != 1 {
		t.Fatalf("无关文档体阻止选页：%v calls=%d", err, calls)
	}
}

func TestConverterRechecksExpiryBetweenPagesAndPreservesCancellation(t *testing.T) {
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := end
	data := permissionInput(t, &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}}, 2)
	calls := 0
	err := Image(t.Context(), data, DPI(10), WithPermissionPolicy(permission.Policy{Now: func() time.Time { return now }}), ImageWriter(func(int, image.Image) error { calls++; now = end.Add(time.Second); return nil }))
	if !errors.Is(err, permission.ErrDenied) || calls != 1 {
		t.Fatalf("到期后继续输出页面：%v calls=%d", err, calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	if err := Text(ctx, data, &output); err != context.Canceled { //nolint:errorlint // 保护转换链原样返回取消错误，不只检查可识别性。
		t.Fatalf("取消错误未原样返回：%v", err)
	}
}

func TestConverterKeepsOriginalPermissionDeclarationAfterParsedInputChanges(t *testing.T) {
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := end
	ofd, err := parser.NewOFD(permissionInput(t, &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}}, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	document := render.NewDocument(nil, ofd.Documents[0], 0)
	defer document.Close()
	c := &Converter{permissionPolicy: permission.Policy{Now: func() time.Time { return now }}}
	if err := c.preparePermissions([]*render.Document{document}); err != nil {
		t.Fatal(err)
	}
	// 迁移前归一化结构按值保存期限；迁移后不能因指针别名丢失这一保护。
	document.Permissions.ValidPeriod.EndDate = spec.DateTime{Time: end.Add(time.Hour)}
	now = end.Add(time.Second)
	if err := c.checkPermissions(); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("外部修改放宽了原始期限：%v", err)
	}
}
