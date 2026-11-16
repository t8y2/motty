package sftpclient

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// localNameLimit is the byte budget for one local name. NTFS allows 255 UTF-16
// units, but staying well below it keeps room for the digest suffix and for
// tools that append their own suffix.
const localNameLimit = 120

// SafeLocalName turns a remote base name into a name the local file system
// takes. Windows rejects `<>:"/\|?*` and control characters, trims trailing
// dots and spaces, and reserves device names such as CON and COM1; the same
// characters are avoided on every platform so a file downloaded on one system
// keeps its name on another.
//
// A name that had to change keeps a short digest of the original. Two remote
// names that differ only in rejected characters (a:b and a*b) would otherwise
// map to one local name and the second transfer would fail as an existing
// file.
func SafeLocalName(remote string) string {
	return safeLocalName(path.Base(remote))
}

// safeLocalName keeps a single path component, without a path separator.
func safeLocalName(name string) string {
	var out strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f, strings.ContainsRune(`<>:"/\|?*`, r):
			out.WriteByte('_')
		default:
			out.WriteRune(r)
		}
	}
	clean := strings.TrimRight(out.String(), ". ")
	if clean == "" {
		clean = "file"
	}
	if reservedLocalName(clean) {
		clean = "_" + clean
	}
	changed := clean != name
	// Keep the extension last so the local file still opens with its type's
	// application: a_b_c~1a2b3c.txt, not a_b_c.txt~1a2b3c.
	extension := path.Ext(clean)
	base := strings.TrimSuffix(clean, extension)
	if base == "" {
		base, extension = clean, ""
	}
	if truncated := truncateLocalName(base, localNameLimit-len(extension)); truncated != base {
		base, changed = truncated, true
	}
	if !changed {
		return base + extension
	}
	digest := sha256.Sum256([]byte(name))
	suffix := "~" + hex.EncodeToString(digest[:3])
	base = truncateLocalName(base, localNameLimit-len(suffix)-len(extension))
	return base + suffix + extension
}

// reservedLocalName reports whether Windows would treat the name as a device
// rather than a file; the check is case-insensitive and ignores an extension,
// because CON.txt is reserved too.
func reservedLocalName(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(stem) < 4 {
		return false
	}
	prefix := strings.ToUpper(stem[:3])
	if prefix != "COM" && prefix != "LPT" {
		return false
	}
	return len(stem) == 4 && stem[3] >= '0' && stem[3] <= '9'
}

// truncateLocalName keeps at most limit bytes without splitting a rune, then
// drops the trailing dots and spaces Windows would strip anyway.
func truncateLocalName(name string, limit int) string {
	if len(name) <= limit {
		return strings.TrimRight(name, ". ")
	}
	cut := 0
	for cut < len(name) {
		_, size := utf8.DecodeRuneInString(name[cut:])
		if cut+size > limit {
			break
		}
		cut += size
	}
	return strings.TrimRight(name[:cut], ". ")
}

// localRelativePath keeps a remote relative path usable on the local file
// system. Only Windows rejects characters and device names in a component;
// renaming there keeps the tree's shape, while other systems keep the remote
// names untouched.
func localRelativePath(relative string, windows bool) string {
	relative = filepath.FromSlash(relative)
	if !windows {
		return relative
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		parts[index] = safeLocalName(part)
	}
	return strings.Join(parts, string(filepath.Separator))
}
