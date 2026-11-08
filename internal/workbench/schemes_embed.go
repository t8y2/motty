package workbench

import "embed"

// bundledSchemes holds the read-only community color scheme collection that
// ships inside the binary. The files are X resources schemes from the
// iTerm2-Color-Schemes collection as the Tabby terminal distributes them; their
// licenses are in schemes/LICENSE and NOTICE.
//
//go:embed all:schemes
var bundledSchemes embed.FS
