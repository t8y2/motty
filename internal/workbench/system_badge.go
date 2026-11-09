package workbench

import (
	"sync"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

// distroBadge is a distribution's white glyph on the color of its brand.
type distroBadge struct {
	icon  gfx.Icon
	brand gfx.Color
}

var distroBadges = map[sshconn.RemoteSystem]distroBadge{
	sshconn.SystemDebian:      {icons.Debian, gfx.RGB(168, 29, 51)},
	sshconn.SystemFedora:      {icons.Fedora, gfx.RGB(81, 162, 218)},
	sshconn.SystemAlpine:      {icons.Alpine, gfx.RGB(13, 89, 127)},
	sshconn.SystemArch:        {icons.Arch, gfx.RGB(23, 147, 209)},
	sshconn.SystemCentOS:      {icons.Centos, gfx.RGB(38, 37, 119)},
	sshconn.SystemRedHat:      {icons.Redhat, gfx.RGB(238, 0, 0)},
	sshconn.SystemRocky:       {icons.Rocky, gfx.RGB(16, 185, 129)},
	sshconn.SystemAlma:        {icons.Alma, gfx.RGB(15, 66, 102)},
	sshconn.SystemOpenSUSE:    {icons.Opensuse, gfx.RGB(115, 186, 37)},
	sshconn.SystemKali:        {icons.Kali, gfx.RGB(85, 124, 148)},
	sshconn.SystemMint:        {icons.Mint, gfx.RGB(134, 190, 67)},
	sshconn.SystemRaspberryPi: {icons.Raspberrypi, gfx.RGB(162, 40, 70)},
	sshconn.SystemFreeBSD:     {icons.Freebsd, gfx.RGB(171, 43, 40)},
	sshconn.SystemNixOS:       {icons.Nixos, gfx.RGB(82, 119, 195)},
	sshconn.SystemGentoo:      {icons.Gentoo, gfx.RGB(84, 72, 122)},
	sshconn.SystemManjaro:     {icons.Manjaro, gfx.RGB(53, 191, 92)},
	// The appliances and the derivatives below carry their brand color, darkened
	// where white on it would fall under a 3:1 contrast.
	sshconn.SystemSynology:       {icons.Synology, gfx.RGB(76, 82, 88)},
	sshconn.SystemQNAP:           {icons.Qnap, gfx.RGB(12, 46, 130)},
	sshconn.SystemFnOS:           {icons.Fnos, gfx.RGB(51, 116, 219)},
	sshconn.SystemTrueNAS:        {icons.Truenas, gfx.RGB(0, 149, 213)},
	sshconn.SystemUnraid:         {icons.Unraid, gfx.RGB(241, 90, 44)},
	sshconn.SystemOpenMediaVault: {icons.Openmediavault, gfx.RGB(61, 143, 192)},
	sshconn.SystemProxmox:        {icons.Proxmox, gfx.RGB(229, 112, 0)},
	sshconn.SystemOpenWrt:        {icons.Openwrt, gfx.RGB(0, 160, 204)},
	sshconn.SystemDeepin:         {icons.Deepin, gfx.RGB(0, 124, 255)},
	sshconn.SystemUOS:            {icons.Uos, gfx.RGB(0, 150, 235)},
	sshconn.SystemKylin:          {icons.Kylin, gfx.RGB(208, 18, 27)},
	sshconn.SystemPopOS:          {icons.Popos, gfx.RGB(46, 154, 166)},
	sshconn.SystemZorin:          {icons.Zorin, gfx.RGB(14, 134, 198)},
	sshconn.SystemElementary:     {icons.Elementary, gfx.RGB(47, 134, 212)},
	sshconn.SystemVoid:           {icons.Void, gfx.RGB(71, 128, 97)},
	sshconn.SystemSlackware:      {icons.Slackware, gfx.RGB(60, 60, 60)},
	// A vendor's own distribution carries the vendor's mark and color.
	sshconn.SystemAnolis:      {icons.Anolis, gfx.RGB(78, 154, 39)},
	sshconn.SystemTencentOS:   {icons.Tencentos, gfx.RGB(0, 82, 217)},
	sshconn.SystemAmazonLinux: {icons.Amazonlinux, gfx.RGB(35, 47, 62)},
	sshconn.SystemAzureLinux:  {icons.Azurelinux, gfx.RGB(0, 120, 212)},
	sshconn.SystemOracleLinux: {icons.Oraclelinux, gfx.RGB(199, 70, 52)},
	sshconn.SystemESXi:        {icons.Esxi, gfx.RGB(96, 112, 120)},
}

// badgeGlyph is one filled glyph of a system's badge: the icon, how far the badge's
// edge it is inset (a share of the badge's size) and its color.
type badgeGlyph struct {
	icon  gfx.Icon
	inset float32
	color gfx.Color
}

// badgeArt is what a system's badge is made of: a rounded square or a circle
// in the background color, with its glyphs on it.
type badgeArt struct {
	circle     bool
	background gfx.Color
	glyphs     []badgeGlyph
}

// systemBadgeArt describes the badge of a system, for a dark or a light page. It
// reports false for a system without a badge of its own.
func systemBadgeArt(system sshconn.RemoteSystem, dark bool) (badgeArt, bool) {
	white := gfx.RGB(255, 255, 255)
	ink := gfx.RGB(36, 41, 56)
	paper := gfx.RGB(244, 242, 237)
	background, foreground := ink, white
	if !dark {
		background, foreground = paper, ink
	}
	switch system {
	case sshconn.SystemUbuntu:
		return badgeArt{circle: true, background: gfx.RGB(221, 72, 20), glyphs: []badgeGlyph{{icons.Ubuntu, 0, white}}}, true
	case sshconn.SystemWindows:
		return badgeArt{background: background, glyphs: []badgeGlyph{{icons.Windows, 0, gfx.RGB(0, 173, 239)}}}, true
	case sshconn.SystemDarwin:
		return badgeArt{background: background, glyphs: []badgeGlyph{{icons.Apple, 0.16, foreground}}}, true
	case sshconn.SystemLinux:
		return badgeArt{background: background, glyphs: []badgeGlyph{
			{icons.Linux, 0.10, foreground}, {icons.LinuxDetail, 0.10, background}, {icons.LinuxAccent, 0.10, gfx.RGB(245, 192, 33)},
		}}, true
	}
	if badge, ok := distroBadges[system]; ok {
		// VMware's mark is a monogram two and a half times wider than it is tall,
		// so the square-mark inset would leave it a few pixels high.
		inset := float32(0.16)
		if system == sshconn.SystemESXi {
			inset = 0.10
		}
		return badgeArt{background: badge.brand, glyphs: []badgeGlyph{{badge.icon, inset, white}}}, true
	}
	return badgeArt{}, false
}

func paintSystemBadge(canvas *gfx.Canvas, system sshconn.RemoteSystem, bounds gfx.Rect, pal *palette) {
	const badgeRadius = float32(0.24)
	art, ok := systemBadgeArt(system, pal.dark)
	if !ok {
		canvas.Rounded(bounds, bounds.Width*badgeRadius, pal.accentSoft)
		canvas.DrawIcon(icons.Server, insetRect(bounds, 0.22), pal.accent)
		return
	}
	radius := bounds.Width * badgeRadius
	if art.circle {
		radius = bounds.Width / 2
	}
	canvas.Rounded(bounds, radius, art.background)
	for _, glyph := range art.glyphs {
		paintFilledIcon(canvas, glyph.icon, insetRect(bounds, glyph.inset), glyph.color)
	}
}

// badgeIconView is the side of the square the composed icons are drawn in.
const badgeIconView = float32(24)

var badgeIcons [2]struct {
	once  sync.Once
	icons map[sshconn.RemoteSystem]gfx.Icon
}

// systemBadgeIcon is a system's badge as one multi-color icon, for the places that
// take an icon (the items of a list) and not a painted shape. It reports false for a
// system without a badge of its own.
func systemBadgeIcon(system sshconn.RemoteSystem, dark bool) (gfx.Icon, bool) {
	slot := &badgeIcons[0]
	if dark {
		slot = &badgeIcons[1]
	}
	slot.once.Do(func() {
		slot.icons = make(map[sshconn.RemoteSystem]gfx.Icon)
		for candidate := sshconn.SystemLinux; candidate.String() != ""; candidate++ {
			if art, ok := systemBadgeArt(candidate, dark); ok {
				slot.icons[candidate] = composeBadgeIcon(art)
			}
		}
	})
	icon, ok := slot.icons[system]
	return icon, ok
}

func composeBadgeIcon(art badgeArt) gfx.Icon {
	const half = badgeIconView / 2
	radius := badgeIconView * 0.24
	if art.circle {
		radius = half
	}
	layers := []gfx.IconLayer{{Fill: art.background, Commands: roundedSquare(badgeIconView, radius)}}
	for pass := 0; pass < 2; pass++ {
		for _, glyph := range art.glyphs {
			layers = append(layers, gfx.IconLayer{Fill: glyph.color, Commands: scaledCommands(glyph.icon, glyph.inset)})
		}
	}
	return gfx.Icon{ViewBox: badgeIconView, Layers: layers}
}

// roundedSquare is a square of the given side with rounded corners, as path commands.
func roundedSquare(side, radius float32) []gfx.PathCommand {
	const kappa = float32(0.5522847)
	k := radius * kappa
	return []gfx.PathCommand{
		gfx.MoveTo(radius, 0),
		gfx.LineTo(side-radius, 0), gfx.CubicTo(side-radius+k, 0, side, radius-k, side, radius),
		gfx.LineTo(side, side-radius), gfx.CubicTo(side, side-radius+k, side-radius+k, side, side-radius, side),
		gfx.LineTo(radius, side), gfx.CubicTo(radius-k, side, 0, side-radius+k, 0, side-radius),
		gfx.LineTo(0, radius), gfx.CubicTo(0, radius-k, radius-k, 0, radius, 0),
		gfx.ClosePath(),
	}
}

// scaledCommands moves a glyph's path into the badge's square, inset from its edge by
// the given share of it. The glyph is drawn in one color, so it takes every path it
// has, whether the compiler left them at the top level or in layers for their fills.
func scaledCommands(icon gfx.Icon, inset float32) []gfx.PathCommand {
	margin := badgeIconView * inset
	scale := (badgeIconView - 2*margin) / icon.ViewBox
	place := func(point gfx.Point) gfx.Point {
		return gfx.Point{X: margin + point.X*scale, Y: margin + point.Y*scale}
	}
	commands := glyphCommands(icon)
	for index, command := range commands {
		command.Point, command.Control1, command.Control2 = place(command.Point), place(command.Control1), place(command.Control2)
		commands[index] = command
	}
	return commands
}

// glyphCommands is a glyph's geometry whatever form the compiler left it in: a
// single-fill icon carries its paths at the top level, one that names fill colors
// carries them in layers.
func glyphCommands(icon gfx.Icon) []gfx.PathCommand {
	if len(icon.Layers) == 0 {
		return append([]gfx.PathCommand(nil), icon.Commands...)
	}
	var commands []gfx.PathCommand
	for _, layer := range icon.Layers {
		commands = append(commands, layer.Commands...)
	}
	return commands
}

func paintFilledIcon(canvas *gfx.Canvas, icon gfx.Icon, bounds gfx.Rect, tint gfx.Color) {
	icon.Stroke = 0
	canvas.DrawIcon(icon, bounds, tint)
	canvas.DrawIcon(icon, bounds, tint)
}

func insetRect(bounds gfx.Rect, ratio float32) gfx.Rect {
	inset := bounds.Width * ratio
	return gfx.Rect{X: bounds.X + inset, Y: bounds.Y + inset, Width: bounds.Width - inset*2, Height: bounds.Height - inset*2}
}
