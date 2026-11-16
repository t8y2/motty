package workbench

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The bundled community collection is read-only. The data folder's schemes
// directory adds the user's own files beside it, one scheme per file in the X
// resources form the community collections ship: background, foreground,
// cursorColor and color0..color15, with ! comment lines and #define variables.
// A file that does not parse is skipped, so a half-written or foreign file
// costs nothing.

const (
	// communitySchemePrefix marks a scheme from the read-only collection and
	// dirSchemePrefix one from the user's directory. Neither can collide with a
	// hand-written built-in's id.
	communitySchemePrefix = "community:"
	dirSchemePrefix       = "dir:"
)

// schemeFileExt is the extension the bundled files carry. It is stripped for
// the scheme's name; user files may use another extension or none.
const schemeFileExt = ".xresources"

var (
	errNoBackground  = errors.New("scheme: no background")
	errNoForeground  = errors.New("scheme: no foreground")
	errMissingColors = errors.New("scheme: not all sixteen colors")

	communitySchemes     = mustLoadCommunitySchemes()
	communitySchemeIndex = indexSchemeIDs(communitySchemes)
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

// loadSchemeFS reads every scheme file in dir, named after its file. A
// directory, hidden file, unreadable file, unparsable file or name that is
// already taken is skipped. Names are sorted case-insensitively.
func loadSchemeFS(fsys fs.FS, dir, prefix string) []colorScheme {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var schemes []colorScheme
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		data, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(dir, entry.Name())))
		if err != nil {
			continue
		}
		scheme, err := parseXResources(data)
		if err != nil {
			continue
		}
		name := schemeFileName(entry.Name())
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		scheme.id = prefix + name
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

// indexSchemeIDs maps each scheme's stable id to its place in the slice.
func indexSchemeIDs(schemes []colorScheme) map[string]int {
	index := make(map[string]int, len(schemes))
	for slot := range schemes {
		index[schemes[slot].id] = slot
	}
	return index
}

// mustLoadCommunitySchemes reads the collection embedded in the binary. The
// files are part of the build, so an empty set is a broken build rather than a
// condition the application can recover from.
func mustLoadCommunitySchemes() []colorScheme {
	schemes := loadSchemeFS(bundledSchemes, "schemes", communitySchemePrefix)
	if len(schemes) == 0 {
		panic("workbench: no bundled color schemes")
	}
	return schemes
}

// schemeNameTaken reports whether a user file would duplicate a hand-written
// built-in or a bundled community scheme. The first definition wins, so a user
// cannot shadow a built-in by dropping a file with the same name.
func schemeNameTaken(name string) bool {
	key := strings.ToLower(name)
	for index := range colorSchemes {
		if strings.ToLower(colorSchemes[index].name) == key {
			return true
		}
	}
	for index := range communitySchemes {
		if strings.ToLower(communitySchemes[index].name) == key {
			return true
		}
	}
	return false
}

// schemesDir is the folder inside the data folder the user's scheme files live
// in.
func (state *State) schemesDir() string {
	return filepath.Join(filepath.Dir(state.options.StatePath), "schemes")
}

// loadDirSchemes reads the user's schemes, before the saved state restores its
// scheme ids.
func (state *State) loadDirSchemes() {
	schemes := loadSchemeFS(os.DirFS(state.schemesDir()), ".", dirSchemePrefix)
	state.dirSchemes = schemes[:0]
	for _, scheme := range schemes {
		if !schemeNameTaken(scheme.name) {
			state.dirSchemes = append(state.dirSchemes, scheme)
		}
	}
	state.dirSchemeIndex = indexSchemeIDs(state.dirSchemes)
}
