// Package comparelocalization 提供 OFD 对比报告使用的本地化消息。
package comparelocalization

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/zc310/ofd/internal/localization"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localeFiles embed.FS

type Localizer struct {
	localizer *i18n.Localizer
	language  string
}

func New(languageTag string) (*Localizer, error) {
	bundle := i18n.NewBundle(language.Chinese)
	for _, name := range []string{"zh-CN", "en"} {
		data, err := localeFiles.ReadFile("locales/" + name + ".json")
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read compare locale %s: %w", name, err)
		}
		if _, err := bundle.ParseMessageFileBytes(data, name+".json"); err != nil {
			return nil, fmt.Errorf("parse compare locale %s: %w", name, err)
		}
	}
	name := localization.CanonicalLanguage(languageTag)
	return &Localizer{localizer: i18n.NewLocalizer(bundle, name), language: name}, nil
}

func (l *Localizer) Language() string { return l.language }

func (l *Localizer) Message(id string, data any) string {
	value, err := l.localizer.Localize(&i18n.LocalizeConfig{MessageID: id, TemplateData: data})
	if err != nil {
		return id
	}
	return value
}
