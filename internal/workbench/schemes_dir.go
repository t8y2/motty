package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The schemes directory. Besides the built-ins, the terminal wears any scheme
// found in the data folder's schemes directory, one scheme per file in the X
// resources form the community collections ship: background, foreground,
// cursorColor and color0..color15, with ! comment lines and #define variables.
// A file that does not parse is skipped, so a half-written or foreign file
// costs nothing. The release unpacks its bundled collection there on the first
// run (SeedSchemesDir); from then on the directory is the user's, and files
// they remove stay removed.

// dirSchemePrefix marks a scheme that came from the schemes directory, so its
// id cannot collide with a built-in's and stays stable while the file keeps
// its name.
const dirSchemePrefix = "dir:"

// schemeFileExt is the extension the bundled files carry. It is stripped for
// the scheme's name, and other extensions (or none) are welcome besides.
const schemeFileExt = ".xresources"

var (
	errNoBackground  = errors.New("scheme: no background")
	errNoForeground  = errors.New("scheme: no foreground")
	errMissingColors = errors.New("scheme: not all sixteen colors")
)

// parseXResources reads one scheme file. Values may name a #define variable;
// colors may have three, six or twelve hex digits.
func parseXResources(data []byte) (colorScheme, error) {
	vars := map[string]string{}
	values := map[string][3]uint8{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "#define"); ok {
			name, value, _ := strings.Cut(strings.TrimSpace(rest), " ")
			if name != "" {
				vars[name] = strings.TrimSpace(value)
			}
			continue
		}
		rest, ok := strings.CutPrefix(line, "*.")
		if !ok {
			continue
		}
		key, value, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if variable, isVar := vars[value]; isVar {
			value = variable
		}
		if color, err := parseHexColor(value); err == nil {
			values[strings.TrimSpace(key)] = color
		}
	}
	scheme := colorScheme{}
	background, ok := values["background"]
	if !ok {
		return scheme, errNoBackground
	}
	foreground, ok := values["foreground"]
	if !ok {
		return scheme, errNoForeground
	}
	scheme.background, scheme.foreground, scheme.cursor = background, foreground, foreground
	if cursor, ok := values["cursorColor"]; ok {
		scheme.cursor = cursor
	}
	for slot := 0; slot < 16; slot++ {
		color, ok := values[fmt.Sprintf("color%d", slot)]
		if !ok {
			return scheme, errMissingColors
		}
		scheme.ansi[slot] = color
	}
	return scheme, nil
}

// parseHexColor reads #rgb, #rrggbb or #rrrrggggbbbb into eight-bit channels.
func parseHexColor(value string) ([3]uint8, error) {
	digits, ok := strings.CutPrefix(value, "#")
	if !ok {
		return [3]uint8{}, fmt.Errorf("scheme: %q is not a color", value)
	}
	digits = strings.ToLower(digits)
	if len(digits)%3 != 0 || len(digits) == 0 || len(digits) > 12 {
		return [3]uint8{}, fmt.Errorf("scheme: %q is not a color", value)
	}
	width := len(digits) / 3
	channel := func(slice string) uint8 {
		var v int64
		for _, r := range slice {
			d := int64(r - '0')
			if r > '9' {
				d = int64(r-'a') + 10
			}
			v = v*16 + d
		}
		switch bits := width * 4; {
		case bits < 8: // #rgb: four bits a channel, copied into eight
			v = v<<4 | v
		case bits > 8: // #rrrrggggbbbb: keep the eight high bits
			v >>= uint(bits - 8)
		}
		return uint8(v)
	}
	return [3]uint8{
		channel(digits[:width]),
		channel(digits[width : width*2]),
		channel(digits[width*2:]),
	}, nil
}

// scanSchemesDir reads every scheme file in dir, named after its file. A
// directory, hidden file, unreadable file, unparsable file or name that is
// already taken is skipped.
func scanSchemesDir(dir string) []colorScheme {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var schemes []colorScheme
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		scheme, err := parseXResources(data)
		if err != nil {
			continue
		}
		name := schemeFileName(entry.Name())
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		scheme.id = dirSchemePrefix + name
		scheme.name = name
		schemes = append(schemes, scheme)
	}
	sort.Slice(schemes, func(a, b int) bool {
		return strings.ToLower(schemes[a].name) < strings.ToLower(schemes[b].name)
	})
	return schemes
}

// schemeFileName is the name a scheme takes from its file: the file's name
// without the scheme extension.
func schemeFileName(file string) string {
	return strings.TrimSuffix(file, schemeFileExt)
}

// schemesDir is the folder inside the data folder the scheme files live in.
func (state *State) schemesDir() string {
	return filepath.Join(filepath.Dir(state.options.StatePath), "schemes")
}

// loadDirSchemes reads the schemes directory, before the saved state restores
// its scheme ids.
func (state *State) loadDirSchemes() {
	state.dirSchemes = scanSchemesDir(state.schemesDir())
	state.dirSchemeIndex = make(map[string]int, len(state.dirSchemes))
	for index := range state.dirSchemes {
		state.dirSchemeIndex[state.dirSchemes[index].id] = index
	}
}

// SeedSchemesDir unpacks the bundled collection into the data folder's schemes
// directory the first time a data folder is used, so a new install starts with
// the whole collection to keep or trim. A directory that already holds files is
// left alone: from then on it is the user's, and files they removed stay
// removed. An empty one — a folder made by hand before the first run, say — is
// filled, because it carries nothing of the user's yet. It unpacks to a
// temporary folder first and renames it, so a half-seeded directory never takes
// the place of the real one. The count of unpacked schemes is 0 when the
// directory was already in use.
func SeedSchemesDir(dataDir string) (int, error) {
	dir := filepath.Join(dataDir, "schemes")
	if existing, err := os.ReadDir(dir); err == nil && len(existing) > 0 {
		return 0, nil
	}
	entries, err := bundledSchemes.ReadDir("schemes")
	if err != nil {
		return 0, err
	}
	staging := dir + ".unpacking"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return 0, err
	}
	for _, entry := range entries {
		data, err := bundledSchemes.ReadFile("schemes/" + entry.Name())
		if err == nil {
			err = writeFileAtomic(filepath.Join(staging, entry.Name()), data)
		}
		if err != nil {
			_ = os.RemoveAll(staging)
			return 0, err
		}
	}
	// An empty directory of the same name is replaced; one that appeared with
	// files while the collection was being unpacked is left alone.
	if existing, err := os.ReadDir(dir); err == nil && len(existing) > 0 {
		_ = os.RemoveAll(staging)
		return 0, nil
	} else if err == nil {
		_ = os.RemoveAll(dir)
	}
	if err := os.Rename(staging, dir); err != nil {
		_ = os.RemoveAll(staging)
		return 0, err
	}
	return len(entries), nil
}
