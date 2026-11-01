package i18n

import (
	"reflect"
	"strings"
	"testing"
)

// sameInEveryLanguage lists the texts that are symbols, protocol names or
// example values, which read the same in every language.
var sameInEveryLanguage = map[string]bool{
	"FontPickerSampleSymbols": true, "ProxySOCKS5": true, "ProxyHTTP": true, "ProtocolSSH": true, "ProtocolTelnet": true,
	"FieldDevicePlaceholder": true, "FieldLinePlaceholder": true, "FlowHardware": true, "FlowSoftware": true,
	"AlgoCustomPlaceholder": true, "RemoteColPID": true, "RemoteColCPU": true, "RemoteColID": true, "RemoteTabCompose": true,
	"CatMCP": true, "SyncConfigure": true, "SyncConfigureS3": true, "SyncConfigureGist": true,
	"FilesEditorVSCode": true, "FilesEditorZed": true,
}

// The terminal tools' texts sit in a nested struct that the catalog-wide test
// does not look into.
func TestToolsCatalogIsCompleteInBothLanguages(t *testing.T) {
	english, chinese := reflect.ValueOf(For(English).Tools), reflect.ValueOf(For(Chinese).Tools)
	for index := 0; index < english.NumField(); index++ {
		name := english.Type().Field(index).Name
		en, zh := english.Field(index).String(), chinese.Field(index).String()
		if strings.TrimSpace(en) == "" || strings.TrimSpace(zh) == "" {
			t.Errorf("%s is empty: %q / %q", name, en, zh)
			continue
		}
		if strings.Count(en, "%") != strings.Count(zh, "%") {
			t.Errorf("%s: format verbs differ between %q and %q", name, en, zh)
		}
		if !hasHan(zh) && !sameInEveryLanguage[name] {
			t.Errorf("%s: %q has no Chinese text", name, zh)
		}
	}
}

func TestDesktopCatalogIsCompleteInBothLanguages(t *testing.T) {
	english, chinese := reflect.ValueOf(For(English).Desktop), reflect.ValueOf(For(Chinese).Desktop)
	for index := 0; index < english.NumField(); index++ {
		name := english.Type().Field(index).Name
		en, zh := english.Field(index).String(), chinese.Field(index).String()
		if strings.TrimSpace(en) == "" || strings.TrimSpace(zh) == "" {
			t.Errorf("%s is empty: %q / %q", name, en, zh)
			continue
		}
		if strings.Count(en, "%") != strings.Count(zh, "%") {
			t.Errorf("%s: format verbs differ between %q and %q", name, en, zh)
		}
		same := map[string]bool{"ProtocolVNC": true, "ProtocolRDP": true, "StripTLS": true, "Actual": true, "CtrlAltDel": true, "StripSizeFmt": true}
		if !hasHan(zh) && !same[name] {
			t.Errorf("%s: %q has no Chinese text", name, zh)
		}
	}
}
