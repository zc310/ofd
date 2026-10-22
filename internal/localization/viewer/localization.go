// Package viewerlocalization 提供 OFD Viewer 使用的本地化消息。
package viewerlocalization

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/zc310/ofd/internal/localization"
)

//go:embed locales/en.json
var localeFiles embed.FS

type Localizer struct {
	language string
	english  map[string]string
}

func New(languageTag string) (*Localizer, error) {
	data, err := localeFiles.ReadFile("locales/en.json")
	if err != nil {
		return nil, fmt.Errorf("read viewer locale: %w", err)
	}
	english := make(map[string]string)
	if err := json.Unmarshal(data, &english); err != nil {
		return nil, fmt.Errorf("parse viewer locale: %w", err)
	}
	return &Localizer{language: localization.CanonicalLanguage(languageTag), english: english}, nil
}

func (l *Localizer) Language() string { return l.language }

func (l *Localizer) Message(source string, _ any) string {
	if l.language == "en" {
		if translation, ok := l.english[source]; ok {
			return translation
		}
	}
	return source
}
