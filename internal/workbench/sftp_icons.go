package workbench

import (
	"os"
	"path"
	"strings"

	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

// sftpExtensionIcons maps a lower-case extension to its file-type glyph. The
// glyphs are Material Icon Theme's: flat, filled and colored, so the list reads
// at a glance by shape and hue. Built once at start; a listing only looks up.
var sftpExtensionIcons = map[string]*ui.Icon{
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
var sftpNameIcons = map[string]*ui.Icon{
	"dockerfile": &icons.MitDocker, "docker-compose.yml": &icons.MitDocker, "docker-compose.yaml": &icons.MitDocker,
	"license": &icons.MitLicense, "licence": &icons.MitLicense, "copying": &icons.MitLicense,
	"readme": &icons.MitReadme, "readme.md": &icons.MitReadme, "readme.txt": &icons.MitReadme,
	"package.json": &icons.MitNodejs, "package-lock.json": &icons.MitNodejs,
	"nginx.conf": &icons.MitNginx, "makefile": &icons.MitSettings,
	".gitignore": &icons.MitGit, ".gitattributes": &icons.MitGit, ".gitmodules": &icons.MitGit, ".gitconfig": &icons.MitGit,
}

// sftpFileIcon picks the glyph of one directory entry from its mode and name.
// mono reports a glyph that has no colors of its own (the generic file and the
// link), which the list tints quietly. It runs once when a listing arrives,
// not on every paint.
func sftpFileIcon(name string, mode os.FileMode) (icon *ui.Icon, mono bool) {
	switch {
	case mode&os.ModeSymlink != 0:
		return &icons.Link, true
	case mode.IsDir():
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
