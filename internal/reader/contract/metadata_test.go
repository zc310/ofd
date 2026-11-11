package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/reader"
)

func TestPermissionContractIncludesPagesAndReasons(t *testing.T) {
	value := reader.DocumentPermissions{Scope: 2, Pages: []int{1, 3}, AccessReason: permission.Reason("expired"), Export: true, PrintCopies: -1}
	result := Permissions([]reader.DocumentPermissions{value})[0]
	if result.Scope != 2 || result.AccessReason != "expired" || !result.Export || result.PrintCopies != -1 {
		t.Fatalf("权限契约 = %+v", result)
	}
	value.Pages[0] = 9
	if !reflect.DeepEqual(result.Pages, []int{1, 3}) {
		t.Fatalf("页码快照 = %v", result.Pages)
	}
	data, err := json.Marshal(Permissions([]reader.DocumentPermissions{{}})[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["pages"].([]any); !ok {
		t.Fatalf("空页码必须是数组: %s", data)
	}
}

func TestFontContractPreservesTemplateAndStyle(t *testing.T) {
	font := Fonts([]reader.FontInfo{{ID: 7, Scope: 2, TemplateID: 3, Name: "字体", FixedWidth: true, Embedded: true}})[0]
	if font.ID != 7 || font.Scope != 2 || font.TemplateID != 3 || font.Name != "字体" || !font.FixedWidth || !font.Embedded {
		t.Fatalf("字体契约 = %+v", font)
	}
	if Permissions(nil) == nil || Fonts(nil) == nil {
		t.Fatal("空列表不能为 nil")
	}
}
