package viewerlocalization

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
	if got := english.Message("页面", nil); got != "Pages" {
		t.Fatalf("English message = %q", got)
	}
	if got := chinese.Message("页面", nil); got != "页面" {
		t.Fatalf("Chinese message = %q", got)
	}
}
