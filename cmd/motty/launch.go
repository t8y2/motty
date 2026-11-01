//go:build darwin || windows || linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/singleinstance"
	"motty/internal/buildinfo"
	"motty/internal/i18n"
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

// claimInstanceID names the claim for a data directory. Equivalent spellings
// of one directory must meet, so the path is made absolute and its existing
// symbolic links resolved. A directory that does not exist yet keeps its
// cleaned absolute path as its identity.
func claimInstanceID(dataDir string) (string, error) {
	if dataDir == "" {
		return applicationID, nil
	}
	canonical, err := canonicalPath(dataDir)
	if err != nil {
		return "", err
	}
	defaultDir, err := defaultDataDir()
	if err != nil {
		return "", err
	}
	defaultCanonical, err := canonicalPath(defaultDir)
	if err != nil {
		return "", err
	}
	if canonical == defaultCanonical {
		return applicationID, nil
	}
	return instanceID(canonical), nil
}

func defaultDataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "motty"), nil
}

func canonicalPath(path string) (string, error) {
	return canonicalPathWith(path, filepath.EvalSymlinks)
}

// canonicalPathWith is canonicalPath with the symlink resolver passed in, so a
// test can reproduce the Windows classification of a path below a file.
func canonicalPathWith(path string, evalSymlinks func(string) (string, error)) (string, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return "", err
	}

	var missing []string
	current := absolute
	for {
		resolved, err := evalSymlinks(current)
		if err == nil {
			// Walking up past a missing tail must stop on a directory: on
			// Windows a path below a file reports ErrNotExist rather than
			// ENOTDIR, so without this check a file would anchor a claim.
			if len(missing) > 0 {
				info, statErr := os.Stat(resolved)
				if statErr != nil {
					return "", statErr
				}
				if !info.IsDir() {
					return "", fmt.Errorf("%s is not a directory", resolved)
				}
			}
			resolved, err = canonicalExistingCase(resolved)
			if err != nil {
				return "", err
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = appendPathPart(resolved, missing[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent, part, ok := splitLastPathPart(current)
		if !ok || part == "." || part == ".." {
			return "", err
		}
		missing = append(missing, part)
		current = parent
	}
}

// absolutePath anchors a relative path without cleaning it first. Cleaning
// symlink/../other before resolving the link would change the directory it
// names.
func absolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if volume := filepath.VolumeName(path); volume != "" {
		return "", fmt.Errorf("relative path with volume %q is ambiguous", volume)
	}
	return cwd + string(os.PathSeparator) + path, nil
}

// canonicalExistingCase returns the on-disk spelling of an existing path.
// EvalSymlinks resolves links but keeps the case the caller supplied, which
// would let Data and DATA get different claims on a case-insensitive volume.
func canonicalExistingCase(path string) (string, error) {
	volume := canonicalVolume(filepath.VolumeName(path))
	current := volume
	if current == "" {
		current = string(os.PathSeparator)
	} else if !os.IsPathSeparator(current[len(current)-1]) {
		current += string(os.PathSeparator)
	}
	for _, part := range pathParts(path[len(volume):]) {
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", err
		}
		wanted, err := os.Stat(filepath.Join(current, part))
		if err != nil {
			return "", err
		}
		actual := ""
		for _, entry := range entries {
			if !strings.EqualFold(entry.Name(), part) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return "", err
			}
			if os.SameFile(info, wanted) {
				actual = entry.Name()
				break
			}
		}
		if actual == "" {
			return "", fmt.Errorf("cannot determine on-disk case for %s", filepath.Join(current, part))
		}
		current = filepath.Join(current, actual)
	}
	return current, nil
}

// canonicalVolume makes a Windows drive or UNC server/share compare without
// regard to case. Unix has no volume component, so this is a no-op there.
func canonicalVolume(volume string) string {
	if volume == "" {
		return ""
	}
	return strings.ToLower(volume)
}

func pathParts(path string) []string {
	var parts []string
	for start := 0; start < len(path); {
		for start < len(path) && os.IsPathSeparator(path[start]) {
			start++
		}
		end := start
		for end < len(path) && !os.IsPathSeparator(path[end]) {
			end++
		}
		if end > start {
			parts = append(parts, path[start:end])
		}
		start = end
	}
	return parts
}

func splitLastPathPart(path string) (parent, part string, ok bool) {
	end := len(path)
	for end > 0 && os.IsPathSeparator(path[end-1]) {
		end--
	}
	volume := filepath.VolumeName(path)
	if end <= len(volume) {
		return "", "", false
	}
	start := end - 1
	for start >= len(volume) && !os.IsPathSeparator(path[start]) {
		start--
	}
	if start < len(volume) {
		return "", "", false
	}
	part = path[start+1 : end]
	parent = path[:start]
	if parent == "" {
		parent = string(os.PathSeparator)
	} else if parent == volume {
		parent += string(os.PathSeparator)
	}
	return parent, part, true
}

func appendPathPart(path, part string) string {
	if len(path) > 0 && os.IsPathSeparator(path[len(path)-1]) {
		return path + part
	}
	return path + string(os.PathSeparator) + part
}

// claimLaunch decides whether this process may open shared storage. A failed
// claim must stop an ordinary launch; while a copy already owns the directory,
// its launch is delivered there and this process exits.
func claimLaunch(dataDir string, args []string, onLaunch func([]string)) (primary bool, claim *singleinstance.Instance, err error) {
	id, err := claimInstanceID(dataDir)
	if err != nil {
		return false, nil, fmt.Errorf("single instance: %w", err)
	}
	claim, primary, err = singleinstance.SingleInstance(id, args, onLaunch)
	if err != nil {
		return false, nil, fmt.Errorf("single instance: %w", err)
	}
	return primary, claim, nil
}

// headlessClaim permits a headless test run to skip the desktop claim only when
// it names a data directory other than the user's. HOME isolation alone is not
// enough: it does not isolate the system keychain.
func headlessClaim(dataDir string) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("headless runs need an explicit MOTTY_DATA_DIR test directory")
	}
	requested, err := canonicalPath(dataDir)
	if err != nil {
		return fmt.Errorf("headless test data directory: %w", err)
	}
	defaultDir, err := defaultDataDir()
	if err != nil {
		return fmt.Errorf("headless test data directory: %w", err)
	}
	defaultCanonical, err := canonicalPath(defaultDir)
	if err != nil {
		return fmt.Errorf("headless test data directory: %w", err)
	}
	if requested == defaultCanonical {
		return errors.New("headless runs cannot use the default MOTTY_DATA_DIR")
	}
	return nil
}

// explainSecondaryLaunch names the build that did not start. Release builds
// keep their existing quiet handoff; a development run must not look started.
// The stable final line is the launcher's machine-readable result.
func explainSecondaryLaunch(w io.Writer) {
	if !buildinfo.Development {
		return
	}
	fmt.Fprintln(w, secondaryLaunchText(launchLanguage(), buildinfo.ProductName))
	fmt.Fprintln(w, "motty: secondary launch handed off")
}

// launchLanguage uses the same explicit language override as the workbench.
// The system locale is the fallback because a secondary launch exits before it
// may read the saved language.
func launchLanguage() i18n.Language {
	for _, name := range []string{languageVariable, "LC_ALL", "LC_MESSAGES", "LANG"} {
		tag := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
		switch {
		case strings.HasPrefix(tag, "zh"):
			return i18n.Chinese
		case strings.HasPrefix(tag, "en"):
			return i18n.English
		}
	}
	return i18n.English
}

func secondaryLaunchText(language i18n.Language, product string) string {
	if language == i18n.Chinese {
		return fmt.Sprintf("%s：启动请求已转交给正在运行的副本；本次开发构建未启动。请退出正在运行的副本后重试。", product)
	}
	return fmt.Sprintf("%s: launch handed to the running copy; this development build did not start. Quit the running copy and try again.", product)
}

// reportStarted is the line the development launcher waits for. It is separate
// from launch so a test can prove a failed window is never reported as ready.
func reportStarted(err error) {
	if err == nil {
		log.Printf("motty: started pid=%d", os.Getpid())
	}
}

// handoffArguments is what a second launch tells the running copy: the
// arguments, and that it was started in the background (at login), which must
// not bring the window to the front.
func handoffArguments(args []string, background bool) []string {
	if !background {
		return args
	}
	return append(slices.Clone(args), "--background")
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
