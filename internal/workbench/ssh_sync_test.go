package workbench

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"motty/internal/sshconn"
)

func newSyncState(t *testing.T, config string) (*State, string) {
	t.Helper()
	options := freshOptions(t)
	path := filepath.Join(t.TempDir(), ".ssh", "config")
	if config != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	options.SSHConfigPath = path
	return newTestState(t, options), path
}

func addSyncHost(t *testing.T, state *State, label, address, user string, port uint16) Host {
	t.Helper()
	host := Host{ID: state.nextHostID, Label: label, Address: address, User: user, Port: port}
	state.nextHostID++
	host.CatalogID = uniqueCatalogID("", host, map[string]struct{}{})
	host.finish()
	state.hosts = append(state.hosts, host)
	state.hostDataChanged()
	state.save()
	return host
}

func editSyncHost(t *testing.T, state *State, label string, edit func(*Host)) {
	t.Helper()
	for index := range state.hosts {
		if state.hosts[index].Label == label {
			previous := state.hosts[index]
			updated := previous
			edit(&updated)
			updated.normalizeEndpoint()
			updated.finish()
			state.replaceHost(index, previous, updated)
			state.hostDataChanged()
			state.save()
			return
		}
	}
	t.Fatalf("no host %q", label)
}

func enableSync(t *testing.T, state *State) {
	t.Helper()
	state.clickSSHSync(hitSettingsSSHSyncToggle)
	if !state.sshSync.file.Enabled {
		t.Fatalf("sync did not turn on: %s", state.status)
	}
}

func readSyncFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeSyncFile edits the config the way an editor does and moves its time on, so
// the change is seen even where the file system's clock is coarse.
func writeSyncFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func syncedHost(t *testing.T, state *State, label string) Host {
	t.Helper()
	host, ok := state.hostByLabel(label)
	if !ok {
		t.Fatalf("no host %q in %+v", label, state.hosts)
	}
	return host
}

func (state *State) hostByLabel(label string) (Host, bool) {
	for _, host := range state.hosts {
		if host.Label == label {
			return host, true
		}
	}
	return Host{}, false
}

func TestSSHSyncOffTouchesNothing(t *testing.T) {
	state, path := newSyncState(t, "Host mine\n    HostName mine.example\n")
	addSyncHost(t, state, "web", "web.example", "deploy", 22)
	state.Click(hitLocalTerminal)
	if len(state.hosts) != 1 || readSyncFile(t, path) != "Host mine\n    HostName mine.example\n" {
		t.Fatalf("hosts=%+v file=%q", state.hosts, readSyncFile(t, path))
	}
	if _, err := os.Stat(state.sshSyncStorePath()); err == nil {
		t.Fatal("a sync file was written while the sync is off")
	}
}

func TestSSHSyncEnableMeetsBothSidesAndSecondSyncChangesNothing(t *testing.T) {
	state, path := newSyncState(t, `Host mine
    HostName mine.example
    User me
    Port 2200

Host *
    ServerAliveInterval 30
`)
	web := addSyncHost(t, state, "web", "web.example", "deploy", 22)
	enableSync(t, state)

	mine := syncedHost(t, state, "mine")
	if mine.Address != "mine.example" || mine.User != "me" || mine.Port != 2200 {
		t.Fatalf("imported host = %+v", mine)
	}
	file := readSyncFile(t, path)
	wantBlock := "Host web\n    HostName web.example\n    User deploy\n"
	if !strings.Contains(file, wantBlock) || strings.Index(file, wantBlock) > strings.Index(file, "Host *") {
		t.Fatalf("the new host is not written before the general block:\n%s", file)
	}
	if !strings.HasPrefix(file, "Host mine\n    HostName mine.example\n    User me\n    Port 2200\n") {
		t.Fatalf("the user's own block changed:\n%s", file)
	}
	if _, err := os.Stat(path + ".motty-backup"); err != nil {
		t.Fatalf("no copy of the file was kept: %v", err)
	}

	before := file
	state.syncSSHConfig(true)
	if readSyncFile(t, path) != before || state.sshSync.result != state.messages.SSHSync.UpToDate {
		t.Fatalf("a second sync changed something: %q\n%s", state.sshSync.result, readSyncFile(t, path))
	}
	restarted := newTestState(t, state.options)
	if got := readSyncFile(t, path); got != before || len(restarted.hosts) != 2 || restarted.sshSync.result != "" && restarted.sshSync.result != restarted.messages.SSHSync.UpToDate {
		t.Fatalf("restart changed something: hosts=%d result=%q\n%s", len(restarted.hosts), restarted.sshSync.result, got)
	}
	_ = web
}

func TestSSHSyncCarriesEditsBothWays(t *testing.T) {
	state, path := newSyncState(t, "Host mine\n    HostName mine.example # home\n    User me\n")
	addSyncHost(t, state, "web", "web.example", "deploy", 22)
	enableSync(t, state)

	// A change made here is written where the host already is.
	editSyncHost(t, state, "mine", func(host *Host) { host.Port = 2222; host.User = "" })
	if got := readSyncFile(t, path); !strings.HasPrefix(got, "Host mine\n    HostName mine.example # home\n    Port 2222\n") {
		t.Fatalf("file after editing mine:\n%s", got)
	}
	editSyncHost(t, state, "web", func(host *Host) { host.Address = "web2.example"; host.Port = 2200 })
	if got := readSyncFile(t, path); !strings.Contains(got, "Host web\n    HostName web2.example\n    User deploy\n    Port 2200\n") {
		t.Fatalf("file after editing web:\n%s", got)
	}

	// A change made in the file, and a new host there, come in at the next click.
	writeSyncFile(t, path, strings.Replace(readSyncFile(t, path), "web2.example", "web3.example", 1)+"\nHost extra\n    HostName extra.example\n    User root\n")
	state.sshSync.checked = time.Time{}
	state.Click(hitLocalTerminal)
	if web := syncedHost(t, state, "web"); web.Address != "web3.example" || web.Port != 2200 {
		t.Fatalf("web after the file changed = %+v", web)
	}
	if extra := syncedHost(t, state, "extra"); extra.Address != "extra.example" || extra.User != "root" {
		t.Fatalf("extra = %+v", extra)
	}
	if got := state.sshSync.result; !strings.Contains(got, "SSH config") {
		t.Fatalf("result = %q", got)
	}
}

func TestSSHSyncFileWinsWhenBothChanged(t *testing.T) {
	state, path := newSyncState(t, "")
	addSyncHost(t, state, "web", "web.example", "deploy", 22)
	enableSync(t, state)
	// Both sides move in one step: the file first, then a host change that is
	// saved with sync held back.
	writeSyncFile(t, path, strings.Replace(readSyncFile(t, path), "User deploy", "User fileuser", 1))
	state.sshSync.running = true // as if a sync were under way: the change below only marks
	editSyncHost(t, state, "web", func(host *Host) { host.User = "hostuser" })
	state.sshSync.running = false
	state.sshSync.pending = true
	state.syncSSHConfig(true)
	if web := syncedHost(t, state, "web"); web.User != "fileuser" {
		t.Fatalf("web = %+v; the file's value should win", web)
	}
	if !strings.Contains(readSyncFile(t, path), "User fileuser") {
		t.Fatalf("file lost its value:\n%s", readSyncFile(t, path))
	}
}

func TestSSHSyncDeletionsFollowOwnership(t *testing.T) {
	state, path := newSyncState(t, "Host mine\n    HostName mine.example\n")
	addSyncHost(t, state, "web", "web.example", "deploy", 22)
	enableSync(t, state)
	if !strings.Contains(readSyncFile(t, path), "Host web\n") {
		t.Fatalf("web was not written:\n%s", readSyncFile(t, path))
	}

	// Deleting a host that Motty wrote removes its block; one the user wrote
	// stays in the file and is not brought back.
	state.hostActionIDs = []uint32{syncedHost(t, state, "web").ID}
	state.confirmDeleteHosts()
	state.hostActionIDs = []uint32{syncedHost(t, state, "mine").ID}
	state.confirmDeleteHosts()
	file := readSyncFile(t, path)
	if strings.Contains(file, "Host web") || strings.Contains(file, "motty") || !strings.Contains(file, "Host mine\n    HostName mine.example\n") {
		t.Fatalf("file after deleting both:\n%s", file)
	}
	state.syncSSHConfig(true)
	if _, back := state.hostByLabel("mine"); back || len(state.hosts) != 0 {
		t.Fatalf("a host the user deleted came back: %+v", state.hosts)
	}

	// Once the user's block is gone from the file, the note about it goes too.
	writeSyncFile(t, path, "")
	state.syncSSHConfig(true)
	if len(state.sshSync.file.Ignored) != 0 {
		t.Fatalf("ignored = %v", state.sshSync.file.Ignored)
	}
}

func TestSSHSyncAliasRemovedFromFileDetachesTheHost(t *testing.T) {
	state, path := newSyncState(t, "")
	addSyncHost(t, state, "web", "web.example", "deploy", 22)
	enableSync(t, state)
	writeSyncFile(t, path, "# nothing left\n")
	state.syncSSHConfig(true)
	if len(state.hosts) != 1 || state.sshSync.file.Links[0].Detached != true {
		t.Fatalf("hosts=%+v links=%+v", state.hosts, state.sshSync.file.Links)
	}
	if got := readSyncFile(t, path); got != "# nothing left\n" {
		t.Fatalf("a removed alias was written back:\n%s", got)
	}
	// Editing it here does not put it back either.
	editSyncHost(t, state, "web", func(host *Host) { host.Port = 2200 })
	if got := readSyncFile(t, path); got != "# nothing left\n" {
		t.Fatalf("a detached host was written:\n%s", got)
	}
}

func TestSSHSyncRefusesToEmptyTheFileWhenEveryHostIsMissing(t *testing.T) {
	state, path := newSyncState(t, "")
	addSyncHost(t, state, "one", "one.example", "", 22)
	addSyncHost(t, state, "two", "two.example", "", 22)
	enableSync(t, state)
	written := readSyncFile(t, path)

	// As if the catalog had failed to load: the hosts are gone, the links remain.
	state.hosts = nil
	state.hostDataChanged()
	state.syncSSHConfig(true)
	if readSyncFile(t, path) != written || len(state.sshSync.file.Links) != 2 {
		t.Fatalf("hosts vanished and the file followed:\n%s\nlinks=%+v", readSyncFile(t, path), state.sshSync.file.Links)
	}
	if state.modal != modalHostImportResult || !strings.Contains(state.hostImportResult.warnings[0], "2") {
		t.Fatalf("no warning was shown: modal=%d %+v", state.modal, state.hostImportResult)
	}
}

func TestSSHSyncJumpHostsGoBothWays(t *testing.T) {
	state, path := newSyncState(t, `Host bastion
    HostName bastion.example
    User jump

Host inner
    HostName inner.example
    ProxyJump bastion
`)
	enableSync(t, state)
	inner, bastion := syncedHost(t, state, "inner"), syncedHost(t, state, "bastion")
	chain, valid := state.proxyChainFor(inner)
	if !valid || len(chain) != 1 || chain[0].ID != bastion.ID {
		t.Fatalf("chain = %+v, %v", chain, valid)
	}

	// A host made here, routed through a jump host, is written with the alias.
	web := addSyncHost(t, state, "web", "web.example", "deploy", 22)
	state.proxyEditor.targetID, state.proxyEditor.jumpID = web.ID, bastion.ID
	state.submitProxyDialog()
	if got := readSyncFile(t, path); !strings.Contains(got, "Host web\n    HostName web.example\n    User deploy\n    ProxyJump bastion\n") {
		t.Fatalf("file:\n%s", got)
	}

	// A jump changed in the file moves the route here; a raw hop makes a host that
	// is not written back as an alias.
	writeSyncFile(t, path, strings.Replace(readSyncFile(t, path), "ProxyJump bastion", "ProxyJump jump@gate.example:2022", 1))
	state.syncSSHConfig(true)
	chain, valid = state.proxyChainFor(syncedHost(t, state, "inner"))
	gate, ok := state.hostByConnection("gate.example", "jump", 2022)
	if !valid || len(chain) != 1 || !ok || chain[0].ID != gate.ID {
		t.Fatalf("chain = %+v (%v), gate=%+v %v", chain, valid, gate, ok)
	}
	if strings.Contains(readSyncFile(t, path), "Host gate.example") {
		t.Fatalf("a hop that only exists to be a jump was written as a host:\n%s", readSyncFile(t, path))
	}
	before := readSyncFile(t, path)
	state.syncSSHConfig(true)
	if readSyncFile(t, path) != before {
		t.Fatalf("the sync is not stable:\n%s", readSyncFile(t, path))
	}
}

func TestSSHSyncRenamingAHostRenamesItsAliasAndTheJumpsToIt(t *testing.T) {
	state, path := newSyncState(t, `Host bastion
    HostName bastion.example

Host inner
    HostName inner.example
    ProxyJump bastion
`)
	enableSync(t, state)
	editSyncHost(t, state, "bastion", func(host *Host) { host.Label = "gateway" })
	file := readSyncFile(t, path)
	if !strings.Contains(file, "Host gateway\n") || strings.Contains(file, "Host bastion") || !strings.Contains(file, "ProxyJump gateway\n") {
		t.Fatalf("file:\n%s", file)
	}
	chain, valid := state.proxyChainFor(syncedHost(t, state, "inner"))
	if !valid || len(chain) != 1 || chain[0].Label != "gateway" {
		t.Fatalf("the route was lost on rename: %+v, %v", chain, valid)
	}
}

func TestSSHSyncSharedBlocksAndOverridesAreReportedNotForced(t *testing.T) {
	state, path := newSyncState(t, `Host a b
    HostName shared.example

Host c
    HostName c.example
`)
	enableSync(t, state)
	// "b" has the address of "a", and one address is one host here.
	if len(state.hosts) != 2 {
		t.Fatalf("hosts = %+v", state.hosts)
	}
	if w := state.hostImportResult.warnings[0]; !strings.Contains(w, "Skipped b") {
		t.Fatalf("warnings = %+v", state.hostImportResult)
	}
	before := readSyncFile(t, path)
	state.Click(hitHostImportDone)
	editSyncHost(t, state, "a", func(host *Host) { host.User = "someone" })
	state.syncSSHConfig(true)
	if readSyncFile(t, path) != before {
		t.Fatalf("a shared block was edited:\n%s", readSyncFile(t, path))
	}
	var shared bool
	for _, warning := range state.hostImportResult.warnings[:state.hostImportResult.visible] {
		shared = shared || strings.Contains(warning, "shares a block")
	}
	if state.modal != modalHostImportResult || !shared {
		t.Fatalf("modal=%d warnings=%+v", state.modal, state.hostImportResult)
	}
}

func TestSSHSyncToggleThroughSettings(t *testing.T) {
	state, path := newSyncState(t, "")
	addSyncHost(t, state, "web", "web.example", "", 22)
	state.openSettings()
	state.selectSettingsCategory(settingsConnection)
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.SSHSync.ToggleRow) || !hasHit(canvas, hitSettingsSSHSyncToggle) {
		t.Fatalf("setting is not shown: %v", texts)
	}
	state.Click(hitSettingsSSHSyncToggle)
	if !strings.Contains(readSyncFile(t, path), "Host web") {
		t.Fatalf("turning it on wrote nothing:\n%s", readSyncFile(t, path))
	}
	state.Click(hitSettingsSSHSyncToggle)
	addSyncHost(t, state, "later", "later.example", "", 22)
	if strings.Contains(readSyncFile(t, path), "Host later") {
		t.Fatal("a host was written after the sync was turned off")
	}
	if state.sshSync.result != state.messages.SSHSync.Disabled {
		t.Fatalf("result = %q", state.sshSync.result)
	}
}

func TestSSHSyncChoosingAnotherFileStartsOver(t *testing.T) {
	state, path := newSyncState(t, "")
	enableSync(t, state)
	other := filepath.Join(t.TempDir(), "other_config")
	if err := os.WriteFile(other, []byte("Host elsewhere\n    HostName elsewhere.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.setSSHSyncPath(other)
	if _, ok := state.hostByLabel("elsewhere"); !ok || state.sshConfigPath() != other {
		t.Fatalf("hosts=%+v path=%s", state.hosts, state.sshConfigPath())
	}
	state.setSSHSyncPath("")
	if state.sshConfigPath() != path {
		t.Fatalf("reset path = %s", state.sshConfigPath())
	}
}

func TestSSHSyncDoesNotDetachEverythingFromAnEmptiedOrMissingFile(t *testing.T) {
	state, path := newSyncState(t, "")
	addSyncHost(t, state, "one", "one.example", "", 22)
	addSyncHost(t, state, "two", "two.example", "", 22)
	enableSync(t, state)

	writeSyncFile(t, path, "")
	state.syncSSHConfig(true)
	for _, link := range state.sshSync.file.Links {
		if link.Detached {
			t.Fatalf("an emptied file detached %+v", link)
		}
	}
	if got := readSyncFile(t, path); got != "" {
		t.Fatalf("the sync wrote into the emptied file:\n%s", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	state.syncSSHConfig(true)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the missing file was made again")
	}
	if state.modal != modalHostImportResult || !strings.Contains(state.hostImportResult.warnings[0], state.messages.SSHSync.WarnFileMissing) {
		t.Fatalf("modal=%d %+v", state.modal, state.hostImportResult)
	}
	for _, link := range state.sshSync.file.Links {
		if link.Detached {
			t.Fatalf("a missing file detached %+v", link)
		}
	}
}

func TestSSHSyncOffersTheIdentityFileOnce(t *testing.T) {
	key := writeWorkbenchIdentity(t, "id_sync", "")
	state, path := newSyncState(t, "Host keyed\n    HostName keyed.example\n    IdentityFile "+key+"\n")
	enableSync(t, state)
	if len(state.identities) != 1 || state.identities[0].Path != key {
		t.Fatalf("identities = %+v", state.identities)
	}
	state.identities = nil
	state.refreshIdentityCount()
	writeSyncFile(t, path, readSyncFile(t, path)+"# touched\n")
	state.syncSSHConfig(true)
	if len(state.identities) != 0 {
		t.Fatalf("a key the user removed was added again: %+v", state.identities)
	}
}

func TestSSHSyncLargeConfigIsFast(t *testing.T) {
	var hosts strings.Builder
	for index := 0; index < 400; index++ {
		hosts.WriteString("Host file" + strings.Repeat("x", index%3) + string(rune('a'+index%26)) + strings.Repeat("y", index/26) + "\n    HostName 10.1." + itoa(index/200) + "." + itoa(index%200+1) + "\n    User u\n\n")
	}
	state, _ := newSyncState(t, hosts.String()+"Host *\n    ServerAliveInterval 30\n")
	for index := 0; index < 400; index++ {
		addSyncHost(t, state, "mine-"+itoa(index), "10.2."+itoa(index/200)+"."+itoa(index%200+1), "", 22)
	}
	started := time.Now()
	enableSync(t, state)
	first := time.Since(started)
	started = time.Now()
	state.syncSSHConfig(true)
	second := time.Since(started)
	if len(state.hosts) != 800 {
		t.Fatalf("hosts = %d", len(state.hosts))
	}
	t.Logf("first sync of 400+400 hosts %v, a sync with nothing to do %v", first, second)
	if first > 10*time.Second || second > 2*time.Second {
		t.Fatalf("too slow: %v then %v", first, second)
	}
}

func itoa(number int) string { return strconv.Itoa(number) }

func TestSSHSyncBringsAProxyCommandAlongUnconfirmed(t *testing.T) {
	config := "Host tunnel\n    HostName ssh.example.com\n    ProxyCommand cloudflared access ssh --hostname %h\n"
	state, path := newSyncState(t, config)
	enableSync(t, state)
	tunnel := syncedHost(t, state, "tunnel")
	if tunnel.Proxy == nil || tunnel.Proxy.kind != sshconn.ProxyCommand ||
		tunnel.Proxy.address != "cloudflared access ssh --hostname %h" || !tunnel.Proxy.unconfirmed {
		t.Fatalf("the synced host's proxy = %+v", tunnel.Proxy)
	}
	if got := readSyncFile(t, path); got != config {
		t.Fatalf("the user's block changed:\n%s", got)
	}
}
