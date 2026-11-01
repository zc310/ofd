package export_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/manifest"
	ofdexport "github.com/zc310/ofd/internal/manifest/export"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/merge"
	"github.com/zc310/ofd/pkg/permission"
)

func permissionFixture(t *testing.T, value *creator.Permissions) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "permissions", Pages: []creator.Page{{}}, Permissions: value,
		Attachments: []creator.Attachment{{ID: "note", Name: "note", FileName: "note.txt", Data: []byte("private")}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestManifestExportDeniesBeforeAssetsAndOutput(t *testing.T) {
	denied := false
	for _, value := range []*creator.Permissions{{Export: &denied}, {ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}}} {
		data := permissionFixture(t, value)
		for _, format := range []string{"yaml", "json"} {
			var output bytes.Buffer
			root := filepath.Join(t.TempDir(), "assets")
			err := ofdexport.WriteManifest(data, &output, ofdexport.Options{AssetRoot: root, Format: format})
			if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
				t.Fatalf("拒绝前写出manifest：%v", err)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("拒绝前创建资源目录：%v", err)
			}
		}
		calls := 0
		opts := ofdexport.Options{AssetSink: func(string, []byte) error { calls++; return nil }}
		if _, err := ofdexport.BuildManifest(data, 0, opts); !errors.Is(err, permission.ErrDenied) || calls != 0 {
			t.Fatalf("拒绝前调用AssetSink：%v calls=%d", err, calls)
		}
		opts.PermissionPolicy.Mode = permission.IgnoreDeclarations
		if _, err := ofdexport.BuildManifest(data, 0, opts); err != nil || calls == 0 {
			t.Fatalf("明确授权未传递：%v calls=%d", err, calls)
		}
	}
}

func TestManifestBundlePreflightsAllButSingleExportUsesSelectedScope(t *testing.T) {
	denied := false
	var input bytes.Buffer
	if err := merge.Bytes([][]byte{permissionFixture(t, nil), permissionFixture(t, &creator.Permissions{Export: &denied})}, &input, merge.Options{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "bundle")
	if err := ofdexport.WriteBundle(input.Bytes(), root, ofdexport.Options{}); !errors.Is(err, permission.ErrDenied) {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("拒绝后仍发布目录：%v", err)
	}
	var output bytes.Buffer
	if err := ofdexport.WriteDocumentManifest(input.Bytes(), 0, &output, ofdexport.Options{AssetRoot: t.TempDir()}); err != nil {
		t.Fatalf("未选文档体阻止导出：%v", err)
	}
	ofd, err := parser.NewOFD(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	calls := 0
	_, err = ofdexport.BuildManifestFromOFD(ofd, 1, ofdexport.Options{AssetSink: func(string, []byte) error { calls++; return nil }})
	if !errors.Is(err, permission.ErrDenied) || calls != 0 {
		t.Fatalf("已解析入口绕过拒绝：%v", err)
	}
}

func TestManifestRechecksPeriodAfterAssetSink(t *testing.T) {
	end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	now := end
	data := permissionFixture(t, &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: end}})
	_, err := ofdexport.BuildManifest(data, 0, ofdexport.Options{PermissionPolicy: permission.Policy{Now: func() time.Time { return now }}, AssetSink: func(string, []byte) error { now = end.Add(time.Second); return nil }})
	if !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("资源回调后到期仍成功：%v", err)
	}
}

func TestManifestPermissionPeriodSurvivesSingleEndpointRoundTrip(t *testing.T) {
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, period := range []*creator.ValidPeriod{{Start: date}, {End: date}, {}} {
		data := permissionFixture(t, &creator.Permissions{ValidPeriod: period})
		for _, format := range []string{"json", "yaml"} {
			var output bytes.Buffer
			err := ofdexport.WriteManifest(data, &output, ofdexport.Options{AssetRoot: t.TempDir(), Format: format, PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}})
			if err != nil {
				t.Fatal(err)
			}
			value, err := manifest.Parse(output.Bytes(), format)
			if err != nil {
				t.Fatal(err)
			}
			result, err := value.BuildWithOptions("", "", manifest.BuildOptions{LoadAsset: func(string) ([]byte, error) { return []byte("private"), nil }})
			if err != nil {
				t.Fatal(err)
			}
			if result.Permissions == nil || result.Permissions.ValidPeriod == nil || !result.Permissions.ValidPeriod.Start.Equal(period.Start) || !result.Permissions.ValidPeriod.End.Equal(period.End) {
				t.Fatalf("有效期端点丢失：%+v", result.Permissions)
			}
		}
	}
}
