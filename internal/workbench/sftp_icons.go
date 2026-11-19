package workbench

import (
	"path"
	"strings"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
	"motty/internal/workbench/icons"
)

// sftpExtensionIcons maps a lower-case extension to its file-type glyph. The
// glyphs are Material Icon Theme's: flat, filled and colored, so the list reads
// at a glance by shape and hue. Built once at start; a listing only looks up.
var sftpExtensionIcons = map[string]*gfx.Icon{
	".png": &icons.MitImage, ".jpg": &icons.MitImage, ".jpeg": &icons.MitImage, ".gif": &icons.MitImage,
	".webp": &icons.MitImage, ".svg": &icons.MitImage, ".bmp": &icons.MitImage, ".ico": &icons.MitImage,
	".heic": &icons.MitImage, ".avif": &icons.MitImage, ".tif": &icons.MitImage, ".tiff": &icons.MitImage,

	".pdf": &icons.MitPdf,
	".txt": &icons.MitDocument, ".rtf": &icons.MitDocument, ".doc": &icons.MitDocument, ".docx": &icons.MitDocument,
	".odt": &icons.MitDocument, ".rst": &icons.MitDocument, ".tex": &icons.MitDocument,
	".md": &icons.MitMarkdown, ".markdown": &icons.MitMarkdown,
	".log": &icons.MitLog,

	".sh": &icons.MitConsole, ".bash": &icons.MitConsole, ".zsh": &icons.MitConsole, ".fish": &icons.MitConsole,
	".bat": &icons.MitConsole, ".cmd": &icons.MitConsole, ".ps1": &icons.MitPowershell,

	".json": &icons.MitJson, ".jsonc": &icons.MitJson, ".json5": &icons.MitJson, ".ndjson": &icons.MitJson,
	".yaml": &icons.MitYaml, ".yml": &icons.MitYaml, ".toml": &icons.MitToml, ".xml": &icons.MitXml, ".plist": &icons.MitXml,
	".ini": &icons.MitSettings, ".conf": &icons.MitSettings, ".cfg": &icons.MitSettings, ".env": &icons.MitSettings,
	".properties": &icons.MitSettings, ".lock": &icons.MitLock,

	".csv": &icons.MitTable, ".tsv": &icons.MitTable, ".xls": &icons.MitTable, ".xlsx": &icons.MitTable, ".ods": &icons.MitTable,

	".html": &icons.MitHtml, ".htm": &icons.MitHtml, ".xhtml": &icons.MitHtml,
	".css": &icons.MitCss, ".scss": &icons.MitSass, ".sass": &icons.MitSass, ".less": &icons.MitCss,
	".js": &icons.MitJavascript, ".mjs": &icons.MitJavascript, ".cjs": &icons.MitJavascript,
	".ts": &icons.MitTypescript, ".jsx": &icons.MitReact, ".tsx": &icons.MitReact, ".vue": &icons.MitVue,
	".py": &icons.MitPython, ".go": &icons.MitGo, ".rs": &icons.MitRust, ".java": &icons.MitJava,
	".c": &icons.MitC, ".h": &icons.MitC, ".cc": &icons.MitCpp, ".cpp": &icons.MitCpp, ".hpp": &icons.MitCpp,
	".cs": &icons.MitCsharp, ".php": &icons.MitPhp, ".rb": &icons.MitRuby, ".swift": &icons.MitSwift,
	".lua": &icons.MitLua, ".gradle": &icons.MitGradle,
	".sql": &icons.MitDatabase, ".db": &icons.MitDatabase, ".sqlite": &icons.MitDatabase,

	".zip": &icons.MitZip, ".tar": &icons.MitZip, ".gz": &icons.MitZip, ".tgz": &icons.MitZip, ".bz2": &icons.MitZip,
	".xz": &icons.MitZip, ".zst": &icons.MitZip, ".7z": &icons.MitZip, ".rar": &icons.MitZip, ".jar": &icons.MitZip,
	".war": &icons.MitZip, ".deb": &icons.MitZip, ".rpm": &icons.MitZip, ".dmg": &icons.MitZip, ".iso": &icons.MitZip,

	".mp3": &icons.MitAudio, ".wav": &icons.MitAudio, ".flac": &icons.MitAudio, ".aac": &icons.MitAudio,
	".ogg": &icons.MitAudio, ".m4a": &icons.MitAudio, ".opus": &icons.MitAudio,
	".mp4": &icons.MitVideo, ".mov": &icons.MitVideo, ".mkv": &icons.MitVideo, ".avi": &icons.MitVideo,
	".webm": &icons.MitVideo, ".m4v": &icons.MitVideo, ".flv": &icons.MitVideo,

	".pem": &icons.MitCertificate, ".crt": &icons.MitCertificate, ".cer": &icons.MitCertificate,
	".csr": &icons.MitCertificate, ".p12": &icons.MitCertificate, ".pfx": &icons.MitCertificate,
	".key": &icons.MitKey, ".pub": &icons.MitKey, ".gpg": &icons.MitKey, ".asc": &icons.MitKey, ".p8": &icons.MitKey,

	".exe": &icons.MitExe, ".msi": &icons.MitExe, ".app": &icons.MitExe,
}

// sftpNameIcons covers files recognized by their whole name.
var sftpNameIcons = map[string]*gfx.Icon{
	"dockerfile": &icons.MitDocker, "docker-compose.yml": &icons.MitDocker, "docker-compose.yaml": &icons.MitDocker,
	"license": &icons.MitLicense, "licence": &icons.MitLicense, "copying": &icons.MitLicense,
	"readme": &icons.MitReadme, "readme.md": &icons.MitReadme, "readme.txt": &icons.MitReadme,
	"package.json": &icons.MitNodejs, "package-lock.json": &icons.MitNodejs,
	"nginx.conf": &icons.MitNginx, "makefile": &icons.MitSettings,
	".gitignore": &icons.MitGit, ".gitattributes": &icons.MitGit, ".gitmodules": &icons.MitGit, ".gitconfig": &icons.MitGit,
}

// sftpFileIcon picks the glyph of one directory entry from its type, its name
// and, for a symbolic link, what it points at. A link takes the kind of its
// target, as a file manager does, so a folder of links still reads by kind
// instead of every row showing the same chain; a broken or unresolved link keeps
// the link glyph, because its target is unknown. mono reports a glyph that has
// no colors of its own (the generic file and the link), which the list tints
// quietly. It runs once when a listing arrives, not on every paint.
func sftpFileIcon(entry sftpclient.Entry) (icon *gfx.Icon, mono bool) {
	name := entry.Name
	switch {
	case entry.IsLink():
		switch {
		case entry.LinkBroken:
			return &icons.Link, true
		case entry.LinkDir:
			return &icons.FolderSolid, false
		case entry.LinkTarget == "":
			// The listing ran out of its per-folder link lookups before this
			// one: what it points at is unknown, and the link glyph says so
			// instead of guessing a kind.
			return &icons.Link, true
		}
		// A link to a file takes the type of the file it names: the target's
		// own name says what it is even when the link's name does not.
		name = sftpLinkTargetName(entry.LinkTarget, name)
	case isApplicationBundle(name, entry.Mode):
		// A bundle is a folder on disk but one thing to the person: a program.
		// This is its stand-in where the system's own icon for it is out of reach,
		// as on a server's disk.
		return &icons.MitExe, false
	case entry.Mode.IsDir():
		return &icons.FolderSolid, false
	}
	lower := strings.ToLower(name)
	if icon, ok := sftpNameIcons[lower]; ok {
		return icon, false
	}
	if icon, ok := sftpExtensionIcons[path.Ext(lower)]; ok {
		return icon, false
	}
	if strings.HasPrefix(name, ".") {
		return &icons.MitSettings, false // dotfiles are configuration
	}
	return &icons.File, true
}

// sftpLinkTargetName is the base name of a link's target, or the link's own name
// when the target names no file (a bare ".", ".." or root). A local Windows
// target may use backslashes even though remote targets use slashes.
func sftpLinkTargetName(target, fallback string) string {
	trimmed := strings.TrimRight(target, `/\`)
	if index := strings.LastIndexAny(trimmed, `/\`); index >= 0 {
		trimmed = trimmed[index+1:]
	}
	switch trimmed {
	case "", ".", "..":
		return fallback
	}
	return trimmed
}
