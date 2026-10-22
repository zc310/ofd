package localization

import "testing"

func TestCanonicalLanguage(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"en-US", "en"},
		{"zh-CN", "zh-CN"},
		{"zh_TW", "zh-CN"},
		{"fr", "zh-CN"},
		{"", "zh-CN"},
	} {
		if got := CanonicalLanguage(tc.input); got != tc.want {
			t.Errorf("CanonicalLanguage(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
