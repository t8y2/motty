package workbench

import "embed"

// bundledSchemes holds the community color scheme collection the release
// unpacks into the data folder's schemes directory on its first run. The files
// are X resources schemes from the iTerm2-Color-Schemes collection as the
// Tabby terminal distributes them; their license is noted in NOTICE.
//
//go:embed all:schemes
var bundledSchemes embed.FS
