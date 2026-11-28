package workbench

import (
	"errors"
	"fmt"
	"motty/internal/configstore"
	"strings"

	"github.com/google/uuid"
)

const customSchemePrefix = "custom:"

// 自定义主题使用独立 ID，改名不会改变引用，也不会覆盖导入的主题。
type savedCustomScheme struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Background      [3]uint8     `json:"background"`
	Foreground      [3]uint8     `json:"foreground"`
	Cursor          [3]uint8     `json:"cursor"`
	ANSI            [16][3]uint8 `json:"ansi"`
	Selection       [3]uint8     `json:"selection"`
	SelectionText   [3]uint8     `json:"selection_text"`
	CustomSelection bool         `json:"custom_selection,omitempty"`
	Opacity         *int         `json:"opacity,omitempty"`
}

func (state *State) savedCustomSchemes() []savedCustomScheme {
	result := make([]savedCustomScheme, 0, len(state.managedSchemes))
	for _, s := range state.managedSchemes {
		result = append(result, savedCustomScheme{ID: s.id, Name: s.name, Background: s.background,
			Foreground: s.foreground, Cursor: s.cursor, ANSI: s.ansi, Selection: s.selection,
			SelectionText: s.selectionText, CustomSelection: s.customSelection, Opacity: s.opacity})
	}
	return result
}

func (state *State) restoreCustomSchemes(saved []savedCustomScheme) {
	state.managedSchemes = nil
	seen := map[string]bool{}
	for _, s := range saved {
		if !strings.HasPrefix(s.ID, customSchemePrefix) || seen[s.ID] || strings.TrimSpace(s.Name) == "" {
			continue
		}
		seen[s.ID] = true
		state.managedSchemes = append(state.managedSchemes, colorScheme{id: s.ID, name: s.Name,
			background: s.Background, foreground: s.Foreground, cursor: s.Cursor, ansi: s.ANSI,
			selection: s.Selection, selectionText: s.SelectionText, customSelection: s.CustomSelection,
			opacity: cloneSchemeOpacity(s.Opacity)})
	}
}

func cloneSchemeOpacity(value *int) *int {
	if value == nil {
		return nil
	}
	copy := max(0, min(100, *value))
	return &copy
}

func (state *State) saveCustomScheme(s colorScheme) (string, error) {
	s.name = strings.TrimSpace(s.name)
	if s.name == "" {
		return "", fmt.Errorf("theme name is required")
	}
	if len([]rune(s.name)) > 80 {
		return "", fmt.Errorf("theme name must be at most 80 characters")
	}
	for _, other := range state.managedSchemes {
		if other.id != s.id && strings.EqualFold(other.name, s.name) {
			return "", fmt.Errorf("theme name already exists")
		}
	}
	if !strings.HasPrefix(s.id, customSchemePrefix) {
		s.id = customSchemePrefix + uuid.NewString()
	}
	s.opacity = cloneSchemeOpacity(s.opacity)
	for i := range state.managedSchemes {
		if state.managedSchemes[i].id == s.id {
			state.managedSchemes[i] = s
			return s.id, nil
		}
	}
	state.managedSchemes = append(state.managedSchemes, s)
	return s.id, nil
}

func (state *State) deleteCustomScheme(id string) error {
	before := state.savedCustomSchemes()
	selected := state.terminalSchemes
	application := state.applicationTheme
	for i, s := range state.managedSchemes {
		if s.id != id {
			continue
		}
		state.managedSchemes = append(state.managedSchemes[:i], state.managedSchemes[i+1:]...)
		for look, selected := range state.terminalSchemes {
			if selected == id {
				state.terminalSchemes[look] = defaultSchemeID
			}
		}
		state.refreshApplicationTheme()
		if err := state.saveWithResultModeRecovery(true, false, true); err != nil {
			state.restoreAfterThemeSave(before, selected, application, err)
			return err
		}
		return nil
	}
	return nil
}

// 主题编辑不能采用通用的“共享配置优先”恢复，否则冲突会被误报为保存成功。
// 冲突时读回最新配置，但保留编辑器草稿，用户可以继续保存。
func (state *State) restoreAfterThemeSave(before []savedCustomScheme, schemes [2]string, application applicationTheme, err error) {
	state.restoreCustomSchemes(before)
	state.terminalSchemes = schemes
	state.applicationTheme = application
	if errors.Is(err, configstore.ErrConflict) {
		_ = state.refreshShared()
	}
	state.refreshApplicationTheme()
}
