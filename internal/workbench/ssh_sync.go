package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconfig"
)

// Keeping the hosts in step with the user's OpenSSH config, both ways.
//
// The two sides are compared with what each looked like at the last sync, so
// that a change on one side travels to the other, and a field changed on both
// sides keeps the config file's value. A host is linked to the Host alias that
// stands for it in the file; the links, the choice to sync and the aliases the
// user took out of Motty live in one small file beside the state file, because
// they describe this computer's ~/.ssh/config and must not follow the hosts to
// other computers through cloud sync.
//
// Nothing here runs by itself. A sync happens at start, after the hosts change,
// and when the user touches the window and the file is found changed by a look
// at its modification time; an application with nothing to say holds no timer.

// sshSyncFileVersion is the only version of the sync file this build reads.
const sshSyncFileVersion = 1

// sshSyncGap is the least time between two looks at the config file's
// modification time that clicks cause.
const sshSyncGap = 500 * time.Millisecond

var (
	hitSettingsSSHSyncToggle = tree.ID(settingsHits, "settings-ssh-sync")
	hitSettingsSSHSyncFile   = tree.ID(settingsHits, "settings-ssh-sync-file")
	hitSettingsSSHSyncReset  = tree.ID(settingsHits, "settings-ssh-sync-reset")
	hitSettingsSSHSyncNow    = tree.ID(settingsHits, "settings-ssh-sync-now")
)

// sshSyncFile is what is kept of the sync between runs.
type sshSyncFile struct {
	Version int  `json:"version"`
	Enabled bool `json:"enabled,omitempty"`
	// Path is the config file to sync; empty is ~/.ssh/config.
	Path  string        `json:"path,omitempty"`
	Links []sshSyncLink `json:"links,omitempty"`
	// Ignored are the aliases of blocks the user wrote that were taken out of
	// Motty: they are not brought back while the file still has them.
	Ignored []string `json:"ignored,omitempty"`
}

// sshSyncLink ties a host to its alias in the config file.
type sshSyncLink struct {
	// ID is the host's catalog ID.
	ID    string `json:"id"`
	Alias string `json:"alias,omitempty"`
	// Detached hosts are not written to the file: their alias was removed from
	// it, or they were made only to be the hop of a ProxyJump.
	Detached bool          `json:"detached,omitempty"`
	Base     sshSyncFields `json:"base"`
}

// live reports whether the file holds this link's alias.
func (link sshSyncLink) live() bool { return !link.Detached && link.Alias != "" }

// sshSyncFields is what both sides held at the last sync.
type sshSyncFields struct {
	Label   string `json:"label"`
	Address string `json:"host"`
	User    string `json:"user,omitempty"`
	Port    uint16 `json:"port"`
	// Jump names the hosts of the ProxyJump by catalog ID, so a renamed alias or a
	// hop written another way is still the same chain.
	Jump string `json:"jump,omitempty"`
	// Identity is the IdentityFile last offered to the Keys list.
	Identity string `json:"identity,omitempty"`
}

type sshFileStamp struct {
	path   string
	exists bool
	size   int64
	mod    int64
}

type sshSyncState struct {
	file    sshSyncFile
	loaded  bool
	blocked bool   // the file is from a newer build: it is read as off and never written
	written []byte // what the file holds, to write only a change
	// pending is set when hosts or routes changed since the last sync, print is
	// what they were then, and running keeps a sync from starting a sync.
	pending bool
	running bool
	print   uint64
	// stamps are the config files as the last sync saw them.
	stamps  []sshFileStamp
	checked time.Time
	result  string // how the last sync went, for the setting
	// shown is the config path as the setting prints it, made once: painting
	// must not build strings.
	shown string
}

func (state *State) sshSyncStorePath() string {
	return filepath.Join(filepath.Dir(state.options.StatePath), "ssh-config-sync.json")
}

// sshSyncLoad reads the sync file once. A damaged one is set aside and the sync
// starts off; a newer one is left alone.
func (state *State) sshSyncLoad() {
	view := &state.sshSync
	if view.loaded {
		return
	}
	view.loaded = true
	path := state.sshSyncStorePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var file sshSyncFile
	if err := json.Unmarshal(data, &file); err != nil {
		_, _ = setAside(path)
		return
	}
	if file.Version > sshSyncFileVersion {
		view.blocked = true
		return
	}
	file.Version = sshSyncFileVersion
	view.file, view.written = file, data
}

func (state *State) sshSyncSave() error {
	view := &state.sshSync
	if view.blocked {
		return errors.New("the sync file is from a newer version")
	}
	view.file.Version = sshSyncFileVersion
	data, err := json.MarshalIndent(view.file, "", "  ")
	if err != nil {
		return err
	}
	if bytes.Equal(data, view.written) {
		return nil
	}
	if err := writeFileAtomic(state.sshSyncStorePath(), data); err != nil {
		return err
	}
	view.written = data
	return nil
}

func (state *State) sshSyncOn() bool {
	state.sshSyncLoad()
	return state.sshSync.file.Enabled && !state.sshSync.blocked
}

// sshConfigPath is the file being synced.
func (state *State) sshConfigPath() string {
	if path := state.sshSync.file.Path; path != "" {
		return path
	}
	if path := state.options.SSHConfigPath; path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}

// sshAlias is the name the SSH config knows a host by: the link the sync made,
// or the label the file itself names a Host by, because that is how a host ends
// up in the file. Empty means the file does not know the host.
func (state *State) sshAlias(host Host) string {
	state.sshSyncLoad()
	for _, link := range state.sshSync.file.Links {
		if link.ID == host.CatalogID && link.live() {
			return link.Alias
		}
	}
	config, err := sshconfig.Load(state.sshConfigPath())
	if err != nil {
		return ""
	}
	known, _ := config.Host(host.Label)
	return known.Alias
}

// sshConfigPathText is sshConfigPath for painting.
func (state *State) sshConfigPathText() string {
	if state.sshSync.shown == "" {
		state.sshSync.shown = state.sshConfigPath()
	}
	return state.sshSync.shown
}

// sshSyncMark notes that the hosts or the routes between them changed.
func (state *State) sshSyncMark() {
	if !state.sshSync.running {
		state.sshSync.pending = true
	}
}

// sshSyncLeads says whether this window does the process's syncing: with
// several windows open, one of them does, so two never write the same file.
func (state *State) sshSyncLeads() bool {
	return !state.closed && !state.starting && state.syncLeader() == state
}

// sshSyncFlush runs the sync the hosts' change asked for, once what changed them
// has been saved.
func (state *State) sshSyncFlush() {
	if !state.sshSync.pending || state.sshSync.running || !state.sshSyncLeads() {
		return
	}
	if !state.sshSyncOn() {
		state.sshSync.pending = false
		return
	}
	if state.sshSyncPrint() == state.sshSync.print {
		state.sshSync.pending = false
		return
	}
	state.syncSSHConfig(false)
}

// sshSyncCheck looks at the config files' modification times, after a click, and
// syncs when one changed since the last sync.
func (state *State) sshSyncCheck() {
	view := &state.sshSync
	if view.running || view.pending || !state.sshSyncLeads() || !state.sshSyncOn() {
		return
	}
	if now := time.Now(); now.Sub(view.checked) < sshSyncGap {
		return
	} else {
		view.checked = now
	}
	if sameStamps(view.stamps, statStamps(view.stamps)) {
		return
	}
	state.syncSSHConfig(false)
}

// sshSyncStart is the sync a window makes when it has started.
func (state *State) sshSyncStart() {
	if !state.sshSyncOn() || !state.sshSyncLeads() || state.saveBlocked || state.config == nil {
		return
	}
	state.syncSSHConfig(false)
}

func statStamps(previous []sshFileStamp) []sshFileStamp {
	stamps := make([]sshFileStamp, len(previous))
	for index, stamp := range previous {
		stamps[index] = sshFileStamp{path: stamp.path}
		if info, err := os.Stat(stamp.path); err == nil {
			stamps[index].exists, stamps[index].size, stamps[index].mod = true, info.Size(), info.ModTime().UnixNano()
		}
	}
	return stamps
}

func sameStamps(a, b []sshFileStamp) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func stampsOf(config *sshconfig.Config) []sshFileStamp {
	files := config.Files()
	stamps := make([]sshFileStamp, len(files))
	for index, path := range files {
		stamps[index].path = path
	}
	return statStamps(stamps)
}

// sshSyncPrint is a number that changes when a host, or a route between hosts,
// does, so a save that changed neither does not sync.
func (state *State) sshSyncPrint() uint64 {
	hash := fnvHash(14695981039346656037)
	for _, host := range state.hosts {
		if !host.isSSH() {
			continue
		}
		hash.text(host.CatalogID)
		hash.text(host.Label)
		hash.text(host.Address)
		hash.text(host.User)
		hash.number(uint64(host.Port))
	}
	for _, route := range state.proxies {
		hash.number(uint64(route.TargetID)<<32 | uint64(route.JumpID))
	}
	return uint64(hash)
}

type fnvHash uint64

func (hash *fnvHash) number(value uint64) {
	for shift := 0; shift < 64; shift += 8 {
		*hash = (*hash ^ fnvHash(value>>shift&0xff)) * 1099511628211
	}
}

func (hash *fnvHash) text(value string) {
	for index := 0; index < len(value); index++ {
		*hash = (*hash ^ fnvHash(value[index])) * 1099511628211
	}
	*hash = (*hash ^ 0xff) * 1099511628211
}

// clickSSHSync handles the controls of the setting.
func (state *State) clickSSHSync(id ui.ID) {
	if leader := state.syncLeader(); leader != state {
		leader.clickSSHSync(id)
		state.status = leader.status
		return
	}
	state.sshSyncLoad()
	view := &state.sshSync
	m := state.messages.SSHSync
	if view.blocked {
		state.status = fmt.Sprintf(m.FailedFmt, "the sync file is from a newer version")
		return
	}
	switch id {
	case hitSettingsSSHSyncToggle:
		view.file.Enabled = !view.file.Enabled
		if err := state.sshSyncSave(); err != nil {
			view.file.Enabled = !view.file.Enabled
			state.status = fmt.Sprintf(m.FailedFmt, err)
			return
		}
		if !view.file.Enabled {
			view.result, view.pending = m.Disabled, false
			state.status = m.Disabled
			return
		}
		state.syncSSHConfig(true)
	case hitSettingsSSHSyncFile:
		paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: m.FilePickerTitle})
		if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
			return
		}
		if err != nil {
			state.status = fmt.Sprintf(m.FailedFmt, err)
			return
		}
		state.setSSHSyncPath(paths[0])
	case hitSettingsSSHSyncReset:
		state.setSSHSyncPath("")
	case hitSettingsSSHSyncNow:
		state.syncSSHConfig(true)
	}
}

// setSSHSyncPath changes the file that is synced. The links belong to the old
// file, so they are dropped; the hosts stay, and meet the new file as strangers.
func (state *State) setSSHSyncPath(path string) {
	view := &state.sshSync
	if view.file.Path == path {
		return
	}
	view.file.Path, view.file.Links, view.file.Ignored = path, nil, nil
	view.stamps, view.result, view.shown = nil, "", ""
	if err := state.sshSyncSave(); err != nil {
		state.status = fmt.Sprintf(state.messages.SSHSync.FailedFmt, err)
		return
	}
	if view.file.Enabled {
		state.syncSSHConfig(true)
	}
}

// sshSyncDescription is what the "Sync now" row says.
func (state *State) sshSyncDescription() string {
	if result := state.sshSync.result; result != "" {
		return result
	}
	return state.messages.SSHSync.NowNever
}
