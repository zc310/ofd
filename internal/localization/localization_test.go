package localization

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
	unsupported, err := New("fr")
	if err != nil {
		t.Fatal(err)
	}
	if got := unsupported.Language(); got != "zh-CN" {
		t.Fatalf("unsupported language fallback: %q", got)
	}
}
