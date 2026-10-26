package workbench

import (
	"net/url"
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/store"
	"motty/internal/i18n"
	"motty/internal/workbench/icons"
)

// aiPreset is a provider the add dialog can fill in: its name, which API it
// speaks and its address. The last one is the custom entry, which fills in
// nothing.
type aiPreset struct {
	label string
	icon  ui.Icon
	kind  string
	url   string
	// urlZH is the address for the Chinese interface when the vendor runs a
	// separate endpoint for mainland China.
	urlZH string
}

var aiPresets = [...]aiPreset{
	{label: "Claude", icon: icons.AiClaude, kind: store.KindAnthropic, url: "https://api.anthropic.com"},
	{label: "OpenAI", icon: icons.AiOpenai, kind: store.KindOpenAI, url: "https://api.openai.com/v1"},
	{label: "Gemini", icon: icons.AiGemini, kind: store.KindOpenAI, url: "https://generativelanguage.googleapis.com/v1beta/openai"},
	{label: "DeepSeek", icon: icons.AiDeepseek, kind: store.KindOpenAI, url: "https://api.deepseek.com/v1"},
	{label: "Kimi", icon: icons.AiKimi, kind: store.KindOpenAI, url: "https://api.moonshot.cn/v1"},
	{label: "Qwen", icon: icons.AiQwen, kind: store.KindOpenAI, url: "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	{label: "Zhipu", icon: icons.AiZai, kind: store.KindOpenAI, url: "https://api.z.ai/api/paas/v4", urlZH: "https://open.bigmodel.cn/api/paas/v4"},
	{label: "MiniMax", icon: icons.AiMinimax, kind: store.KindOpenAI, url: "https://api.minimax.io/v1", urlZH: "https://api.minimaxi.com/v1"},
	{label: "Ollama", icon: icons.AiOllama, kind: store.KindOpenAI, url: "http://localhost:11434/v1"},
	{icon: icons.Plug, kind: store.KindOpenAI}, // custom
}

const aiPresetCustom = len(aiPresets) - 1

func (preset aiPreset) address(language i18n.Language) string {
	if language == i18n.Chinese && preset.urlZH != "" {
		return preset.urlZH
	}
	return preset.url
}

// aiPresetLabel is what the preset is called in the list.
func (state *State) aiPresetLabel(index int) string {
	if index == aiPresetCustom {
		return state.messages.AISettings.FormPresetCustom
	}
	return aiPresets[index].label
}

// aiChoosePreset fills the form from a preset. The name follows the preset
// only while it is still what a preset put there, so a name typed by hand
// stays.
func (state *State) aiChoosePreset(index int) {
	cfg := &state.ai.cfg
	if index < 0 || index >= len(aiPresets) {
		return
	}
	preset := aiPresets[index]
	if name := cfg.name.String(); name == "" || name == state.aiPresetName(cfg.preset) {
		cfg.name.SetSelection(text.Range{Start: 0, End: cfg.name.RuneCount()})
		cfg.name.Insert(state.aiPresetName(index))
	}
	cfg.baseURL.SetSelection(text.Range{Start: 0, End: cfg.baseURL.RuneCount()})
	cfg.baseURL.Insert(preset.address(state.language))
	cfg.kind, cfg.preset, cfg.presetOpen = preset.kind, index, false
}

// aiPresetName is the name a preset gives a provider: none for the custom one.
func (state *State) aiPresetName(index int) string {
	if index == aiPresetCustom {
		return ""
	}
	return aiPresets[index].label
}

// aiPresetOf is the preset a saved provider points at, or aiPresetCustom. Only the
// provider's name, kind and address are stored, not which preset it began as, and
// the name is the user's to change, so the address decides: a preset's host, in
// either of its regions. No address is the official one of the provider's kind.
func aiPresetOf(provider store.Provider) int {
	if strings.TrimSpace(provider.BaseURL) == "" {
		if provider.Kind == store.KindAnthropic {
			return 0
		}
		return 1
	}
	host := aiHost(provider.BaseURL)
	for index, preset := range aiPresets {
		if index == aiPresetCustom {
			break
		}
		if host != "" && host == aiHost(preset.url) || preset.urlZH != "" && host == aiHost(preset.urlZH) {
			return index
		}
	}
	return aiPresetCustom
}

// aiHost is the lower-case host of an address, with its port, and with the
// loopback address written as localhost; empty when there is none.
func aiHost(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Host)
	return strings.Replace(host, "127.0.0.1", "localhost", 1)
}

// aiProviderIcon is the mark shown for a saved provider: its preset's, or the
// gear of the custom entry.
func aiProviderIcon(provider store.Provider) ui.Icon {
	return aiPresets[aiPresetOf(provider)].icon
}
