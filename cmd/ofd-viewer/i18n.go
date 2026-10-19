package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/lang"
)

const languagePreferenceKey = "viewer-language"

func languageForLocale(locale string) string {
	locale = strings.ToLower(strings.ReplaceAll(locale, "_", "-"))
	if locale == "zh" || strings.HasPrefix(locale, "zh-") {
		return "zh"
	}
	return "en"
}

func loadLanguage(preferences fyne.Preferences, systemLocale string) string {
	if preferences != nil {
		if saved := preferences.String(languagePreferenceKey); saved == "zh" || saved == "en" {
			return saved
		}
	}
	return languageForLocale(systemLocale)
}

func initializeLanguage(a fyne.App) {
	useEnglish.Store(loadLanguage(a.Preferences(), string(lang.SystemLocale())) == "en")
}

func (v *viewer) languageMenuItem() *fyne.MenuItem {
	item := fyne.NewMenuItem(tr("语言 / Language"), nil)
	choices := make([]*fyne.MenuItem, 0, 2)
	for _, language := range []struct{ code, label string }{{"zh", "简体中文"}, {"en", "English"}} {
		choice := fyne.NewMenuItem(language.label, func() { v.setLanguage(language.code) })
		choice.Checked = useEnglish.Load() == (language.code == "en")
		choices = append(choices, choice)
	}
	item.ChildMenu = fyne.NewMenu(item.Label, choices...)
	item.Disabled = v.loading || v.exporting
	return item
}

// setLanguage updates labels in place, keeping document sessions and render caches.
func (v *viewer) setLanguage(language string) {
	if v.closed.Load() || v.loading || v.exporting || (language != "zh" && language != "en") {
		return
	}
	useEnglish.Store(language == "en")
	if a := fyne.CurrentApp(); a != nil {
		a.Preferences().SetString(languagePreferenceKey, language)
	}
	v.pageEntry.SetPlaceHolder(tr("页码"))
	v.thumbnailHeading.SetText(tr("页面"))
	v.updateControls()
	v.thumbnailList.Refresh()
}
