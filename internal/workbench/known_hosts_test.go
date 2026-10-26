package workbench

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"golang.org/x/crypto/ssh"
	sshknownhosts "golang.org/x/crypto/ssh/knownhosts"
	"motty/internal/knownhosts"
)

func TestKnownHostsWorkspacePaintsEntriesAndInteractiveHits(t *testing.T) {
	options := freshOptions(t)
	entries := writeKnownHostsFixture(t, options.KnownHostsPath)
	state := newTestState(t, options)
	state.Click(hitKnownHosts)

	if state.activeSection != sectionKnownHosts || len(state.knownHosts.rows) != len(entries) {
		t.Fatalf("section=%d rows=%d", state.activeSection, len(state.knownHosts.rows))
	}
	canvas, texts := paintedTexts(state)
	for _, id := range []ui.ID{
		hitKnownHostsSearch, hitKnownHostsRefresh, hitKnownHostsDelete, hitKnownHostsRetrust,
		hitKnownHostRowList.At(0), hitKnownHostCheckList.At(0),
	} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Errorf("missing hit %d", id)
		}
	}
	if hit, _ := hitByID(canvas, hitKnownHosts); !hit.Selected {
		t.Fatal("Known Hosts navigation item is not selected")
	}
	if hit, _ := hitByID(canvas, hitKnownHostsDelete); !hit.Disabled {
		t.Fatal("Delete is enabled without a selection")
	}
	for _, text := range []string{
		"@revoked", "|1|", ssh.KeyAlgoED25519, entries[0].Fingerprint[:20], "primary server", "compromised",
	} {
		if !contains(texts, text) {
			t.Errorf("workspace did not paint %q", text)
		}
	}

	// A heading is never cut, whatever the language makes of it.
	for _, heading := range []string{
		state.messages.KnownHostsColumnLine, state.messages.KnownHostsColumnHosts, state.messages.KnownHostsColumnFlags,
		state.messages.KnownHostsColumnType, state.messages.KnownHostsColumnFingerprint, state.messages.KnownHostsColumnComment,
	} {
		if !slices.Contains(texts, heading) {
			t.Errorf("column heading %q was cut", heading)
		}
	}

	state.Click(hitKnownHostRowList.At(0))
	state.Click(hitKnownHostCheckList.At(1))
	if state.knownHosts.selected != 2 {
		t.Fatalf("selected=%d, want 2", state.knownHosts.selected)
	}
	canvas, _ = paintedTexts(state)
	if hit, _ := hitByID(canvas, hitKnownHostCheckList.At(0)); !hit.Checked || hit.Role != ui.RoleCheckbox {
		t.Fatalf("first checkbox hit=%+v", hit)
	}
	if hit, _ := hitByID(canvas, hitKnownHostsRetrust); hit.Disabled {
		t.Fatal("Re-trust stayed disabled with selected entries")
	}
}

func TestKnownHostsSearchAndKeyboardSelection(t *testing.T) {
	options := freshOptions(t)
	writeKnownHostsFixture(t, options.KnownHostsPath)
	state := newTestState(t, options)
	state.Click(hitKnownHosts)

	editor := state.EditorFor(hitKnownHostsSearch)
	if editor == nil {
		t.Fatal("Known Hosts search has no text editor")
	}
	app.TextEvent{Kind: app.TextInsert, Text: "compromised"}.Apply(editor)
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	if len(state.knownHosts.visible) != 1 {
		t.Fatalf("visible=%v", state.knownHosts.visible)
	}
	index := state.knownHosts.visible[0]
	result := state.Key(ui.KeyEvent{Target: hitKnownHostRowList.At(index), Key: ui.KeySpace})
	if !result.Handled || !state.knownHosts.rows[index].selected {
		t.Fatalf("space result=%+v selected=%v", result, state.knownHosts.rows[index].selected)
	}
	result = state.Key(ui.KeyEvent{Target: hitKnownHostsSearch, Key: ui.KeyEscape})
	if !result.Handled || state.knownHosts.search.RuneCount() != 0 || len(state.knownHosts.visible) != len(state.knownHosts.rows) {
		t.Fatalf("escape result=%+v search=%q visible=%d", result, state.knownHosts.search.String(), len(state.knownHosts.visible))
	}
}

func TestKnownHostsDeleteRequiresConfirmationAndRefreshes(t *testing.T) {
	options := freshOptions(t)
	writeKnownHostsFixture(t, options.KnownHostsPath)
	state := newTestState(t, options)
	state.Click(hitKnownHosts)

	result := state.Key(ui.KeyEvent{Target: hitKnownHostRowList.At(0), Key: ui.KeySpace})
	if !result.Handled {
		t.Fatal("keyboard did not select the row")
	}
	result = state.Key(ui.KeyEvent{Target: hitKnownHostRowList.At(0), Key: ui.KeyDelete})
	if !result.Handled || state.knownHosts.action != knownHostsActionDelete {
		t.Fatalf("delete result=%+v action=%d", result, state.knownHosts.action)
	}
	if entries, err := knownhosts.New(options.KnownHostsPath).List(); err != nil || len(entries) != 3 {
		t.Fatalf("file changed before confirmation: entries=%d err=%v", len(entries), err)
	}

	canvas, texts := paintedTexts(state)
	if _, ok := hitByID(canvas, hitKnownHostsDialogConfirm); !ok || !contains(texts, state.messages.KnownHostsDeleteTitle) {
		t.Fatal("delete confirmation is not headless-visible")
	}
	result = state.Key(ui.KeyEvent{Target: hitKnownHostsDialogConfirm, Key: ui.KeyEnter})
	if !result.Handled || state.knownHosts.action != knownHostsActionNone || len(state.knownHosts.rows) != 2 {
		t.Fatalf("confirm result=%+v action=%d rows=%d", result, state.knownHosts.action, len(state.knownHosts.rows))
	}
	if entries, err := knownhosts.New(options.KnownHostsPath).List(); err != nil || len(entries) != 2 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
}

func TestKnownHostsRetrustOnlyRemovesOldKeys(t *testing.T) {
	options := freshOptions(t)
	writeKnownHostsFixture(t, options.KnownHostsPath)
	state := newTestState(t, options)
	state.Click(hitKnownHosts)
	state.Click(hitKnownHostRowList.At(1))
	state.Click(hitKnownHostsRetrust)

	if state.knownHosts.action != knownHostsActionRetrust || !strings.Contains(state.knownHosts.dialogDescription, "No replacement key will be trusted") {
		t.Fatalf("action=%d description=%q", state.knownHosts.action, state.knownHosts.dialogDescription)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.KnownHostsVerifyWarning) {
		t.Fatal("re-trust confirmation does not show the verification warning")
	}
	state.Click(hitKnownHostsDialogConfirm)
	if len(state.sessions) != 0 || !strings.Contains(state.knownHosts.status, "next connection will verify") ||
		!strings.Contains(state.knownHosts.status, "no new key was trusted") {
		t.Fatalf("sessions=%d status=%q", len(state.sessions), state.knownHosts.status)
	}
	if entries, err := knownhosts.New(options.KnownHostsPath).List(); err != nil || len(entries) != 2 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
}

func TestKnownHostsDeleteErrorStaysInConfirmation(t *testing.T) {
	options := freshOptions(t)
	writeKnownHostsFixture(t, options.KnownHostsPath)
	state := newTestState(t, options)
	state.Click(hitKnownHosts)
	state.Click(hitKnownHostRowList.At(0))
	state.Click(hitKnownHostsDelete)

	if err := os.Remove(options.KnownHostsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(options.KnownHostsPath, 0o700); err != nil {
		t.Fatal(err)
	}
	state.Click(hitKnownHostsDialogConfirm)
	if state.knownHosts.action != knownHostsActionDelete || state.knownHosts.dialogError == "" {
		t.Fatalf("action=%d error=%q", state.knownHosts.action, state.knownHosts.dialogError)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, state.knownHosts.dialogError) {
		t.Fatal("delete error is not painted in the confirmation")
	}
	state.Click(hitKnownHostsDialogCancel)
	if state.knownHosts.action != knownHostsActionNone {
		t.Fatal("cancel did not close the failed confirmation")
	}
}

func TestKnownHostsSteadyStatePaintDoesNotAllocate(t *testing.T) {
	options := freshOptions(t)
	writeKnownHostsFixture(t, options.KnownHostsPath)
	state := newTestState(t, options)
	state.Click(hitKnownHosts)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)

	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("Known Hosts steady-state paint allocations = %v", allocations)
	}
}

func TestKnownHostsPaginationKeepsEveryEntryReachable(t *testing.T) {
	options := freshOptions(t)
	key := workbenchKnownHostKey(t)
	var data strings.Builder
	for index := range knownHostsPageSize + 1 {
		data.WriteString(sshknownhosts.Line([]string{fmt.Sprintf("host-%02d.example.com", index)}, key))
		data.WriteByte('\n')
	}
	if err := os.WriteFile(options.KnownHostsPath, []byte(data.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.Click(hitKnownHosts)
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitKnownHostsNext); !ok {
		t.Fatal("first page has no Next hit")
	}
	if _, ok := hitByID(canvas, hitKnownHostRowList.At(int(knownHostsPageSize))); ok {
		t.Fatal("second-page row was painted on the first page")
	}

	state.Click(hitKnownHostsNext)
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitKnownHostRowList.At(int(knownHostsPageSize))); !ok {
		t.Fatal("last entry is not reachable on the second page")
	}
	if _, ok := hitByID(canvas, hitKnownHostsPrevious); !ok {
		t.Fatal("second page has no Previous hit")
	}
}

func writeKnownHostsFixture(t testing.TB, path string) []knownhosts.Entry {
	t.Helper()
	key := workbenchKnownHostKey(t)
	encoded := base64.StdEncoding.EncodeToString(key.Marshal())
	hashed := sshknownhosts.HashHostname("hidden.example.com")
	data := strings.Join([]string{
		sshknownhosts.Line([]string{"plain.example.com"}, key) + " primary server",
		"@revoked old.example.com " + key.Type() + " " + encoded + " compromised",
		hashed + " " + key.Type() + " " + encoded + " private endpoint",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := knownhosts.New(path).List()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func workbenchKnownHostKey(t testing.TB) ssh.PublicKey {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{41}, ed25519.SeedSize))
	key, err := ssh.NewPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	return key
}
