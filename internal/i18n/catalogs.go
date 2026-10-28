package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

// Catalogs are bundled with the application. Switching a language never needs
// a network request; each additional catalog is decoded only on its first use.
//
//go:embed locales
var catalogFiles embed.FS

var catalogCache [LanguageCount]struct {
	once     sync.Once
	messages Messages
}

func translatedCatalog(language Language) Messages {
	entry := &catalogCache[language]
	entry.once.Do(func() {
		data, err := catalogFiles.ReadFile("locales/" + language.String() + ".json")
		if err == nil {
			err = json.Unmarshal(data, &entry.messages)
		}
		if err != nil {
			panic(fmt.Sprintf("i18n: invalid bundled %s catalog: %v", language, err))
		}
	})
	return entry.messages
}
