// Package assets holds the files the program carries inside its binary.
package assets

import _ "embed"

// Icon is the application's icon, the file viva.toml names as [app] icon: a
// full-bleed square that Viva shapes for each platform, in the packaged icon and
// in the running window alike.
//
//go:embed icon.png
var Icon []byte

// IconTile is the application icon wearing the rounded tile the system draws
// it in, four times the size the About page paints it (26 points), so the page
// shows the same mark as the Dock without shaping pixels while it paints. It is
// icon.svg rendered with its corners rounded, not a second artwork.
//
//go:embed icon-tile.png
var IconTile []byte

// LockupDark and LockupLight are the logo with the word motty beside it, as
// premultiplied-alpha PNGs four times the size they are drawn (138 x 36 logical
// points), rendered from logo-dark.svg and logo-light.svg with the word in the
// text color of the dark and of the light theme. The sidebar header paints
// them; a bitmap, not vector shapes, because the logo's soft shading and fine
// curves are the pictures' own anti-aliased pixels.
//
//go:embed lockup-dark.png
var LockupDark []byte

//go:embed lockup-light.png
var LockupLight []byte
