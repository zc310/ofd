package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image/color"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func preserveLanguage(t *testing.T) {
	t.Helper()
	previous := useEnglish.Load()
	t.Cleanup(func() { useEnglish.Store(previous) })
}

func TestLanguageUsesSavedChoiceBeforeSystemLocale(t *testing.T) {
	a := test.NewTempApp(t)
	for _, tc := range []struct{ saved, locale, want string }{
		{"", "zh-CN", "zh"}, {"", "zh_TW", "zh"}, {"", "en-US", "en"},
		{"", "fr-FR", "en"}, {"", "", "en"},
		{"en", "zh-CN", "en"}, {"zh", "en-US", "zh"}, {"invalid", "zh-CN", "zh"},
	} {
		a.Preferences().SetString(languagePreferenceKey, tc.saved)
		if got := loadLanguage(a.Preferences(), tc.locale); got != tc.want {
			t.Errorf("saved %q, locale %q: got %q, want %q", tc.saved, tc.locale, got, tc.want)
		}
	}
}

func TestLanguageSwitchRefreshesUIAndPreservesDocumentState(t *testing.T) {
	preserveLanguage(t)
	useEnglish.Store(false)
	v := newLoadedTestViewer(t)
	v.currentPage = 1
	v.pageLayout.mode = viewDoublePage
	v.documentTitle.SetText("文档自带的标题")
	pages, operation := v.totalPages, v.operation.Load()
	for _, tc := range []struct{ code, heading, placeholder, page string }{
		{"en", "Pages", "Page", "Page 2"},
		{"zh", "页面", "页码", "第 2 页"},
	} {
		v.setLanguage(tc.code)
		if v.thumbnailHeading.Text != tc.heading || v.pageEntry.PlaceHolder != tc.placeholder {
			t.Fatalf("UI was not refreshed for %s", tc.code)
		}
		cell := newThumbnailCell(color.White)
		updateThumbnailCell(cell, 1, v)
		if cell.label.Text != tc.page {
			t.Errorf("thumbnail = %q, want %q", cell.label.Text, tc.page)
		}
		if v.currentPage != 1 || v.totalPages != pages || v.operation.Load() != operation || v.pageLayout.mode != viewDoublePage {
			t.Fatal("language switch changed document or navigation state")
		}
		if v.documentTitle.Text != "文档自带的标题" {
			t.Fatal("document title was translated")
		}
		if got := loadLanguage(fyne.CurrentApp().Preferences(), "en-US"); got != tc.code {
			t.Fatalf("saved language = %q, want %q", got, tc.code)
		}
		menu := v.languageMenuItem()
		if menu.ChildMenu.Items[0].Checked != (tc.code == "zh") || menu.ChildMenu.Items[1].Checked != (tc.code == "en") {
			t.Fatal("language menu checkmarks do not match selection")
		}
	}
	v.closeDocument()
	v.setLanguage("en")
	if v.documentTitle.Text != "No document loaded" || v.pageLabel.Text != "/ No document loaded" {
		t.Fatal("empty document labels were not translated")
	}
}

func TestLanguageSwitchDisabledDuringFileOperations(t *testing.T) {
	preserveLanguage(t)
	useEnglish.Store(false)
	v := newLoadedTestViewer(t)
	for _, state := range []string{"loading", "exporting"} {
		v.loading, v.exporting = state == "loading", state == "exporting"
		if !v.languageMenuItem().Disabled {
			t.Fatalf("language menu enabled while %s", state)
		}
		v.setLanguage("en")
		if useEnglish.Load() {
			t.Fatalf("language changed while %s", state)
		}
	}
	v.loading, v.exporting = false, false
	v.setLanguage("invalid")
	if useEnglish.Load() {
		t.Fatal("invalid language changed the current language")
	}
}

func TestTranslatedExportOptionsKeepFormatAndBackground(t *testing.T) {
	preserveLanguage(t)
	for _, english := range []bool{false, true} {
		useEnglish.Store(english)
		for _, format := range []struct{ label, code string }{
			{exportFormatPDF, "pdf"}, {exportFormatTXT, "txt"}, {exportFormatJPG, "jpg"},
			{exportFormatPNG, "png"}, {exportFormatSVG, "svg"}, {exportFormatEPS, "eps"}, {exportFormatTeX, "tex"},
		} {
			if got := exportFormatCode(tr(format.label)); got != format.code {
				t.Errorf("English %v: format %q = %q, want %q", english, tr(format.label), got, format.code)
			}
		}
		if !sameBackground(exportBackgroundColor(tr(exportBackgroundWhite)), color.White) ||
			!sameBackground(exportBackgroundColor(tr(exportBackgroundTransparent)), color.Transparent) {
			t.Fatal("translated background options changed export colors")
		}
	}
}

func TestTranslationCatalogCoversMessagesAndPreservesFormatArguments(t *testing.T) {
	verbs := regexp.MustCompile(`%[dswv]`)
	for source, translation := range englishCatalog {
		if strings.TrimSpace(translation) == "" {
			t.Errorf("empty translation for %q", source)
		}
		if !reflect.DeepEqual(verbs.FindAllString(source, -1), verbs.FindAllString(translation, -1)) {
			t.Errorf("format arguments differ: %q -> %q", source, translation)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			function, ok := call.Fun.(*ast.Ident)
			if !ok || function.Name != "tr" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if ok && literal.Kind == token.STRING {
				source, _ := strconv.Unquote(literal.Value)
				if _, exists := englishCatalog[source]; !exists {
					t.Errorf("%s: missing translation for %q", name, source)
				}
			}
			return true
		})
	}
}
