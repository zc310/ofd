package main

import (
	_ "embed"
	"encoding/json"
	"sync/atomic"
)

// Chinese source messages are stable lookup keys; translations are embedded so
// desktop, Android and Web packages all carry the same catalog.
//
//go:embed locales/en.json
var englishCatalogJSON []byte

var englishCatalog = func() map[string]string {
	var messages map[string]string
	if err := json.Unmarshal(englishCatalogJSON, &messages); err != nil {
		panic(err)
	}
	return messages
}()

// File pickers and export workers also translate messages.
var useEnglish atomic.Bool

func tr(message string) string {
	if useEnglish.Load() {
		if translated, ok := englishCatalog[message]; ok {
			return translated
		}
	}
	return message
}
