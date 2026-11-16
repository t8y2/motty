package workbench

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"motty/internal/ai/store"
	"motty/internal/aiproviderimport"
)

// Importing model providers from magpie and cc-switch. The files are read only
// when the user opens this dialog; the package that parses them never touches
// the window, the vault or the network. This file holds the dialog's state, the
// match/write rules and the result the summary is painted from; ai_import_paint.go
// paints it.
//
// The candidates carry credentials in memory until the dialog closes. Closing
// the dialog, and finishing the import, drop them; the preview and the summary
// only ever ask whether a key is present.

// aiImportStep is where the import dialog is.
type aiImportStep uint8

const (
	aiImportSources aiImportStep = iota
	aiImportPreview
	aiImportDone
)

// aiImportReasonCount is one skip reason and how many candidates had it. It is a
// short list rather than a map so the summary paints in a stable order.
type aiImportReasonCount struct {
	Reason string
	Count  int
}

// aiImportResult is what the summary shows. It never holds a credential or an
// endpoint's user information.
type aiImportResult struct {
	Created   int
	Updated   int
	Skipped   int
	Failed    int
	KeyFailed int
	// GatewayCreated says the loopback magpie gateway was among the new
	// providers, so the summary can say magpie has to be running.
	GatewayCreated bool
	Reasons        []aiImportReasonCount
	Err            string
}

func (result *aiImportResult) addReason(reason string) {
	for index := range result.Reasons {
		if result.Reasons[index].Reason == reason {
			result.Reasons[index].Count++
			return
		}
	}
	result.Reasons = append(result.Reasons, aiImportReasonCount{Reason: reason, Count: 1})
}

func (result *aiImportResult) fail(message string) {
	result.Failed++
	if result.Err == "" {
		result.Err = message
	}
}

// aiOpenImport opens the source step and detects the standard locations. This
// is the only place detection runs: opening Settings and painting it never
// reads another application's data.
func (state *State) aiOpenImport() {
	if _, err := state.aiStore(); err != nil {
		return
	}
	cfg := &state.ai.cfg
	state.aiCloseDialog()
	cfg.dialog, cfg.importStep = aiDialogImport, aiImportSources
	cfg.importSources = state.aiProviderScan()
}

// aiProviderScan reads the standard locations. Tests replace the seam with
// temporary sources and a counter, which proves the paint path never calls it.
func (state *State) aiProviderScan() []aiproviderimport.SourceInfo {
	if state.options.AIProviderScan != nil {
		return state.options.AIProviderScan()
	}
	return aiproviderimport.Scan()
}

// aiImportUseSource previews one detected source.
func (state *State) aiImportUseSource(index int) {
	cfg := &state.ai.cfg
	if index < 0 || index >= len(cfg.importSources) || !cfg.importSources[index].Available() {
		return
	}
	state.aiImportShowPreview(cfg.importSources[index].Candidates)
}

// aiImportChooseFile asks for a file and identifies it by content, so a
// portable copy or a non-standard data directory still works.
func (state *State) aiImportChooseFile() {
	cfg, m := &state.ai.cfg, state.messages.AISettings
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: m.ImportChooseTitle})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		cfg.importErr = state.aiScrub(fmt.Sprintf(m.ImportFileFailedFmt, err), "")
		return
	}
	candidates, err := aiproviderimport.ParseFile(paths[0])
	if err != nil {
		cfg.importErr = state.aiScrub(fmt.Sprintf(m.ImportFileFailedFmt, err), "")
		return
	}
	state.aiImportShowPreview(candidates)
}

func (state *State) aiImportShowPreview(candidates []aiproviderimport.Candidate) {
	cfg := &state.ai.cfg
	cfg.importStep, cfg.importErr = aiImportPreview, ""
	cfg.importCandidates = aiImportOrdered(candidates)
	cfg.importSelected = make(map[int]bool, len(cfg.importCandidates))
	for index, candidate := range cfg.importCandidates {
		if candidate.Importable() {
			cfg.importSelected[index] = true
		}
	}
}

// aiImportOrdered puts what can be written first and what cannot last, keeping
// the source's order inside each group. The order is fixed when the preview
// opens, so ticking a row never moves it; the unavailable group then reads as
// one explanation block instead of noise between selectable rows.
func aiImportOrdered(candidates []aiproviderimport.Candidate) []aiproviderimport.Candidate {
	ordered := make([]aiproviderimport.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Importable() {
			ordered = append(ordered, candidate)
		}
	}
	for _, candidate := range candidates {
		if !candidate.Importable() {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}

// aiImportBack returns to the source step without reading anything again.
func (state *State) aiImportBack() {
	cfg := &state.ai.cfg
	cfg.importStep, cfg.importErr = aiImportSources, ""
	cfg.importCandidates, cfg.importSelected = nil, nil
}

func (state *State) aiImportToggle(index int) {
	cfg := &state.ai.cfg
	if index < 0 || index >= len(cfg.importCandidates) || !cfg.importCandidates[index].Importable() {
		return
	}
	cfg.importSelected[index] = !cfg.importSelected[index]
}

func (state *State) aiImportSelectedCount() int {
	count := 0
	for index := range state.ai.cfg.importSelected {
		if state.ai.cfg.importSelected[index] {
			count++
		}
	}
	return count
}

// aiImportImportableCount is how many rows the preview offers, regardless of
// what is currently ticked.
func aiImportImportableCount(candidates []aiproviderimport.Candidate) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.Importable() {
			count++
		}
	}
	return count
}

func (state *State) aiImportSelectedCandidates() []aiproviderimport.Candidate {
	cfg := &state.ai.cfg
	var selected []aiproviderimport.Candidate
	for index, candidate := range cfg.importCandidates {
		if cfg.importSelected[index] {
			selected = append(selected, candidate)
		}
	}
	return selected
}

// aiImportApply writes the selected candidates. It runs synchronously in the
// click handler, like the other imports: the reads already happened, the store
// is local, and the write set is bounded. A locked vault is opened first so a
// credential is never dropped without asking.
func (state *State) aiImportApply() {
	cfg := &state.ai.cfg
	if cfg.dialog != aiDialogImport || cfg.importStep != aiImportPreview {
		return
	}
	selected := state.aiImportSelectedCandidates()
	if len(selected) == 0 {
		return
	}
	if state.aiImportNeedsUnlock(selected) {
		state.openVaultUnlock(nil, func() { state.aiImportApply() })
		return
	}
	db, err := state.aiStore()
	if err != nil {
		result := aiImportResult{Err: state.aiScrub(err.Error(), "")}
		cfg.importResult, cfg.importStep = result, aiImportDone
		cfg.importCandidates, cfg.importSelected, cfg.importSources = nil, nil, nil
		return
	}

	result := aiImportResult{}
	for _, candidate := range cfg.importCandidates {
		if !candidate.Importable() {
			result.Skipped++
			result.addReason(candidate.SkipReason)
		}
	}

	providers := append([]store.Provider(nil), state.ai.providers...)
	models := make(map[string][]store.Model, len(providers))
	for _, model := range state.ai.models {
		models[model.ProviderID] = append(models[model.ProviderID], model)
	}
	hadDefault := state.aiHasDefault()
	firstProviderID, firstModel := "", ""

	for _, candidate := range selected {
		if index, ok := aiImportMatch(providers, candidate); ok {
			existing := providers[index]
			updated := false
			oldStyle := existing.AuthStyle
			keySaved := false
			if candidate.Key != "" && !existing.HasKey {
				if err := state.aiSaveKey(existing.ID, candidate.Key); err != nil {
					result.KeyFailed++
				} else {
					existing.HasKey = true
					keySaved = true
					// A provider without a key was not using its old header.
					// The imported key is only valid with the source's style,
					// so store them together instead of sending it the wrong way.
					existing.AuthStyle = candidate.AuthStyle
					if err := db.SaveProvider(existing); err != nil {
						// The key was written before the row: undo it so a failed
						// update leaves the provider exactly as it was.
						_ = state.secrets.vault.Delete(aiKeySecretName(existing.ID))
						result.fail(state.aiScrub(err.Error(), ""))
						continue
					}
					providers[index] = existing
					updated = true
				}
			}
			list, added := aiImportMergeModels(models[existing.ID], existing.ID, candidate.Models, false)
			if len(added) > 0 {
				if err := db.SaveModels(existing.ID, list); err != nil {
					// The models are the last write, so a key saved above must
					// be undone here too; otherwise the result claims nothing
					// changed while the provider kept the new credential.
					if keySaved {
						existing.HasKey = false
						existing.AuthStyle = oldStyle
						if saveErr := db.SaveProvider(existing); saveErr == nil {
							// Only drop the credential once the row no longer
							// claims it; otherwise the provider would advertise
							// a key that the vault no longer holds.
							_ = state.secrets.vault.Delete(aiKeySecretName(existing.ID))
							providers[index] = existing
						}
					}
					result.fail(state.aiScrub(err.Error(), ""))
					continue
				}
				models[existing.ID] = list
				updated = true
			}
			if updated {
				result.Updated++
			} else {
				// A re-import of the same data changes nothing; counting it as
				// skipped keeps the summary honest without inventing a fifth state.
				result.Skipped++
			}
			continue
		}

		provider := store.Provider{
			ID: newProviderID(), Name: aiImportUniqueName(providers, candidate.Name), Kind: candidate.Kind,
			BaseURL: candidate.BaseURL, AuthStyle: candidate.AuthStyle, Enabled: true,
		}
		if candidate.Key != "" {
			if err := state.aiSaveKey(provider.ID, candidate.Key); err != nil {
				result.KeyFailed++
			} else {
				provider.HasKey = true
			}
		}
		if err := db.SaveProvider(provider); err != nil {
			if provider.HasKey {
				_ = state.secrets.vault.Delete(aiKeySecretName(provider.ID))
			}
			result.fail(state.aiScrub(err.Error(), ""))
			continue
		}
		list, _ := aiImportMergeModels(nil, provider.ID, candidate.Models, true)
		if len(list) > 0 {
			if err := db.SaveModels(provider.ID, list); err != nil {
				// Do not report a provider whose models could not be written.
				// Delete the row first; only then may the key go, or a failed
				// row delete would leave the row claiming a key that is gone.
				if deleteErr := db.DeleteProvider(provider.ID); deleteErr == nil && provider.HasKey {
					_ = state.secrets.vault.Delete(aiKeySecretName(provider.ID))
				}
				result.fail(state.aiScrub(err.Error(), ""))
				continue
			}
		}
		providers = append(providers, provider)
		models[provider.ID] = list
		result.Created++
		if candidate.Source == aiproviderimport.SourceMagpieGateway {
			result.GatewayCreated = true
		}
		if firstProviderID == "" && len(candidate.Models) > 0 {
			firstProviderID, firstModel = provider.ID, candidate.Models[0]
		}
	}

	if !hadDefault && firstProviderID != "" {
		if err := db.SetDefaultModel(firstProviderID, firstModel); err != nil && result.Err == "" {
			// The providers were written; this is reported without counting an
			// entry as failed to save.
			result.Err = state.aiScrub(err.Error(), "")
		}
	}

	cfg.importResult, cfg.importStep = result, aiImportDone
	cfg.importCandidates, cfg.importSelected, cfg.importSources = nil, nil, nil
	state.aiWrote()
}

// aiImportNeedsUnlock reports whether applying the selected candidates needs a
// credential stored in a vault that is currently locked. A source key that a
// matching provider already has is not a reason to ask.
func (state *State) aiImportNeedsUnlock(selected []aiproviderimport.Candidate) bool {
	opened := state.secrets.vault
	if opened == nil || !opened.NeedsConversion() {
		return false
	}
	for _, candidate := range selected {
		if candidate.Key == "" {
			continue
		}
		if index, ok := aiImportMatch(state.ai.providers, candidate); ok && state.ai.providers[index].HasKey {
			continue
		}
		return true
	}
	return false
}

// aiImportMatch finds the provider a candidate would update: the same kind at
// the normalized endpoint under the same name. The numbered-name fallback makes
// a second import idempotent when the first had to disambiguate a name.
func aiImportMatch(providers []store.Provider, candidate aiproviderimport.Candidate) (int, bool) {
	base := aiImportBase(candidate.BaseURL)
	for index, provider := range providers {
		if provider.Kind == candidate.Kind && provider.Name == candidate.Name && aiImportBase(provider.BaseURL) == base {
			return index, true
		}
	}
	found, count := -1, 0
	for index, provider := range providers {
		if provider.Kind != candidate.Kind || aiImportBase(provider.BaseURL) != base {
			continue
		}
		if aiImportNumberedName(provider.Name, candidate.Name) {
			found, count = index, count+1
		}
	}
	if count == 1 {
		return found, true
	}
	return 0, false
}

// aiImportNumberedName reports whether name is the numbered variant a prior
// collision assigned, such as "Relay (2)". The base is truncated to the same
// 64-rune limit aiImportUniqueName uses, so a long name still matches on the
// next import instead of creating another provider.
func aiImportNumberedName(name, base string) bool {
	open := strings.LastIndex(name, " (")
	if open < 0 || !strings.HasSuffix(name, ")") {
		return false
	}
	suffix := name[open:]
	number, err := strconv.Atoi(suffix[2 : len(suffix)-1])
	if err != nil || number < 2 {
		return false
	}
	return name == truncateRunes(base, aiproviderimport.MaxNameRunes-len([]rune(suffix)))+suffix
}

// aiImportUniqueName returns desired, or the first numbered variant no existing
// provider uses. Names stay inside the provider form's 64-rune limit.
func aiImportUniqueName(providers []store.Provider, desired string) string {
	if desired == "" {
		desired = "provider"
	}
	used := make(map[string]bool, len(providers))
	for _, provider := range providers {
		used[provider.Name] = true
	}
	if !used[desired] {
		return desired
	}
	for number := 2; ; number++ {
		suffix := fmt.Sprintf(" (%d)", number)
		name := truncateRunes(desired, aiproviderimport.MaxNameRunes-len([]rune(suffix))) + suffix
		if !used[name] {
			return name
		}
	}
}

// aiImportBase normalizes an endpoint for matching: scheme and host are
// case-insensitive and a trailing slash is noise. The path is left alone
// because relays differ there and equality is what deduplication needs.
func aiImportBase(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return strings.TrimRight(parsed.String(), "/")
}

// aiImportMergeModels appends the names a provider does not list yet. A new
// provider gets its models enabled; a matched provider's added models start
// disabled so an existing model choice is never changed behind the user.
func aiImportMergeModels(existing []store.Model, providerID string, names []string, enabled bool) ([]store.Model, []string) {
	seen := make(map[string]bool, len(existing)+len(names))
	for _, model := range existing {
		seen[model.Name] = true
	}
	var added []string
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		existing = append(existing, store.Model{ProviderID: providerID, Name: name, Enabled: enabled})
		added = append(added, name)
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].Name < existing[j].Name })
	return existing, added
}

// aiImportHost is the endpoint host shown in the preview. It never includes
// credentials, and a malformed URL falls back to the raw text the parser
// rejected only when that text is short enough to paint.
func aiImportHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return ""
}
