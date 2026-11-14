package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Read only launcher-relative references, without evaluating batch or
// PowerShell. npm uses %dp0% or $basedir; the official installer uses %~dp0
// or Join-Path $basedir. The referenced package owns the entry point, even
// when both package names are installed in the same prefix.
var piLauncherReference = regexp.MustCompile(`(?i)["'](?:%~dp0|%dp0%|\$basedir)[/\\]?([^"'\r\n]+)["']|Join-Path\s+\$basedir\s+["']([^"'\r\n]+)["']`)

func resolvePiLauncher(launcher string) (command, error) {
	path, err := piLauncherPath(launcher)
	if err != nil {
		return command{}, errors.New("cli: unsupported_launcher")
	}
	dir := filepath.Dir(path)
	var entry, script string
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat", ".ps1":
		script, err = piLauncherScript(path)
		if err != nil {
			return command{}, errors.New("cli: unsupported_launcher")
		}
		entry, err = piScriptEntry(dir, script)
		if err != nil {
			return command{}, err
		}
	case ".js", ".cjs", ".mjs":
		entry = piLauncherEntry(path, true)
		if strings.EqualFold(filepath.Base(entry), "pi-launcher.js") {
			// A directly selected managed JS launcher can recover its Node
			// choice only from a sibling shim that refers to this same file.
			for _, name := range []string{"pi.cmd", "pi.ps1"} {
				code, e := piLauncherScript(filepath.Join(dir, name))
				if e != nil {
					continue
				}
				candidate, e := piScriptEntry(dir, code)
				if e != nil || candidate == "" {
					continue
				}
				actual, e := piLauncherPath(candidate)
				if e == nil && strings.EqualFold(actual, path) {
					script = code
					break
				}
			}
		}
	default:
		return command{program: path}, nil
	}
	if entry == "" {
		return command{}, errors.New("cli: unsupported_launcher")
	}
	// Preserve the official installer's standalone Node choice. npm shims
	// prefer node.exe beside the selected launcher before searching PATH.
	var node string
	err = errors.New("cli: node_required")
	if strings.EqualFold(filepath.Base(entry), "pi-launcher.js") {
		normalized := strings.ToLower(strings.ReplaceAll(script, `\`, "/"))
		if strings.Contains(normalized, "pi-node/current/node.exe") {
			if local := os.Getenv("LOCALAPPDATA"); local != "" {
				if standalone, e := exec.LookPath(filepath.Join(local, "pi-node", "current", "node.exe")); e == nil {
					node, err = standalone, nil
				}
			}
		}
	} else {
		node, err = exec.LookPath(filepath.Join(dir, "node.exe"))
	}
	if err != nil {
		node, err = exec.LookPath("node")
	}
	if err != nil {
		return command{}, errors.New("cli: node_required")
	}
	return command{program: node, args: []string{entry}}, nil
}

func piLauncherScript(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var lines []string
	powershell := strings.EqualFold(filepath.Ext(path), ".ps1")
	block := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if powershell {
			if strings.HasPrefix(trimmed, "<#") {
				block = true
			}
			if block {
				block = !strings.Contains(trimmed, "#>")
				continue
			}
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
		} else {
			lower := strings.ToLower(strings.TrimPrefix(trimmed, "@"))
			if lower == "rem" || strings.HasPrefix(lower, "rem ") || strings.HasPrefix(lower, "rem\t") || strings.HasPrefix(lower, "::") {
				continue
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func piScriptEntry(dir, script string) (string, error) {
	var entry string
	for _, match := range piLauncherReference.FindAllStringSubmatch(script, -1) {
		relative := match[1]
		if relative == "" {
			relative = match[2]
		}
		target := filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(relative, `\`, "/")))
		candidate := piLauncherEntry(target, false)
		if candidate == "" {
			// A missing Pi branch must not make a conditional launcher
			// silently select a different installation that still exists.
			normalized := strings.ToLower(strings.ReplaceAll(target, `\`, "/"))
			if strings.EqualFold(filepath.Base(target), "pi-launcher.js") ||
				strings.Contains(normalized, "/@mariozechner/pi-coding-agent/") ||
				strings.Contains(normalized, "/@earendil-works/pi-coding-agent/") {
				return "", errors.New("cli: unsupported_launcher")
			}
			continue
		}
		if entry != "" && !strings.EqualFold(entry, candidate) {
			return "", errors.New("cli: unsupported_launcher")
		}
		entry = candidate
	}
	return entry, nil
}

func piLauncherEntry(target string, direct bool) string {
	if strings.EqualFold(filepath.Base(target), "pi-launcher.js") {
		if st, err := os.Stat(target); err == nil && st.Mode().IsRegular() {
			return target
		}
		return ""
	}
	for dir := filepath.Dir(target); ; dir = filepath.Dir(dir) {
		raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct {
				Name string          `json:"name"`
				Bin  json.RawMessage `json:"bin"`
			}
			if json.Unmarshal(raw, &pkg) != nil {
				return ""
			}
			if pkg.Name == "" {
				// A build directory may declare its module type locally. A
				// named package is a boundary; do not adopt an outer package.
				if filepath.Dir(dir) == dir {
					return ""
				}
				continue
			}
			if pkg.Name != "@earendil-works/pi-coding-agent" && pkg.Name != "@mariozechner/pi-coding-agent" {
				return ""
			}
			var bin string
			if json.Unmarshal(pkg.Bin, &bin) != nil {
				var bins map[string]string
				if json.Unmarshal(pkg.Bin, &bins) != nil {
					return ""
				}
				bin = bins["pi"]
			}
			if bin == "" || filepath.IsAbs(bin) || filepath.VolumeName(bin) != "" {
				return ""
			}
			entry := filepath.Join(dir, bin)
			relative, err := filepath.Rel(dir, entry)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return ""
			}
			if direct {
				actual, err := piLauncherPath(entry)
				if err != nil || !strings.EqualFold(actual, target) {
					return ""
				}
			}
			if st, err := os.Stat(entry); err == nil && st.Mode().IsRegular() {
				return entry
			}
			return ""
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}
