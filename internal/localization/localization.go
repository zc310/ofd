// Package localization 为 OFD 各模块提供共享的本地化消息目录。
package localization

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localeFiles embed.FS

type catalog struct {
	bundle *i18n.Bundle
}

type Localizer struct {
	localizer *i18n.Localizer
	language  string
}

func New(languageTag string) (*Localizer, error) {
	catalog, err := loadCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.newLocalizer(languageTag), nil
}

func loadCatalog() (*catalog, error) {
	bundle := i18n.NewBundle(language.Chinese)
	for _, name := range []string{"zh-CN", "en"} {
		data, err := localeFiles.ReadFile("locales/" + name + ".json")
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read locale %s: %w", name, err)
		}
		_, err = bundle.ParseMessageFileBytes(data, name+".json")
		if err != nil {
			return nil, fmt.Errorf("parse locale %s: %w", name, err)
		}
	}
	translations, err := localeFiles.ReadFile("locales/viewer.en.json")
	if err != nil {
		return nil, fmt.Errorf("read viewer locale: %w", err)
	}
	var english map[string]string
	err = json.Unmarshal(translations, &english)
	if err != nil {
		return nil, fmt.Errorf("parse viewer locale: %w", err)
	}
	for source, translation := range english {
		id := "viewer." + source
		err = bundle.AddMessages(language.MustParse("zh-CN"), &i18n.Message{ID: id, Other: source})
		if err != nil {
			return nil, fmt.Errorf("register viewer source message: %w", err)
		}
		err = bundle.AddMessages(language.English, &i18n.Message{ID: id, Other: translation})
		if err != nil {
			return nil, fmt.Errorf("register viewer translation: %w", err)
		}
	}
	return &catalog{bundle: bundle}, nil
}

func (c *catalog) newLocalizer(languageTag string) *Localizer {
	name := CanonicalLanguage(languageTag)
	return &Localizer{localizer: i18n.NewLocalizer(c.bundle, name), language: name}
}

func (l *Localizer) Language() string { return l.language }

func (l *Localizer) Message(id string, data any) string {
	value, err := l.localizer.Localize(&i18n.LocalizeConfig{MessageID: id, TemplateData: data})
	if err != nil {
		return id
	}
	return value
}

func CanonicalLanguage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "zh-CN"
	}
	tag, err := language.Parse(value)
	if err != nil {
		return "zh-CN"
	}
	base, _, _ := tag.Raw()
	englishBase, _, _ := language.English.Raw()
	if base == englishBase {
		return "en"
	}
	return "zh-CN"
}
