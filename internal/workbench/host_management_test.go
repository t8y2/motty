package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func fillNewHost(state *State, label, address, user, password string) {
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, label)
	typeInto(state, hitDialogAddress, address)
	typeInto(state, hitDialogUser, user)
	typeInto(state, hitDialogPassword, password)
}

func TestNewHostPasswordIsSecureAndSavedInTheVault(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	fillNewHost(state, "Edge", "edge.example.com", "ops", "correct horse")

	canvas, texts := paintedTexts(state)
	secureField := false
	for _, hit := range canvas.Hits() {
		if hit.ID == hitDialogPassword && hit.Role == ui.RoleTextField {
			secureField = hit.Secure && hit.Value == ""
		}
	}
	if !secureField {
		t.Fatal("password input was not secure or exposed its value")
	}
	if contains(texts, "correct horse") {
		t.Fatal("the host dialog painted the password")
	}

	state.Click(hitDialogSubmit)
	if state.modal != modalNone || len(state.hosts) != 4 {
		t.Fatalf("new host was not saved: modal=%d hosts=%d error=%q", state.modal, len(state.hosts), state.dialogError)
	}
	if value, found, err := state.secrets.vault.Get("password:ops@edge.example.com:22"); err != nil || !found || value != "correct horse" {
		t.Fatalf("saved password = %q found=%v err=%v", value, found, err)
	}
}

func TestHostPasswordUsesEachProtocolsExistingVaultKey(t *testing.T) {
	tests := []struct {
		name     string
		protocol hostProtocol
		user     string
		secret   string
	}{
		{name: "SSH", protocol: protocolSSH, user: "ops", secret: "password:ops@host.example.com:22"},
		{name: "VNC", protocol: protocolVNC, secret: "password:vnc@host.example.com:5900"},
		{name: "RDP", protocol: protocolRDP, user: "CORP\\alice", secret: "password:rdp:CORP\\alice@host.example.com:3389"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.openHostDialog()
			state.editor.setProtocol(test.protocol)
			state.editor.label.Insert(test.name)
			state.editor.address.Insert("host.example.com")
			state.editor.user.Insert(test.user)
			state.editor.password.Insert("secret")
			state.Click(hitDialogSubmit)

			if state.modal != modalNone {
				t.Fatalf("submit error = %q", state.dialogError)
			}
			if value, found, err := state.secrets.vault.Get(test.secret); err != nil || !found || value != "secret" {
				t.Fatalf("%s = %q found=%v err=%v", test.secret, value, found, err)
			}
		})
	}
}

func TestTelnetAndSerialHostsDoNotOfferAPassword(t *testing.T) {
	for _, protocol := range []hostProtocol{protocolTelnet, protocolSerial} {
		state := newTestState(t, testOptions(t))
		state.openHostDialog()
		state.editor.setProtocol(protocol)
		canvas, _ := paintedTexts(state)
		if _, ok := hitByID(canvas, hitDialogPassword); ok {
			t.Fatalf("protocol %d painted a password field", protocol)
		}
	}
}

func TestCopyHostCopiesSavedPasswordToTheEditedEndpoint(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	clickHostAction(state, hitCopyHost)
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "prod-copy.example.com")
	state.Click(hitDialogSubmit)

	if state.modal != modalNone || len(state.hosts) != 4 {
		t.Fatalf("copy was not saved: modal=%d hosts=%d error=%q", state.modal, len(state.hosts), state.dialogError)
	}
	if value, found, err := state.secrets.vault.Get("password:deploy@prod-copy.example.com:22"); err != nil || !found || value != "source secret" {
		t.Fatalf("copied password = %q found=%v err=%v", value, found, err)
	}
	if value, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || !found || value != "source secret" {
		t.Fatalf("source password changed = %q found=%v err=%v", value, found, err)
	}
}

func TestTypedCopyPasswordOverridesTheSourceAndMissingSourceCreatesNothing(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
			t.Fatal(err)
		}
		clickHostAction(state, hitCopyHost)
		state.editor.address = text.Editor{}
		typeInto(state, hitDialogAddress, "prod-copy.example.com")
		typeInto(state, hitDialogPassword, "replacement")
		state.Click(hitDialogSubmit)
		if value, found, err := state.secrets.vault.Get("password:deploy@prod-copy.example.com:22"); err != nil || !found || value != "replacement" {
			t.Fatalf("copied password = %q found=%v err=%v", value, found, err)
		}
		if value, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || !found || value != "source secret" {
			t.Fatalf("typed copy changed source password = %q found=%v err=%v", value, found, err)
		}
	})

	t.Run("no source", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		clickHostAction(state, hitCopyHost)
		state.editor.address = text.Editor{}
		typeInto(state, hitDialogAddress, "prod-copy.example.com")
		state.Click(hitDialogSubmit)
		if value, found, err := state.secrets.vault.Get("password:deploy@prod-copy.example.com:22"); err != nil || found || value != "" {
			t.Fatalf("empty source created password = %q found=%v err=%v", value, found, err)
		}
	})
}

func TestEditHostKeepsOrReplacesItsSavedPassword(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	clickHostAction(state, hitEditHost)
	state.Click(hitDialogSubmit)
	if value, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || !found || value != "source secret" {
		t.Fatalf("blank edit changed password = %q found=%v err=%v", value, found, err)
	}

	clickHostAction(state, hitEditHost)
	typeInto(state, hitDialogPassword, "replacement")
	state.Click(hitDialogSubmit)
	if value, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || !found || value != "replacement" {
		t.Fatalf("edited password = %q found=%v err=%v", value, found, err)
	}
}

func TestEditHostMovesItsSavedPasswordToAnEditedEndpoint(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	clickHostAction(state, hitEditHost)
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "moved.example.com")
	state.Click(hitDialogSubmit)

	if state.modal != modalNone || state.hosts[0].Address != "moved.example.com" {
		t.Fatalf("edited host was not saved: modal=%d host=%+v error=%q", state.modal, state.hosts[0], state.dialogError)
	}
	if value, found, err := state.secrets.vault.Get("password:deploy@moved.example.com:22"); err != nil || !found || value != "source secret" {
		t.Fatalf("moved password = %q found=%v err=%v", value, found, err)
	}
	if value, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || found || value != "" {
		t.Fatalf("old endpoint kept password = %q found=%v err=%v", value, found, err)
	}

	clickHostAction(state, hitEditHost)
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "final.example.com")
	typeInto(state, hitDialogPassword, "replacement")
	state.Click(hitDialogSubmit)
	if value, found, err := state.secrets.vault.Get("password:deploy@final.example.com:22"); err != nil || !found || value != "replacement" {
		t.Fatalf("typed moved password = %q found=%v err=%v", value, found, err)
	}
	if value, found, err := state.secrets.vault.Get("password:deploy@moved.example.com:22"); err != nil || found || value != "" {
		t.Fatalf("typed move kept previous password = %q found=%v err=%v", value, found, err)
	}
}

func TestTypedMovedPasswordDeletesTheOldKeyWhenApplyOpensTheVault(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
		t.Fatal(err)
	}
	state.secrets.vault.Lock() // system-keystore mode: Apply may open it without user input
	state.openEditHostID(state.hosts[0].ID)
	state.editor.otpChecked = true // isolate host-password planning from OTP discovery
	state.editor.address = *text.NewEditor("moved-while-locked.example.com")
	typeInto(state, hitDialogPassword, "replacement")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("edit modal=%d error=%q", state.modal, state.dialogError)
	}
	if value, found, err := state.secrets.vault.Get("password:deploy@moved-while-locked.example.com:22"); err != nil || !found || value != "replacement" {
		t.Fatalf("moved password = %q found=%v err=%v", value, found, err)
	}
	if value, found, err := state.secrets.vault.Get(prodPasswordName); err != nil || found || value != "" {
		t.Fatalf("old endpoint kept password = %q found=%v err=%v", value, found, err)
	}
}

func TestHostPasswordFitsEveryHostDialogInAShortWindow(t *testing.T) {
	tests := []struct {
		name     string
		protocol hostProtocol
		last     ui.ID
	}{
		{name: "SSH", protocol: protocolSSH, last: hitDialogAdvanced},
		{name: "VNC", protocol: protocolVNC, last: hitDialogClipboard},
		{name: "RDP", protocol: protocolRDP, last: hitDialogClipboard},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.openHostDialog()
			state.editor.setProtocol(test.protocol)
			canvas, _ := paintAt(state, ui.Size{Width: 900, Height: 600})
			initial := canvas
			state.editor.form.offset = state.editor.form.geometry.MaxOffsetY
			canvas, _ = paintAt(state, ui.Size{Width: 900, Height: 600})
			last, ok := hitByID(canvas, test.last)
			if !ok {
				t.Fatalf("last control %d was not painted", test.last)
			}
			submit, ok := hitByID(canvas, hitDialogSubmit)
			if !ok {
				t.Fatal("submit button was not painted")
			}
			if bottom := last.Bounds.Y + last.Bounds.Height; bottom > submit.Bounds.Y {
				t.Fatalf("last control ends at %.0f inside footer starting at %.0f", bottom, submit.Bounds.Y)
			}
			password := false
			for _, hit := range initial.Hits() {
				if hit.ID == hitDialogPassword && hit.Role == ui.RoleTextField {
					password = hit.Secure && hit.Bounds.Y+hit.Bounds.Height <= submit.Bounds.Y
				}
			}
			if !password {
				t.Fatal("secure password input was absent or reached the footer")
			}
		})
	}
}

func TestHostPasswordSaveErrorsKeepTheDialogOpen(t *testing.T) {
	t.Run("locked", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		if err := state.secrets.vault.SetMasterPassword("a master password"); err != nil {
			t.Fatal(err)
		}
		state.secrets.vault.Lock()
		fillNewHost(state, "Edge", "edge.example.com", "ops", "secret")
		state.Click(hitDialogSubmit)
		want := fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.messages.Autofill.ErrorVaultLocked)
		if state.modal != modalNewHost || state.dialogError != want || len(state.hosts) != 3 {
			t.Fatalf("locked submit: modal=%d error=%q hosts=%d", state.modal, state.dialogError, len(state.hosts))
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		_, options := newWorld(t)
		options.Keystore = &memKeys{unavailable: true}
		state := newTestState(t, options)
		fillNewHost(state, "Edge", "edge.example.com", "ops", "secret")
		state.Click(hitDialogSubmit)
		want := fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.messages.VaultUnavailable)
		if state.modal != modalNewHost || state.dialogError != want || len(state.hosts) != 3 {
			t.Fatalf("unavailable submit: modal=%d error=%q hosts=%d", state.modal, state.dialogError, len(state.hosts))
		}
	})
}

func copyProdTo(state *State, address string) {
	clickHostAction(state, hitCopyHost)
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, address)
	state.Click(hitDialogSubmit)
}

func TestCopyHostLeavesAVaultThatIsNotOpenAloneAndSaysSo(t *testing.T) {
	t.Run("no master password", func(t *testing.T) {
		options := testOptions(t)
		keys := options.Keystore.(*memKeys)
		state := newTestState(t, options)
		if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
			t.Fatal(err)
		}
		state.secrets.vault.Lock() // as after a restart: the key waits in the system store
		loads := keys.loads

		copyProdTo(state, "prod-copy.example.com")

		if state.modal != modalNone || len(state.hosts) != 4 {
			t.Fatalf("copy was not saved: modal=%d hosts=%d error=%q", state.modal, len(state.hosts), state.dialogError)
		}
		if keys.loads != loads {
			t.Fatal("copying a host asked the system secret store for the vault key")
		}
		if !strings.Contains(state.status, state.messages.HostPasswordNotCarried) {
			t.Fatalf("status = %q, want the note that the password stayed behind", state.status)
		}
	})

	t.Run("master password", func(t *testing.T) {
		state := newTestState(t, testOptions(t))
		if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
			t.Fatal(err)
		}
		if err := state.secrets.vault.SetMasterPassword("a master password"); err != nil {
			t.Fatal(err)
		}
		state.secrets.vault.Lock()

		copyProdTo(state, "prod-copy.example.com")

		if state.modal != modalNone || len(state.hosts) != 4 || state.secrets.vault.Unlocked() {
			t.Fatalf("locked copy: modal=%d hosts=%d error=%q", state.modal, len(state.hosts), state.dialogError)
		}
		if !strings.Contains(state.status, state.messages.HostPasswordNotCarried) {
			t.Fatalf("status = %q, want the note that the password stayed behind", state.status)
		}
	})

	t.Run("empty vault", func(t *testing.T) {
		options := testOptions(t)
		keys := options.Keystore.(*memKeys)
		state := newTestState(t, options)
		loads := keys.loads
		copyProdTo(state, "prod-copy.example.com")
		if state.modal != modalNone || len(state.hosts) != 4 || keys.loads != loads {
			t.Fatalf("copy with nothing saved: modal=%d hosts=%d loads=%d->%d", state.modal, len(state.hosts), loads, keys.loads)
		}
		if strings.Contains(state.status, state.messages.HostPasswordNotCarried) {
			t.Fatalf("status = %q mentions a password that does not exist", state.status)
		}
	})
}

// Two VNC hosts at one address and port, one reached directly and one through
// SSH, are different hosts that sign in with the same saved password.
func addSharedVNCHosts(t *testing.T, state *State) (direct, tunnelled Host) {
	t.Helper()
	direct = Host{ID: 90, Label: "Desk", Address: "desk.example.com", Port: 5900, Protocol: protocolVNC}
	tunnelled = Host{ID: 91, Label: "Desk via SSH", Address: "desk.example.com", User: "ops", Port: 5900, Protocol: protocolVNC, ViaSSH: true}
	state.hosts = append(state.hosts, direct, tunnelled)
	state.hostDataChanged()
	if err := state.secrets.vault.Set(deskPasswordName, "desk secret"); err != nil {
		t.Fatal(err)
	}
	return direct, tunnelled
}

const deskPasswordName = "password:vnc@desk.example.com:5900"

func TestMovingAHostKeepsThePasswordAnotherHostStillUses(t *testing.T) {
	state := newTestState(t, testOptions(t))
	_, tunnelled := addSharedVNCHosts(t, state)
	state.selectedHostID = tunnelled.ID
	state.Click(hitEditHost)
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "desk2.example.com")
	state.Click(hitDialogSubmit)

	if state.modal != modalNone {
		t.Fatalf("edit was not saved: %q", state.dialogError)
	}
	if value, found, _ := state.secrets.vault.Get(deskPasswordName); !found || value != "desk secret" {
		t.Fatalf("the host left behind lost its password = %q found=%v", value, found)
	}
	if value, found, _ := state.secrets.vault.Get("password:vnc@desk2.example.com:5900"); !found || value != "desk secret" {
		t.Fatalf("the moved host has no password = %q found=%v", value, found)
	}
}

func TestTypedPasswordNeverReplacesTheOneAnotherHostShares(t *testing.T) {
	state := newTestState(t, testOptions(t))
	_, tunnelled := addSharedVNCHosts(t, state)
	state.selectedHostID = tunnelled.ID
	state.Click(hitCopyHost)
	state.editor.user = text.Editor{}
	typeInto(state, hitDialogUser, "ops2") // a new host, but the same VNC password key
	typeInto(state, hitDialogPassword, "another secret")
	state.Click(hitDialogSubmit)

	if state.modal != modalNewHost || state.dialogError != state.messages.HostPasswordShared {
		t.Fatalf("shared password: modal=%d error=%q", state.modal, state.dialogError)
	}
	if value, _, _ := state.secrets.vault.Get(deskPasswordName); value != "desk secret" {
		t.Fatalf("the shared password became %q", value)
	}

	state.editor.password = text.Editor{}
	typeInto(state, hitDialogPassword, "desk secret")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("the same password should be accepted: %q", state.dialogError)
	}
}

func TestEditingASharedPasswordChangesIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	direct, _ := addSharedVNCHosts(t, state)
	state.selectedHostID = direct.ID
	state.Click(hitEditHost)
	typeInto(state, hitDialogPassword, "new desk secret")
	state.Click(hitDialogSubmit)
	if value, _, _ := state.secrets.vault.Get(deskPasswordName); state.modal != modalNone || value != "new desk secret" {
		t.Fatalf("modal=%d error=%q password=%q", state.modal, state.dialogError, value)
	}
}

func TestChangingProtocolNeitherCarriesNorLeavesBehindThePassword(t *testing.T) {
	tests := []struct {
		name     string
		protocol hostProtocol
		port     string
		carried  string
	}{
		{name: "VNC", protocol: protocolVNC, port: "5900", carried: "password:vnc@prod.example.com:5900"},
		{name: "Telnet", protocol: protocolTelnet, port: "23"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			if err := state.secrets.vault.Set(prodPasswordName, "source secret"); err != nil {
				t.Fatal(err)
			}
			clickHostAction(state, hitEditHost)
			state.editor.setProtocol(test.protocol)
			state.Click(hitDialogSubmit)

			if state.modal != modalNone || state.hosts[0].Protocol != test.protocol {
				t.Fatalf("edit was not saved: modal=%d error=%q", state.modal, state.dialogError)
			}
			if value, found, _ := state.secrets.vault.Get(prodPasswordName); found {
				t.Fatalf("the SSH password stayed behind as %q", value)
			}
			if test.carried != "" {
				if value, found, _ := state.secrets.vault.Get(test.carried); found {
					t.Fatalf("the SSH password was copied to the %s account as %q", test.name, value)
				}
			}
		})
	}
}

func TestChangingProtocolDropsAPasswordTypedForTheOtherOne(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	typeInto(state, hitDialogPassword, "ssh secret")
	state.editor.setProtocol(protocolTelnet)
	state.editor.setProtocol(protocolSSH)
	if state.editor.password.String() != "" {
		t.Fatal("a password typed before switching protocol survived the switch")
	}
}

func TestPasswordFieldIsNotPartOfTheHostConfiguration(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	fillNewHost(state, "Edge", "edge.example.com", "ops", "correct horse")
	state.Click(hitDialogSubmit)
	host := state.hosts[len(state.hosts)-1]
	if strings.Contains(fmt.Sprintf("%+v", host), "correct horse") {
		t.Fatalf("host configuration exposed the password: %+v", host)
	}
	restarted := newTestState(t, options)
	if restored := restarted.hosts[len(restarted.hosts)-1]; strings.Contains(fmt.Sprintf("%+v", restored), "correct horse") {
		t.Fatalf("persisted host configuration exposed the password: %+v", restored)
	}
}

func TestEditHostReusesDialogAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	clickHostAction(state, hitEditHost)
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogEdit || state.hostDialogID != state.hosts[0].ID {
		t.Fatalf("edit dialog state = modal %d mode %d id %d", state.modal, state.hostDialogMode, state.hostDialogID)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.EditHostTitle) {
		t.Fatalf("edit dialog did not reuse the host form: %v", texts)
	}
	state.editor.label = text.Editor{}
	state.editor.address = text.Editor{}
	state.editor.group = text.Editor{}
	typeInto(state, hitDialogLabel, "Primary")
	typeInto(state, hitDialogAddress, "primary.example.com")
	typeInto(state, hitDialogGroup, "Critical")
	state.Click(hitDialogSubmit)

	updated := state.hosts[0]
	if updated.ID != 1 || updated.Label != "Primary" || updated.Address != "primary.example.com" || updated.Group != "Critical" {
		t.Fatalf("updated host = %+v", updated)
	}
	if state.status != state.messages.HostUpdated+updated.Label {
		t.Fatalf("status = %q", state.status)
	}
	typeSearch(state, "Critical")
	if len(state.visible) != 1 || state.hosts[state.visible[0]].ID != updated.ID {
		t.Fatalf("edited host was not searchable: visible=%v", state.visible)
	}
	restarted := newTestState(t, options)
	if len(restarted.hosts) != 3 || restarted.hosts[0].Label != "Primary" || restarted.hosts[0].Address != "primary.example.com" || restarted.hosts[0].Group != "Critical" {
		t.Fatalf("restored hosts = %+v", restarted.hosts)
	}
}

func TestEditHostUpdatesSessionForwardAndProxyReferences(t *testing.T) {
	state := newTestState(t, testOptions(t))
	previous := state.hosts[0]
	state.sessions = append(state.sessions, Session{ID: 1, HostID: previous.ID, Title: previous.Label, host: previous})
	state.forwards = append(state.forwards, forwardRecord{
		ID: 1, Label: "Web", HostLabel: previous.Label, HostAddress: previous.Address,
		HostUser: previous.User, HostPort: previous.Port,
	})
	state.proxies = append(state.proxies, proxyRecord{ID: 1, TargetID: previous.ID, JumpID: state.hosts[1].ID})

	clickHostAction(state, hitEditHost)
	state.editor.label = text.Editor{}
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogLabel, "Primary")
	typeInto(state, hitDialogAddress, "primary.example.com")
	state.Click(hitDialogSubmit)

	updated := state.hosts[0]
	if state.sessions[0].host != updated || state.sessions[0].Title != updated.Label || !strings.Contains(state.sessions[0].Address, updated.Address) {
		t.Fatalf("session reference = %+v", state.sessions[0])
	}
	forward := state.forwards[0]
	if forward.HostLabel != updated.Label || forward.HostAddress != updated.Address || forward.HostUser != updated.User || forward.HostPort != updated.Port {
		t.Fatalf("forward reference = %+v", forward)
	}
	if state.proxies[0].TargetID != updated.ID {
		t.Fatalf("proxy reference changed identity: %+v", state.proxies[0])
	}
}

func TestCopyHostRequiresAnEditedUniqueDestination(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	clickHostAction(state, hitCopyHost)
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogCopy || state.editor.value(fieldLabel) != "Production Copy" {
		t.Fatalf("copy dialog mode=%d label=%q", state.hostDialogMode, state.editor.value(fieldLabel))
	}
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrAddressTaken || len(state.hosts) != 3 {
		t.Fatalf("unchanged copy silently saved: modal=%d error=%q hosts=%d", state.modal, state.dialogError, len(state.hosts))
	}
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "prod-copy.example.com")
	state.Click(hitDialogSubmit)
	if len(state.hosts) != 4 || state.hosts[3].Label != "Production Copy" || state.hosts[3].Address != "prod-copy.example.com" {
		t.Fatalf("copy = %+v", state.hosts)
	}
	if restarted := newTestState(t, options); len(restarted.hosts) != 4 || restarted.hosts[3].Address != "prod-copy.example.com" {
		t.Fatalf("copy was not persisted: %+v", restarted.hosts)
	}
}

func TestHostCheckboxMultiSelectAndBatchMoveGroup(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	result := state.Key(ui.KeyEvent{Target: hitHostList.At(int(state.hosts[1].ID)), Key: ui.KeySpace})
	if !result.Handled || len(state.hostSelections) != 2 || state.hostSelectionText != "2 selected" {
		t.Fatalf("selection = %v text=%q result=%+v", state.hostSelections, state.hostSelectionText, result)
	}
	canvas, _ := paintedTexts(state)
	if edit, ok := hitByID(canvas, hitEditHost); !ok || !edit.Disabled {
		t.Fatalf("edit with a multi-selection = %+v, found=%v", edit, ok)
	}
	clickHostAction(state, hitMoveHostGroup)
	if !state.hostGroupMenu {
		t.Fatal("group dropdown did not open")
	}
	state.Click(hitHostGroupNew)
	if state.modal != modalMoveHostGroup || state.EditorFor(hitMoveGroupField) == nil {
		t.Fatal("new group field did not open")
	}
	typeInto(state, hitMoveGroupField, "Critical")
	result = state.Key(ui.KeyEvent{Target: hitMoveGroupField, Key: ui.KeyEnter})
	if !result.Handled || state.modal != modalNone || state.hosts[0].Group != "Critical" || state.hosts[1].Group != "Critical" || state.hosts[2].Group != "Network" {
		t.Fatalf("batch group result=%+v hosts=%+v", result, state.hosts)
	}
	restarted := newTestState(t, options)
	if restarted.hosts[0].Group != "Critical" || restarted.hosts[1].Group != "Critical" || restarted.hosts[2].Group != "Network" {
		t.Fatalf("groups after restart = %+v", restarted.hosts)
	}
}

func TestHostGroupDropdownMovesToExistingOrNoGroup(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitMoveHostGroup)
	index := -1
	for at, group := range state.hostGroups {
		if group.Name == "Network" {
			index = at
		}
	}
	if index < 0 {
		t.Fatalf("groups = %v", state.hostGroups)
	}
	state.Click(hitHostGroupItemList.At(index))
	if state.hosts[0].Group != "Network" {
		t.Fatalf("existing group move = %q", state.hosts[0].Group)
	}
	clickHostAction(state, hitMoveHostGroup)
	state.Click(hitHostGroupNone)
	if state.hosts[0].Group != "" {
		t.Fatalf("no-group move = %q", state.hosts[0].Group)
	}
}

func TestDeleteHostsConfirmsAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitHostCheckList.At(int(state.hosts[1].ID)))
	state.Click(hitHostCheckList.At(int(state.hosts[2].ID)))
	result := state.Key(ui.KeyEvent{Target: hitHostList.At(int(state.hosts[2].ID)), Key: ui.KeyDelete})
	if !result.Handled {
		t.Fatal("Delete did not open confirmation from the keyboard")
	}
	if state.modal != modalDeleteHost || len(state.hosts) != 3 {
		t.Fatalf("delete did not wait for confirmation: modal=%d hosts=%d", state.modal, len(state.hosts))
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if canvas.InitialFocus() != hitHostDeleteCancel {
		t.Fatalf("delete initial focus = %d", canvas.InitialFocus())
	}
	result = state.Key(ui.KeyEvent{Target: hitHostDeleteConfirm, Key: ui.KeyEnter})
	if !result.Handled || len(state.hosts) != 1 || state.hosts[0].Label != "Production" || state.modal != modalNone {
		t.Fatalf("delete result=%+v hosts=%+v modal=%d", result, state.hosts, state.modal)
	}
	if restarted := newTestState(t, options); len(restarted.hosts) != 1 || restarted.hosts[0].Label != "Production" {
		t.Fatalf("delete was not persisted: %+v", restarted.hosts)
	}
}

func TestDeleteHostRejectsEveryReferenceKind(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*State, Host)
		want  func(*State) string
	}{
		{
			name: "proxy",
			setup: func(state *State, host Host) {
				state.proxies = append(state.proxies, proxyRecord{ID: 1, TargetID: host.ID, JumpID: state.hosts[1].ID})
			},
			want: func(state *State) string { return state.messages.HostDeleteBlockedProxyFmt },
		},
		{
			name: "forward",
			setup: func(state *State, host Host) {
				state.forwards = append(state.forwards, forwardRecord{HostAddress: host.Address, HostUser: host.User, HostPort: host.Port})
			},
			want: func(state *State) string { return state.messages.HostDeleteBlockedForwardFmt },
		},
		{
			name: "session",
			setup: func(state *State, host Host) {
				state.sessions = append(state.sessions, Session{ID: 1, HostID: host.ID, host: host, restored: true})
			},
			want: func(state *State) string { return state.messages.HostDeleteBlockedSessionFmt },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			host := state.hosts[0]
			test.setup(state, host)
			clickHostAction(state, hitDeleteHost)
			if state.modal != modalNone || len(state.hosts) != 3 || !strings.Contains(state.status, host.Label) {
				t.Fatalf("blocked delete modal=%d hosts=%d status=%q", state.modal, len(state.hosts), state.status)
			}
			prefix := strings.Split(test.want(state), "%s")[0]
			if !strings.HasPrefix(state.status, prefix) {
				t.Fatalf("status %q does not use %q", state.status, test.want(state))
			}
		})
	}
}

func TestHostManagementHitsAndSteadyPaintAreAllocationFree(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitMoveHostGroup)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	for _, id := range []ui.ID{
		hitEditHost, hitCopyHost, hitDeleteHost, hitMoveHostGroup,
		hitHostCheckList.At(int(state.hosts[0].ID)), hitHostGroupNone, hitHostGroupNew,
	} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Errorf("missing hit %d", id)
		}
	}
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("steady host-management paint allocated %v times", allocations)
	}
}

// Pressing empty space closes the group menu: the framework paints the menu
// but leaves the backdrop that catches that press to the application.
func TestHostGroupMenuHasABackdropThatClosesIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitMoveHostGroup)
	canvas, _ := paintedTexts(state)
	if hit, ok := hitByID(canvas, hitHostGroupDismiss); !ok || hit.Bounds.Width < 1000 {
		t.Fatalf("the open group menu has no window-wide backdrop: %+v, %v", hit, ok)
	}
	state.Click(hitHostGroupDismiss)
	if state.hostGroupMenu {
		t.Fatal("pressing the backdrop left the group menu open")
	}
}

func TestHostGroupDropdownHasNoBlankRowForTheUngroupedBucket(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Group = "" // some hosts are ungrouped, so the list holds the unnamed bucket
	state.hostDataChanged()
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitMoveHostGroup)
	canvas, texts := paintedTexts(state)
	named := 0
	for _, group := range state.hostGroups {
		if group.Name != "" {
			named++
		}
	}
	items := 0
	for _, hit := range canvas.Hits() {
		if hit.Role == ui.RoleMenuItem && hit.Label == "" {
			t.Fatalf("a blank menu item: %+v", hit)
		}
		if hit.ID.Scope() == hitHostGroupItemList {
			items++
		}
	}
	if items != named || !contains(texts, state.messages.MoveGroupNone) || !contains(texts, state.messages.MoveGroupNew) {
		t.Fatalf("menu items for groups = %d, named groups = %d, texts = %v", items, named, texts)
	}
}

// clickHostAction presses one of the actions on the hosts page the way a user
// does: on a host they picked first, as no host is picked until they do.
func clickHostAction(state *State, id ui.ID) {
	if state.selectedHostID == 0 && len(state.hostSelections) == 0 {
		state.Click(hitHostList.At(int(state.hosts[0].ID)))
	}
	state.Click(id)
}

// The group menu opens under the Move group button, wherever the actions row
// puts it.
func TestTheGroupMenuOpensUnderItsButton(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.Click(hitMoveHostGroup)
	canvas, _ := paintedTexts(state)
	button, ok := hitByID(canvas, hitMoveHostGroup)
	if !ok {
		t.Fatal("the Move group button is not painted")
	}
	none, ok := hitByID(canvas, hitHostGroupNone)
	if !ok {
		t.Fatal("the group menu is not painted")
	}
	if none.Bounds.X < button.Bounds.X-1 || none.Bounds.X > button.Bounds.X+button.Bounds.Width || none.Bounds.Y < button.Bounds.Y+button.Bounds.Height {
		t.Fatalf("the menu starts at %+v but its button is at %+v", none.Bounds, button.Bounds)
	}
}

// Group chips: once a group exists the list can be narrowed to one of them, the
// headers are only there while every group is listed, and the narrowing lets go
// of what it hides.
func TestGroupChipsNarrowTheListToOneGroup(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostFilterAll); !ok {
		t.Fatal("hosts in named groups have no chip row")
	}
	headers := func() int {
		count := 0
		for _, row := range state.hostTreeRows {
			if row.Kind == hostTreeGroup {
				count++
			}
		}
		return count
	}
	if headers() != 2 || len(state.hostTreeRows) != 5 {
		t.Fatalf("all groups: %d headers, %d rows", headers(), len(state.hostTreeRows))
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	index := state.hostGroupIndex("Network")
	state.Click(hitHostFilterList.At(index))
	if headers() != 0 || len(state.hostTreeRows) != 1 || len(state.hostSelections) != 0 {
		t.Fatalf("one group: %d headers, %d rows, %d checked", headers(), len(state.hostTreeRows), len(state.hostSelections))
	}
	state.Click(hitHostFilterAll)
	if headers() != 2 || len(state.hostTreeRows) != 5 {
		t.Fatalf("back to all: %d headers, %d rows", headers(), len(state.hostTreeRows))
	}
}

func TestWithoutNamedGroupsThereAreNoChipsAndNoHeader(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for index := range state.hosts {
		state.hosts[index].Group = ""
	}
	state.hostDataChanged()
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostFilterAll); ok {
		t.Fatal("a chip row is painted with nothing to choose")
	}
	for _, row := range state.hostTreeRows {
		if row.Kind == hostTreeGroup {
			t.Fatal("a header is listed over the only group")
		}
	}
	if len(state.hostTreeRows) != len(state.hosts) {
		t.Fatalf("%d rows for %d hosts", len(state.hostTreeRows), len(state.hosts))
	}
}

func TestAHostMovedOutOfTheShownGroupDoesNotVanish(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostFilterList.At(state.hostGroupIndex("Network")))
	state.Click(hitHostCheckList.At(int(state.hosts[2].ID)))
	state.Click(hitMoveHostGroup)
	state.Click(hitHostGroupItemList.At(state.hostGroupIndex("Servers")))
	if state.hostFilterOn {
		t.Fatal("the list still shows only the group the host left")
	}
	if len(state.hostTreeRows) == 0 {
		t.Fatal("nothing is listed")
	}
}
