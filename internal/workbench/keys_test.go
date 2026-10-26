package workbench

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"golang.org/x/crypto/ssh"
	"motty/internal/sshconn"
)

// lockedIdentities holds one passphrase-protected key per passphrase: protecting
// a key with bcrypt costs about a tenth of a second, and no test depends on two
// locked keys being different.
var (
	lockedIdentitiesMu sync.Mutex
	lockedIdentities   = map[string][]byte{}
)

func identityPEM(t testing.TB, passphrase string) []byte {
	t.Helper()
	if passphrase != "" {
		lockedIdentitiesMu.Lock()
		defer lockedIdentitiesMu.Unlock()
		if data, ok := lockedIdentities[passphrase]; ok {
			return data
		}
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(private, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	data := pem.EncodeToMemory(block)
	if passphrase != "" {
		lockedIdentities[passphrase] = data
	}
	return data
}

func writeWorkbenchIdentity(t testing.TB, name, passphrase string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, identityPEM(t, passphrase), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKeysPageImportsPersistsAndRemovesIdentities(t *testing.T) {
	plain := writeWorkbenchIdentity(t, "id_plain", "")
	encrypted := writeWorkbenchIdentity(t, "id_locked", "secret")
	invalid := filepath.Join(t.TempDir(), "not-a-key")
	if err := os.WriteFile(invalid, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}

	options := freshOptions(t)
	var request app.OpenFileOptions
	options.OpenFiles = func(options app.OpenFileOptions) ([]string, error) {
		request = options
		return []string{plain, encrypted, invalid, plain}, nil
	}
	state := newTestState(t, options)
	state.Click(hitKeychain)
	if state.activeSection != sectionKeys {
		t.Fatal("Keychain did not open the keys page")
	}
	state.Click(hitKeysImport)
	if request.Title != state.messages.KeyImportTitle || !request.AllowMultiple {
		t.Fatalf("open options = %+v", request)
	}
	if len(state.identities) != 2 {
		t.Fatalf("identities = %+v", state.identities)
	}
	if state.identities[0].Encrypted || !state.identities[1].Encrypted {
		t.Fatalf("protection metadata = %+v", state.identities)
	}
	if state.identities[0].Algorithm != ssh.KeyAlgoED25519 || state.identities[0].Fingerprint == "" {
		t.Fatalf("plain metadata = %+v", state.identities[0])
	}

	restarted := newTestState(t, options)
	if len(restarted.identities) != 2 || restarted.identities[0].Path != plain || restarted.identities[1].Path != encrypted {
		t.Fatalf("restored identities = %+v", restarted.identities)
	}
	restarted.Click(hitKeychain)
	restarted.selectedIdentityID = restarted.identities[0].ID
	restarted.Click(hitKeysRemove)
	if len(restarted.identities) != 1 || restarted.identities[0].Path != encrypted {
		t.Fatalf("after remove = %+v", restarted.identities)
	}
	if _, err := os.Stat(plain); err != nil {
		t.Fatalf("remove deleted the key file: %v", err)
	}
	third := newTestState(t, options)
	if len(third.identities) != 1 || third.identities[0].Path != encrypted {
		t.Fatalf("remove was not persisted: %+v", third.identities)
	}
}

func TestImportedIdentityPathsReachConnections(t *testing.T) {
	identity := writeWorkbenchIdentity(t, "id_ed25519", "")
	world, options := newWorld(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{identity}, nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	state.Click(hitHosts)
	connect(t, state, world)
	if len(world.configs) != 1 || !slices.Equal(world.configs[0].AdditionalIdentityFiles, []string{identity}) {
		t.Fatalf("dial config = %+v", world.configs)
	}
}

func TestMissingRestoredIdentityIsVisibleButNotOffered(t *testing.T) {
	options := freshOptions(t)
	options = options.withDefaults()
	missing := filepath.Join(t.TempDir(), "missing-key")
	if err := saveState(options.StatePath, savedState{IdentityFiles: []string{missing}}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.identities) != 1 || state.identities[0].Error == "" {
		t.Fatalf("missing identity = %+v", state.identities)
	}
	if paths := state.identityPaths(); len(paths) != 0 {
		t.Fatalf("missing key was offered: %v", paths)
	}
}

func TestKeyImportCancelAndFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "cancel", err: app.ErrCanceled},
		{name: "failure", err: errors.New("picker failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := freshOptions(t)
			options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return nil, test.err }
			state := newTestState(t, options)
			state.Click(hitKeychain)
			state.Click(hitKeysImport)
			if len(state.identities) != 0 || state.status == "" {
				t.Fatalf("identities=%v status=%q", state.identities, state.status)
			}
		})
	}
}

func TestLanguageSwitchRefreshesIdentityCells(t *testing.T) {
	identity := writeWorkbenchIdentity(t, "id_ed25519", "secret")
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{identity}, nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	if got := state.identities[0].cells[2]; got != state.messages.KeyPassphraseProtected {
		t.Fatalf("English protection = %q", got)
	}
	state.Click(hitLanguage)
	if got := state.identities[0].cells[2]; got != state.messages.KeyPassphraseProtected {
		t.Fatalf("Chinese protection = %q", got)
	}
}

func TestKeyPassphraseDialogSubmitsRetriesAndSkips(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	path := "/keys/id_ed25519"
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseKeyPassphrasePrompt
		status.Attempt = 1
		status.IdentityPath = path
	})
	state.Wake()
	if state.modal != modalKeyPassphrase || state.EditorFor(hitKeyPassphraseField) == nil || state.keyPassphraseDescription == "" {
		t.Fatalf("passphrase prompt = modal %d description %q", state.modal, state.keyPassphraseDescription)
	}
	typeInto(state, hitKeyPassphraseField, "once-only")
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitKeyPassphraseField}); !result.Handled {
		t.Fatal("Enter did not submit key passphrase")
	}
	if !slices.Equal(fake.keyPassphrases, []string{"once-only"}) || state.keyPassphrase.String() != "" || state.modal != modalNone {
		t.Fatalf("submitted=%v retained=%q modal=%d", fake.keyPassphrases, state.keyPassphrase.String(), state.modal)
	}

	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseKeyPassphrasePrompt
		status.Attempt = 2
		status.IdentityPath = path
	})
	state.Wake()
	if state.modal != modalKeyPassphrase || state.keyPassphraseError != state.messages.KeyPassphraseRetry {
		t.Fatalf("retry modal=%d error=%q", state.modal, state.keyPassphraseError)
	}
	state.Click(hitKeyPassphraseSkip)
	if fake.skippedIdentities != 1 || state.modal != modalNone || state.keyPassphrase.String() != "" {
		t.Fatalf("skips=%d modal=%d retained=%q", fake.skippedIdentities, state.modal, state.keyPassphrase.String())
	}
}

func TestKeysSteadyStatePaintDoesNotAllocate(t *testing.T) {
	identity := writeWorkbenchIdentity(t, "id_ed25519", "secret")
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{identity}, nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("keys steady-state paint allocations = %v", allocations)
	}
}

func BenchmarkPaintKeysPage(b *testing.B) {
	identity := writeWorkbenchIdentity(b, "id_ed25519", "secret")
	options := freshOptions(b)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{identity}, nil }
	state := newTestState(b, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}
