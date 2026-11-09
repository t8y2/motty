package workbench

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/completion"
	"motty/internal/configstore"
	"motty/internal/i18n"
)

func TestInlineSettingDisablesGhostButKeepsManualCompletion(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git statu")
	if _, ok := w.state.commandGhost(w.session); !ok {
		t.Fatal("the catalog did not offer a suggestion before disabling inline suggestions")
	}
	var saved savedState
	if err := json.Unmarshal([]byte(`{"command_inline_suggest_off":true}`), &saved); err != nil {
		t.Fatal(err)
	}
	w.state.restoreShared(saved)
	if rest, ok := w.state.commandGhost(w.session); ok {
		t.Fatalf("inline suggestions are disabled but %q is still offered", rest)
	}
	w.state.openCompletions()
	if !w.state.completion.list.active || len(w.state.completion.list.candidates) == 0 {
		t.Fatal("disabling inline suggestions also disabled the manual completion list")
	}
	// Off turns off both modes.
	w.state.setCompletionMode(completionOff)
	if w.state.completion.list.active {
		t.Fatal("turning command completion off left the list open")
	}
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("command completion is disabled but the list reopened")
	}
}

func TestCompletionControlsAndKeysHaveSeparateSettingsHomes(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		t.Run(language.String(), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(language)
			state.Click(hitSettings)
			state.selectSettingsCategory(settingsTerminal)
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 2400})
			for _, option := range state.settingsCompletionOptions() {
				hit, ok := hitByID(&canvas, option.ID)
				if !ok || !canvasHasText(&canvas, option.Label) {
					t.Fatalf("completion mode %q is not offered", option.Label)
				}
				if want := option.ID == hitSettingsCompletionBoth; hit.Selected != want {
					t.Errorf("mode %q selected=%v, want %v by default", option.Label, hit.Selected, want)
				}
			}
			state.selectSettingsCategory(settingsShortcuts)
			canvas.Reset()
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 2400})
			if _, ok := hitByID(&canvas, hitShortcutRowList.At(int(shortcutCompletion-1))); !ok {
				t.Fatal("the shortcut for opening completions is not visible")
			}
			if !canvasHasText(&canvas, state.messages.Completion.Open) {
				t.Fatal("the configurable shortcut does not explain that it opens completions")
			}
			for _, help := range state.completionKeys() {
				if !canvasHasText(&canvas, help.label) || !canvasHasText(&canvas, help.keys) {
					t.Errorf("keyboard shortcuts do not explain %q: %q", help.label, help.keys)
				}
				for _, hit := range canvas.Hits() {
					if hit.Label == help.label {
						t.Errorf("fixed completion behavior %q is presented as an interactive shortcut", help.label)
					}
				}
			}
		})
	}
}

func TestCompletionModeIsChosenAndPersistsInSettings(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	cases := []struct {
		id                    gfx.ID
		mode                  completionMode
		suggestOff, inlineOff bool
	}{
		{hitSettingsCompletionList, completionList, false, true},
		{hitSettingsCompletionOff, completionOff, true, false},
		{hitSettingsCompletionBoth, completionBoth, false, false},
	}
	for _, c := range cases {
		state.Click(c.id)
		if state.completionMode() != c.mode || state.history.suggestOff != c.suggestOff || state.history.inlineOff != c.inlineOff {
			t.Fatalf("mode after %v = %v (suggestOff=%v inlineOff=%v)", c.id, state.completionMode(), state.history.suggestOff, state.history.inlineOff)
		}
		again := newTestState(t, options)
		if again.completionMode() != c.mode {
			t.Fatalf("mode %v did not survive a restart: %v", c.mode, again.completionMode())
		}
	}
}

func TestEarlierCompletionPreferencesKeepTheirMeaning(t *testing.T) {
	for _, c := range []struct {
		record string
		want   completionMode
	}{
		{`{}`, completionBoth},
		{`{"command_suggest_off":true}`, completionOff},
		{`{"command_inline_suggest_off":true}`, completionList},
	} {
		var saved savedState
		if err := json.Unmarshal([]byte(c.record), &saved); err != nil {
			t.Fatal(err)
		}
		state := newTestState(t, testOptions(t))
		state.restoreShared(saved)
		if state.completionMode() != c.want {
			t.Errorf("%s reads as mode %v, want %v", c.record, state.completionMode(), c.want)
		}
	}
}

func TestCompletionCatalogStaysUnloadedWhileCompletionIsOff(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setCompletionMode(completionOff)
	state.completion = completionUI{}
	state.Wake()
	if state.completion.catalogLoading || state.completion.store != nil || state.completion.catalog != nil {
		t.Fatal("the catalog was decoded although completion is off")
	}
	state.setCompletionMode(completionList)
	if !state.completion.catalogLoading {
		t.Fatal("turning completion on did not load the catalog")
	}
	state.closeCompletionWorkers()
}

func TestInlinePreferenceUsesAnIndependentSharedRecord(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.history.inlineOff = true
	entries, local, _, err := splitState(state.snapshot(), newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var record *configstore.Entry
	for index := range entries {
		if entry := &entries[index]; entry.Kind == kindSetting && entry.ID == "command_inline_suggest_off" {
			record = entry
		}
	}
	if record == nil {
		t.Fatal("the inline preference is not its own shared setting")
	}
	saved, _, err := joinState(entries, local)
	if err != nil || !saved.CommandInlineSuggestOff || saved.CommandSuggestOff {
		t.Fatalf("shared record round trip: inlineOff=%v completionOff=%v err=%v", saved.CommandInlineSuggestOff, saved.CommandSuggestOff, err)
	}
	var own map[string]json.RawMessage
	if err := json.Unmarshal(local, &own); err != nil {
		t.Fatal(err)
	}
	if _, ok := own[record.ID]; ok {
		t.Fatal("a shared inline preference was also written as device-local state")
	}
}

func TestCompletionKeyHelpIsSearchableInShortcuts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	searchSettings(t, state, "Alt+→")
	if state.settings.count != 1 || state.settings.results[0].category != settingsShortcuts || state.settings.results[0].label != state.messages.Completion.KeysAcceptWord {
		t.Fatalf("word acceptance search results: %+v", state.settings.results[:state.settings.count])
	}
}

// completionTestCatalog builds an installed baseline large enough for the load
// floor and rich enough that a synthetic update can lose subcommands.
func completionTestCatalog(t *testing.T, roots, children int) *completion.Catalog {
	t.Helper()
	nodes := make([]*completion.Node, roots)
	for index := range nodes {
		nodes[index] = &completion.Node{Token: fmt.Sprintf("tool%03d", index)}
		if index < children {
			nodes[index].Children = []*completion.Node{{Token: "child", Flags: []completion.Flag{{Long: "flag"}}}}
		}
	}
	catalog, err := completion.NewCatalog(completion.RepositoryName, "v10.0.0", strings.Repeat("a", 40), "MIT", nodes)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

// sharedCompletionStore returns one store shared by several windows, holding
// the same kind of catalog as the app's data directory: enough commands for
// the load floor and enough options for the coverage guard.
func sharedCompletionStore(t *testing.T) *completion.CatalogStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), completion.FileName)
	if err := completion.InstallCatalog(path, completionTestCatalog(t, 200, 150)); err != nil {
		t.Fatal(err)
	}
	store := completion.NewCatalogStore(path)
	if _, _, _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return store
}

// completionTestArchive is a synthetic codeload archive matching the shared
// baseline. broken adds the upstream plain-scalar defect, so importing it
// reports one repaired file.
func completionTestArchive(t *testing.T, tag string, broken bool) []byte {
	t.Helper()
	prefix := "carapace-bin-" + tag + "/"
	entries := map[string]string{prefix + "LICENSE": "MIT"}
	for index := 0; index < 200; index++ {
		name := fmt.Sprintf("tool%03d", index)
		entries[prefix+"man/cmd/"+name+"/"+name+".yaml"] = "name: " + name + "\n"
		if index < 150 {
			entries[prefix+"man/cmd/"+name+"/"+name+".child.yaml"] = "name: child\nflags:\n  --flag: an option\n"
		}
	}
	if broken {
		entries[prefix+"man/cmd/broken/broken.yaml"] = "name: broken\nflags:\n  --align?: Vertical (default: top): now\n"
	}
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	for name, data := range entries {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type completionRewriteTransport struct{ target string }

func (transport completionRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(transport.target, "http://")
	return http.DefaultTransport.RoundTrip(clone)
}

// completionUpdateClient serves one release feed and archive to the fixed
// api.github.com/codeload URLs the updater uses.
func completionUpdateClient(t *testing.T, tag string, archive []byte) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases/latest"):
			fmt.Fprintf(writer, `{"tag_name":%q}`, tag)
		case strings.Contains(request.URL.Path, "/git/ref/tags/"):
			fmt.Fprint(writer, `{"object":{"type":"commit","sha":"`+strings.Repeat("b", 40)+`"}}`)
		case strings.Contains(request.URL.Path, "/tar.gz/"):
			writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return &http.Client{Transport: completionRewriteTransport{target: server.URL}}
}

func installCompletionUpdate(t *testing.T, store *completion.CatalogStore, tag string, broken bool) completion.ImportStats {
	t.Helper()
	_, _, stats, installed, err := store.Update(context.Background(), completionUpdateClient(t, tag, completionTestArchive(t, tag, broken)))
	if err != nil || !installed {
		t.Fatalf("installing %s: installed=%v err=%v", tag, installed, err)
	}
	return stats
}

func queueCompletionUpdate(state *State, installed bool) {
	state.completion.updateReady = make(chan completionUpdateResult, 1)
	state.completion.updateReady <- completionUpdateResult{installed: installed}
	state.adoptCompletionUpdate()
}

func TestCompletionUpdateReportsRepairedAndSkippedSources(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	store := sharedCompletionStore(t)
	stats := installCompletionUpdate(t, store, "v11.0.0", true)
	if stats.RepairedFiles != 1 || stats.SkippedSources != 0 {
		t.Fatalf("import stats = %+v", stats)
	}
	c := &state.completion
	c.store, c.catalogGeneration = store, 0
	c.updateReady = make(chan completionUpdateResult, 1)
	c.updateReady <- completionUpdateResult{installed: true}
	state.adoptCompletion() // catalog and its stats first, the result second
	catalog := c.catalog
	if catalog == nil || catalog.Release != "v11.0.0" {
		t.Fatalf("catalog = %+v", catalog)
	}
	m := state.messages.Completion
	if want := fmt.Sprintf(m.UpdatedRepairedFmt, catalog.Release, stats.RepairedFiles, stats.SkippedSources); state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if summary := fmt.Sprintf(m.RepairSummaryFmt, stats.RepairedFiles, stats.SkippedSources); !strings.Contains(status, summary) {
		t.Fatalf("settings status %q does not explain %q", status, summary)
	}
	if checked := c.checked.Format("2006-01-02 15:04"); !strings.Contains(status, checked) {
		t.Fatalf("settings status %q lost its check time %q", status, checked)
	}
}

func TestCompletionUpdateWithoutDefectsKeepsThePlainMessage(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	store := sharedCompletionStore(t)
	stats := installCompletionUpdate(t, store, "v11.0.0", false)
	if stats.RepairedFiles != 0 || stats.SkippedSources != 0 {
		t.Fatalf("clean import stats = %+v", stats)
	}
	c := &state.completion
	c.store, c.catalogGeneration = store, 0
	queueCompletionUpdate(state, true)
	m := state.messages.Completion
	if want := fmt.Sprintf(m.UpdatedFmt, c.catalog.Release); state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if summary := fmt.Sprintf(m.RepairSummaryFmt, 0, 0); strings.Contains(status, summary) {
		t.Fatalf("settings status %q reports a defect-free update", status)
	}
}

func TestCompletionUpdateWithoutANewVersionStaysUpToDate(t *testing.T) {
	w := newCompletionWorld(t)
	queueCompletionUpdate(w.state, false)
	if w.state.status != w.state.messages.Completion.UpToDate {
		t.Fatalf("status = %q, want the up-to-date message", w.state.status)
	}
}

func TestCompletionCatalogStatusShowsProvenance(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	m := state.messages.Completion
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	builtin := state.completion.catalog
	repo := fmt.Sprintf(m.CatalogRepoFmt, completion.RepositoryOwner+"/"+completion.RepositoryName, builtin.Commit[:12])
	if !strings.Contains(status, repo) || !strings.Contains(status, m.CatalogOriginBuiltin) {
		t.Fatalf("status %q lacks built-in provenance %q", status, repo)
	}
	if !strings.Contains(status, builtin.Release) {
		t.Fatalf("status %q lost the release", status)
	}

	placeholder, err := completion.NewCatalog(completion.RepositoryName, "v9.9.9", "c", "MIT", []*completion.Node{{Token: "tool"}})
	if err != nil {
		t.Fatal(err)
	}
	state.completion.catalog = placeholder
	state.completion.updateStats = completion.ImportStats{}
	status, err = state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(m.CatalogDescriptionFmt, "v9.9.9", 1, 0) + " · " + m.RecoveryStatsUnknown; status != want {
		t.Fatalf("placeholder commit status = %q, want %q", status, want)
	}

	nonhex, err := completion.NewCatalog(completion.RepositoryName, "v9.9.9", strings.Repeat("z", 40), "MIT", []*completion.Node{{Token: "tool"}})
	if err != nil {
		t.Fatal(err)
	}
	state.completion.catalog = nonhex
	if status, err = state.completionCatalogStatus(); err != nil || strings.Contains(status, "@") {
		t.Fatalf("non-hex commit status = %q, %v", status, err)
	}
}

func TestCompletionCatalogStatusMarksLegacyStatisticsUnknown(t *testing.T) {
	w := newCompletionWorld(t)
	catalog, err := completion.NewCatalog(completion.RepositoryName, "v9.0.0", strings.Repeat("a", 40), "MIT", []*completion.Node{{Token: "tool"}})
	if err != nil {
		t.Fatal(err)
	}
	w.state.completion.catalog = catalog
	status, err := w.state.completionCatalogStatus()
	if err != nil || !strings.Contains(status, "recovery statistics unavailable") {
		t.Fatalf("legacy catalog did not disclose missing statistics: %q, %v", status, err)
	}
}

func TestCompletionCatalogStatusShowsAuditWarnings(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	catalog, _, err := completion.Import(
		[]completion.SourceFile{{Name: "a/a.yaml", Data: []byte("name: a\n")}},
		completion.Metadata{Source: completion.RepositoryName, Release: "v9.9.9", Commit: strings.Repeat("a", 40), License: "MIT"},
		completion.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.AuditWarnings() == 0 {
		t.Fatal("the tiny test catalog should have soft audit warnings")
	}
	state.completion.catalog = catalog
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(state.messages.Completion.AuditWarningsFmt, catalog.AuditWarnings())
	if !strings.Contains(status, want) {
		t.Fatalf("status %q lacks %q", status, want)
	}
}

func TestCompletionCatalogStatusBoundsErrorText(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	state.completion.updateErr = fmt.Errorf("%s", strings.Repeat("bad\u202etext ", 100_000))
	status, err := state.completionCatalogStatus()
	if err == nil {
		t.Fatal("the error disappeared")
	}
	if utf8.RuneCountInString(status) > completionStatusRunes {
		t.Fatalf("status has %d runes, want at most %d", utf8.RuneCountInString(status), completionStatusRunes)
	}
	for _, r := range status {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			t.Fatalf("status contains invisible character %U", r)
		}
	}
}

// The status line is painted while the settings page is open, and an update
// error can carry text from the network. What painting allocates must not
// follow the size of that text.
func TestCompletionCatalogStatusAllocationDoesNotGrowWithTheError(t *testing.T) {
	w := newCompletionWorld(t)
	allocated := func(repeat int) uint64 {
		w.state.completion.updateErr = fmt.Errorf("%s", strings.Repeat("bad text ", repeat))
		w.state.completionCatalogStatus()
		const runs = 20
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range runs {
			w.state.completionCatalogStatus()
		}
		runtime.ReadMemStats(&after)
		return (after.TotalAlloc - before.TotalAlloc) / runs
	}
	small, large := allocated(3_000), allocated(300_000)
	t.Logf("allocated per paint: %d bytes for a 27 KB error, %d bytes for a 2.7 MB error", small, large)
	if large > 2*small+1024 {
		t.Fatalf("painting allocated %d bytes for a 2.7 MB error but %d for a 27 KB one", large, small)
	}
}

func TestCompletionRepairSummarySurvivesAnUpToDateCheck(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	store := sharedCompletionStore(t)
	stats := installCompletionUpdate(t, store, "v11.0.0", true)
	c := &state.completion
	c.store, c.catalogGeneration = store, 0
	queueCompletionUpdate(state, true)
	summary := fmt.Sprintf(state.messages.Completion.RepairSummaryFmt, stats.RepairedFiles, stats.SkippedSources)

	// A later check that installs nothing returns zero stats; the installed
	// catalog still needed those repairs, so the summary must stay.
	c.updateReady = make(chan completionUpdateResult, 1)
	c.updateReady <- completionUpdateResult{}
	state.adoptCompletionUpdate()
	if state.status != state.messages.Completion.UpToDate {
		t.Fatalf("status = %q, want the up-to-date message", state.status)
	}
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, summary) {
		t.Fatalf("an up-to-date check erased the repair summary: %q", status)
	}

	// A failed check installs nothing either.
	c.updateReady = make(chan completionUpdateResult, 1)
	c.updateReady <- completionUpdateResult{err: errors.New("network down")}
	state.adoptCompletionUpdate()
	if c.updateStats.RepairedFiles != stats.RepairedFiles || c.updateStats.SkippedSources != stats.SkippedSources {
		t.Fatalf("failed check changed update stats: %+v", c.updateStats)
	}
}

// The real Wake order observes the published catalog before it observes the
// update result, so the catalog generation has already advanced when the result
// is handled. The store's explicit "installed" answer is what keeps a
// successful install from being reported as "already up to date".
func TestCompletionInstallAdoptedBeforeResultStillReportsTheUpdate(t *testing.T) {
	w := newCompletionWorld(t)
	state := w.state
	store := sharedCompletionStore(t)
	stats := installCompletionUpdate(t, store, "v11.0.0", true)
	c := &state.completion
	c.store, c.catalogGeneration = store, 0
	c.updateReady = make(chan completionUpdateResult, 1)
	c.updateReady <- completionUpdateResult{installed: true}
	state.adoptCompletion() // the real order: catalog first, update result second
	message := state.messages.Completion
	if want := fmt.Sprintf(message.UpdatedRepairedFmt, c.catalog.Release, stats.RepairedFiles, stats.SkippedSources); state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
	status, err := state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if summary := fmt.Sprintf(message.RepairSummaryFmt, stats.RepairedFiles, stats.SkippedSources); !strings.Contains(status, summary) {
		t.Fatalf("settings status %q lacks %q", status, summary)
	}
}

// A pending result can be superseded by another window's install before the
// window consumes it. The message must describe the catalog that was actually
// adopted, not claim that the newer release needed the older result's repairs.
func TestCompletionUpdateMessageDescribesTheAdoptedCatalog(t *testing.T) {
	store := sharedCompletionStore(t)
	first := newCompletionWorld(t)
	first.state.completion.store = store
	first.state.completion.catalogGeneration = 0
	first.state.adoptCompletionCatalog()

	// The first window's check installed the repaired v11; its result is still
	// pending when another window installs the clean v12.
	repairStats := installCompletionUpdate(t, store, "v11.0.0", true)
	installCompletionUpdate(t, store, "v12.0.0", false)
	c := &first.state.completion
	c.updateReady = make(chan completionUpdateResult, 1)
	c.updateReady <- completionUpdateResult{installed: true}
	first.state.adoptCompletion()
	message := first.state.messages.Completion
	if want := fmt.Sprintf(message.UpdatedFmt, "v12.0.0"); first.state.status != want {
		t.Fatalf("status = %q, want %q", first.state.status, want)
	}
	status, err := first.state.completionCatalogStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "v12.0.0") {
		t.Fatalf("settings status %q does not describe the adopted catalog", status)
	}
	if stale := fmt.Sprintf(message.RepairSummaryFmt, repairStats.RepairedFiles, repairStats.SkippedSources); strings.Contains(status, stale) {
		t.Fatalf("settings status %q claims repairs the adopted catalog does not have", status)
	}
}

// Repair counts belong to the shared catalog, not to the window that started
// the update: every window reads them from the store, and a newer clean install
// clears them for all of them.
func TestCompletionRepairStatsFollowTheSharedCatalogAcrossWindows(t *testing.T) {
	store := sharedCompletionStore(t)
	first := newCompletionWorld(t)
	second := newCompletionWorld(t)
	for _, state := range []*State{first.state, second.state} {
		state.completion.store = store
		state.completion.catalogGeneration = 0
		state.adoptCompletionCatalog()
		if state.completion.updateStats != (completion.ImportStats{}) {
			t.Fatalf("window adopted stats %+v from a load", state.completion.updateStats)
		}
	}

	// The first window installs a catalog that needed a repair.
	repairStats := installCompletionUpdate(t, store, "v11.0.0", true)
	for name, state := range map[string]*State{"first": first.state, "second": second.state} {
		state.adoptCompletionCatalog()
		if state.completion.updateStats.RepairedFiles != repairStats.RepairedFiles {
			t.Fatalf("%s window stats = %+v, want %+v", name, state.completion.updateStats, repairStats)
		}
		status, err := state.completionCatalogStatus()
		if err != nil {
			t.Fatal(err)
		}
		if summary := fmt.Sprintf(state.messages.Completion.RepairSummaryFmt, repairStats.RepairedFiles, repairStats.SkippedSources); !strings.Contains(status, summary) {
			t.Fatalf("%s window status %q lacks %q", name, status, summary)
		}
	}

	// The second window installs a newer clean catalog; the counters of the
	// repaired one must not be attributed to it anywhere.
	cleanStats := installCompletionUpdate(t, store, "v12.0.0", false)
	if cleanStats.RepairedFiles != 0 || cleanStats.SkippedSources != 0 {
		t.Fatalf("clean import stats = %+v", cleanStats)
	}
	for name, state := range map[string]*State{"first": first.state, "second": second.state} {
		state.adoptCompletionCatalog()
		if stats := state.completion.updateStats; stats.RepairedFiles != 0 || stats.SkippedSources != 0 {
			t.Fatalf("%s window kept stale stats %+v", name, stats)
		}
		if state.completion.catalog.Release != "v12.0.0" {
			t.Fatalf("%s window release = %q", name, state.completion.catalog.Release)
		}
		status, err := state.completionCatalogStatus()
		if err != nil {
			t.Fatal(err)
		}
		if summary := fmt.Sprintf(state.messages.Completion.RepairSummaryFmt, repairStats.RepairedFiles, repairStats.SkippedSources); strings.Contains(status, summary) {
			t.Fatalf("%s window status %q still reports the old repairs", name, status)
		}
	}
}

func TestCompletionStatusTextBoundsAndSanitizes(t *testing.T) {
	text := "prefix " + strings.Repeat("y", 1000) + "\u202e\x1b end"
	got := completionStatusText(text)
	if utf8.RuneCountInString(got) > completionStatusRunes {
		t.Fatalf("status text has %d runes, want at most %d", utf8.RuneCountInString(got), completionStatusRunes)
	}
	for _, r := range got {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("status text contains invisible character %U", r)
		}
	}
}

func canvasHasText(canvas *gfx.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return true
		}
	}
	return false
}
