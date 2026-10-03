package spec

import "testing"

// TestDocTypeValuesAreDistinct 保护各 profile 的 DocType 取值互不相同——
// 这些字面量直接写进 OFD.xml，一旦重复或改动，profile 判定会静默串档。
func TestDocTypeValuesAreDistinct(t *testing.T) {
	seen := make(map[string]bool, len(DocTypes))
	for _, value := range DocTypes {
		if value == "" {
			t.Fatal("DocTypes 含空取值")
		}
		if seen[value] {
			t.Fatalf("DocType 取值重复: %q", value)
		}
		seen[value] = true
		if !IsDocType(value) {
			t.Errorf("IsDocType(%q) = false, want true", value)
		}
	}
	if len(DocTypes) != 3 {
		t.Fatalf("DocTypes 长度 = %d, want 3", len(DocTypes))
	}
	if DocTypes[0] != DocTypeOFD {
		t.Errorf("DocTypes 首位 = %q, want %q（基础 profile 应排在最前）", DocTypes[0], DocTypeOFD)
	}
}

// TestIsDocTypeRejectsUnknown 保护未知取值的拒绝，包括大小写错误与相似串。
func TestIsDocTypeRejectsUnknown(t *testing.T) {
	for _, value := range []string{"", "ofd", "ofd-a", "ofd-h", "OFD_A", "OFD-A ", "OFD-Archive"} {
		if IsDocType(value) {
			t.Errorf("IsDocType(%q) = true, want false", value)
		}
	}
}
