package watermark

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/merge"
	"github.com/zc310/ofd/pkg/creator/replace"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/spec"
)

func TestWatermarkChecksOnlySelectedBodiesAndPreflightsWholeTarget(t *testing.T) {
	denied := false
	inputs := make([][]byte, 0, 2)
	for _, value := range []*creator.Permissions{nil, {Watermark: &denied}} {
		data, err := creator.Marshal(creator.Document{ID: "body", Pages: []creator.Page{{}}, Permissions: value})
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, data)
	}
	var input, output bytes.Buffer
	if err := merge.Bytes(inputs, &input, merge.Options{}); err != nil {
		t.Fatal(err)
	}
	err := Add(input.Bytes(), Target{Document: -1}, sampleWatermark(), &output, Options{})
	detail, ok := errors.AsType[*permission.DeniedError](err)
	if !ok || detail.Scope != 1 || output.Len() != 0 {
		t.Fatalf("全部目标未预检：%v bytes=%d", err, output.Len())
	}
	if err := Add(input.Bytes(), Target{Document: 0}, sampleWatermark(), &output, Options{}); err != nil {
		t.Fatalf("无关文档体不应阻止选定水印：%v", err)
	}
}

func TestWatermarkPermissionIsIndependentOfEditAndAnnot(t *testing.T) {
	denied := false
	data, err := creator.Marshal(creator.Document{ID: "watermark-only", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{Edit: &denied, Annot: &denied}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Add(data, Target{}, sampleWatermark(), &output, Options{}); err != nil {
		t.Fatalf("水印被Edit/Annot错误阻止：%v", err)
	}
}

func TestWatermarkLegacySkipDoesNotBypassValidity(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "expired", Pages: []creator.Page{{}}, Permissions: &creator.Permissions{ValidPeriod: &creator.ValidPeriod{End: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Add(data, Target{}, sampleWatermark(), &output, Options{SkipPermissionsCheck: true})
	if !errors.Is(err, permission.ErrDenied) || output.Len() != 0 {
		t.Fatalf("旧跳过开关绕过了有效期：%v", err)
	}
	options := Options{Options: replace.Options{PermissionPolicy: permission.Policy{Mode: permission.IgnoreDeclarations}}}
	if err := Add(data, Target{}, sampleWatermark(), &output, options); err != nil {
		t.Fatal(err)
	}
}

func TestWatermarkLegacySkipDoesNotMutateOriginalSpecDeclaration(t *testing.T) {
	denied := false
	declaration := &spec.Permissions{Watermark: &denied}
	e := &engine{permissions: map[int]*spec.Permissions{0: declaration}, options: Options{SkipPermissionsCheck: true}}
	if err := e.checkPermissions(); err != nil {
		t.Fatal(err)
	}
	if declaration.Watermark == nil || *declaration.Watermark {
		t.Fatal("旧跳过开关修改了原始水印声明")
	}
	e.options.SkipPermissionsCheck = false
	if err := e.checkPermissions(); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("豁免调用后丢失原始拒绝：%v", err)
	}
}
