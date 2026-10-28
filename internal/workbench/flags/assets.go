// Package flags provides images rendered from DBX's unmodified SVGs.
package flags

import _ "embed"

// The PNGs are generated from svg/ by tools/flags, preserving SVG use, markers,
// strokes, fill rules and clipping. They are decoded lazily by the workbench.

//go:embed png/us.png
var Us []byte

//go:embed png/az.png
var Az []byte

//go:embed png/es.png
var Es []byte

//go:embed png/id.png
var Id []byte

//go:embed png/it.png
var It []byte

//go:embed png/jp.png
var Jp []byte

//go:embed png/kr.png
var Kr []byte

//go:embed png/br.png
var Br []byte

//go:embed png/pl.png
var Pl []byte

//go:embed png/ru.png
var Ru []byte

//go:embed png/th.png
var Th []byte

//go:embed png/tr.png
var Tr []byte

//go:embed png/cn.png
var Cn []byte
