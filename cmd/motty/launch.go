//go:build darwin || windows || linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"sync"

	"github.com/vivaui/viva/app"
	"motty/internal/workbench"
)

// applicationID is the bundle identifier of viva.toml, which also names the login
// entry and the claim of the one running copy.
const applicationID = "dev.motty.app"

// instanceID is the claim's name. A data directory of its own (a test run, a
// second profile) is a copy of its own, so it gets its own claim.
func instanceID(dataDir string) string {
	if dataDir == "" {
		return applicationID
	}
	sum := sha256.Sum256([]byte(dataDir))
	return applicationID + "." + hex.EncodeToString(sum[:4])
}

// launchArguments drops what the system adds to a command line and reports
// whether the program was started in the background (at login).
func launchArguments(args []string) (rest []string, background bool) {
	for _, arg := range args {
		switch {
		case arg == "--background":
			background = true
		case strings.HasPrefix(arg, "-psn_"): // an old macOS launch serial number
		default:
			rest = append(rest, arg)
		}
	}
	return rest, background
}

// loginStart is the settings page's way to start the program at login.
func loginStart() *workbench.LoginStart {
	return &workbench.LoginStart{
		Enabled: func() (bool, error) { return app.AutostartEnabled(applicationID) },
		Set: func(on bool) error {
			return app.SetAutostart(app.Autostart{ID: applicationID, Name: "Motty", Args: []string{"--background"}}, on)
		},
	}
}

// windowSet is the open windows, which a request from outside is handed to. A
// request that comes before any window exists (a launch by link) waits for the
// first one.
type windowSet struct {
	mu      sync.Mutex
	states  []*workbench.State
	pending [][]string
}

var windows windowSet

// Quitting from any window or the status menu visits unsaved native documents
// first. A canceled document close leaves the application running.
func (set *windowSet) quitWithDocuments(allow func()) {
	set.mu.Lock()
	states := append([]*workbench.State(nil), set.states...)
	set.mu.Unlock()
	for _, state := range states {
		if state.ConfirmDocumentClose(func() { set.quitWithDocuments(allow) }) {
			return
		}
	}
	allow()
}

// launch hands a request to the window opened last, or keeps it for the first.
func (set *windowSet) launch(args []string) {
	set.mu.Lock()
	if len(set.states) == 0 {
		set.pending = append(set.pending, args)
		set.mu.Unlock()
		return
	}
	state := set.states[len(set.states)-1]
	set.mu.Unlock()
	state.Launch(args)
}

func (set *windowSet) add(state *workbench.State) {
	set.mu.Lock()
	set.states = append(set.states, state)
	pending := set.pending
	set.pending = nil
	set.mu.Unlock()
	for _, args := range pending {
		state.Launch(args)
	}
}

func (set *windowSet) remove(state *workbench.State) {
	set.mu.Lock()
	defer set.mu.Unlock()
	for index, candidate := range set.states {
		if candidate == state {
			set.states = slices.Delete(set.states, index, index+1)
			return
		}
	}
}
