package comparelocalization

import "testing"

func TestLocalizersKeepLanguagePreferencesIndependent(t *testing.T) {
	english, err := New("en-US")
	if err != nil {
		t.Fatal(err)
	}
	chinese, err := New("zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if english.Language() != "en" || chinese.Language() != "zh-CN" {
		t.Fatalf("canonical languages: %q, %q", english.Language(), chinese.Language())
	}
	if got := chinese.Message("compare.page_match.heading", nil); got != "页面配对" {
		t.Fatalf("Chinese message = %q", got)
	}
}
