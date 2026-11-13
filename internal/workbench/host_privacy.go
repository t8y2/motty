package workbench

import (
	"math"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var hostPrivacyHits = tree.Feature("host-privacy")

var (
	hitHostPrivacy        = tree.ID(hostPrivacyHits, "toggle-menu")
	hitHostPrivacyDismiss = tree.ID(hostPrivacyHits, "dismiss")
	hitHostPrivacyEnabled = tree.ID(hostPrivacyHits, "enabled")
	hitHostPrivacyName    = tree.ID(hostPrivacyHits, "name")
	hitHostPrivacyAddress = tree.ID(hostPrivacyHits, "address")
	hitHostPrivacyUser    = tree.ID(hostPrivacyHits, "user")
)

type hostPrivacyState struct {
	enabled     bool
	maskName    bool
	maskAddress bool
	maskUser    bool
	menu        bool
	button      gfx.Rect
	blur        hostPrivacyBlurCache
}

type hostPrivacyBlurKey struct {
	value          string
	size           float32
	weight         float32
	italic         bool
	color          gfx.Color
	scale          float32
	width          int
	height         int
	fontGeneration uint32
}

type hostPrivacyBlurCache struct {
	bitmaps map[hostPrivacyBlurKey]gfx.Bitmap
	nextID  uint64
}

func (privacy hostPrivacyState) nameHidden() bool {
	return privacy.enabled && privacy.maskName
}

func (privacy hostPrivacyState) addressHidden() bool {
	return privacy.enabled && privacy.maskAddress
}

func (privacy hostPrivacyState) userHidden() bool {
	return privacy.enabled && privacy.maskUser
}

func (state *State) hostPrivacySemanticLabel(host Host) string {
	if state.hostPrivacy.nameHidden() {
		return state.messages.Tools.HostPrivacyHiddenHost
	}
	return host.Label
}

func (state *State) paintHostPrivacyMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	m := state.messages.Tools
	menu := components.BeginDropdownMenu(
		canvas,
		state.Theme(),
		state.popupBelow(canvas, state.hostPrivacy.button, 220, 176, ui.End),
		components.DropdownMenuOptions{
			Layer: dialogLayer, Scope: dialogScope, DismissID: hitHostPrivacyDismiss,
			Viewport: viewport, Scroll: state.menuScrollFor(hitHostPrivacyDismiss),
		},
	)
	state.menuPlaced(menu.Bounds())
	menu.AddItem(components.DropdownMenuItem{
		ID: hitHostPrivacyEnabled, Label: m.HostPrivacy, Icon: lucideIcons[iconEyeOff],
		Kind: components.DropdownMenuCheckbox, Checked: state.hostPrivacy.enabled,
	})
	menu.AddSeparator()
	menu.AddLabel(m.HostPrivacyFields, false)
	menu.AddItem(components.DropdownMenuItem{
		ID: hitHostPrivacyName, Label: m.HostPrivacyName, Inset: true,
		Kind: components.DropdownMenuCheckbox, Checked: state.hostPrivacy.maskName,
	})
	menu.AddItem(components.DropdownMenuItem{
		ID: hitHostPrivacyAddress, Label: m.HostPrivacyAddress, Inset: true,
		Kind: components.DropdownMenuCheckbox, Checked: state.hostPrivacy.maskAddress,
	})
	menu.AddItem(components.DropdownMenuItem{
		ID: hitHostPrivacyUser, Label: m.HostPrivacyUser, Inset: true,
		Kind: components.DropdownMenuCheckbox, Checked: state.hostPrivacy.maskUser,
	})
	menu.End()
}

func (state *State) clickHostPrivacy(id gfx.ID) bool {
	if state.hostPrivacy.menu {
		state.hostPrivacy.menu = false
		switch id {
		case hitHostPrivacyEnabled:
			state.hostPrivacy.enabled = !state.hostPrivacy.enabled
		case hitHostPrivacyName:
			state.hostPrivacy.maskName = !state.hostPrivacy.maskName
		case hitHostPrivacyAddress:
			state.hostPrivacy.maskAddress = !state.hostPrivacy.maskAddress
		case hitHostPrivacyUser:
			state.hostPrivacy.maskUser = !state.hostPrivacy.maskUser
		case hitHostPrivacy, hitHostPrivacyDismiss:
			return true
		default:
			return true
		}
		state.save()
		return true
	}
	if id != hitHostPrivacy {
		return false
	}
	state.hostPrivacy.menu = true
	state.hostOrderMenu = false
	state.importMenu = false
	state.hostFilterMenu = false
	state.hostGroupMenu = false
	return true
}

const (
	// Keep slightly more than three sigma of transparent space around the
	// rasterized line so the Gaussian tail reaches zero before the bitmap edge.
	hostPrivacyBlurPadding = float32(10)
	hostPrivacyBlurSigma   = float32(2.9)
	// Privacy masks are deliberately low-frequency. Rasterizing them at logical
	// 1x avoids paying Retina pixel cost for detail the Gaussian immediately
	// removes; DrawBitmap scales the cached result to the same logical bounds.
	hostPrivacyRasterScale  = float32(1)
	hostPrivacyBitmapIDBase = uint64(0x6870726976000000)
	hostPrivacyBlurCacheMax = 512
)

func (state *State) paintHostPrivacyMask(canvas *gfx.Canvas, bounds gfx.Rect, value string, style gfx.TextStyle) {
	bitmap, width, height := state.hostPrivacyBlurBitmap(canvas, value, style, bounds)
	if len(bitmap.Pix) == 0 || width <= 0 || height <= 0 {
		return
	}
	// The source text starts exactly at bounds.X/Y inside the padded bitmap.
	// Let the low-alpha Gaussian tail extend naturally instead of clipping it to
	// the text line, which otherwise leaves a visible rectangular cutoff.
	canvas.DrawBitmap(bitmap, gfx.Rect{
		X:      bounds.X - hostPrivacyBlurPadding,
		Y:      bounds.Y - hostPrivacyBlurPadding,
		Width:  width,
		Height: height,
	})
}

func (state *State) hostPrivacyBlurBitmap(canvas *gfx.Canvas, value string, style gfx.TextStyle, bounds gfx.Rect) (gfx.Bitmap, float32, float32) {
	if value == "" || bounds.Width <= 0 || bounds.Height <= 0 {
		return gfx.Bitmap{}, 0, 0
	}
	scale := hostPrivacyRasterScale
	textWidth := min(canvas.TextWidthStyle(value, style)+1, max(bounds.Width-hostPrivacyBlurPadding, 1))
	pixelWidth := max(int(math.Ceil(float64(textWidth*scale))), 1)
	pixelHeight := max(int(math.Ceil(float64(bounds.Height*scale))), 1)
	key := hostPrivacyBlurKey{
		value: value, size: style.Size, weight: style.Weight, italic: style.Italic,
		color: style.Color, scale: scale, width: pixelWidth, height: pixelHeight,
		fontGeneration: canvas.FontGeneration(),
	}

	cache := &state.hostPrivacy.blur
	if cache.bitmaps == nil {
		cache.bitmaps = make(map[hostPrivacyBlurKey]gfx.Bitmap)
	}
	if bitmap, ok := cache.bitmaps[key]; ok {
		return bitmap, float32(bitmap.Width) / scale, float32(bitmap.Height) / scale
	}

	source, err := canvas.RasterizeText(value, style, scale, pixelWidth, pixelHeight)
	if err != nil || len(source.Pix) == 0 {
		return gfx.Bitmap{}, 0, 0
	}

	pad := max(int(math.Ceil(float64(hostPrivacyBlurPadding*scale))), 1)
	paddedWidth := source.Width + 2*pad
	paddedHeight := source.Height + 2*pad
	padded := gfx.Bitmap{
		Pix:   make([]byte, paddedWidth*paddedHeight*4),
		Width: paddedWidth, Height: paddedHeight, Stride: paddedWidth * 4,
	}
	for y := 0; y < source.Height; y++ {
		src := source.Pix[y*source.Stride : y*source.Stride+source.Width*4]
		dst := padded.Pix[(y+pad)*padded.Stride+pad*4:]
		copy(dst, src)
	}

	blurred := gfx.GaussianBlurBitmap(padded, hostPrivacyBlurSigma*scale)
	cache.nextID++
	if cache.nextID == 0 {
		cache.nextID = 1
	}
	blurred.ID = hostPrivacyBitmapIDBase + cache.nextID
	blurred.Version = 1
	if len(cache.bitmaps) >= hostPrivacyBlurCacheMax {
		clear(cache.bitmaps)
	}
	cache.bitmaps[key] = blurred
	return blurred, float32(blurred.Width) / scale, float32(blurred.Height) / scale
}

func hostPrivacyMaskedWidth(canvas *gfx.Canvas, value string, style gfx.TextStyle, available float32) float32 {
	if available <= 0 || value == "" {
		return 0
	}
	// The left blur tail may extend outside this logical slot; reserve the
	// right tail so following visible content does not collide with it.
	return min(canvas.TextWidthStyle(value, style)+1+hostPrivacyBlurPadding, available)
}

func (state *State) hostPrivacySubtitleIsUser(host Host) bool {
	switch host.Protocol {
	case protocolVNC, protocolRDP:
		return host.User != ""
	default:
		return true
	}
}

func hostPrivacyVisibleWidth(canvas *gfx.Canvas, value string, style gfx.TextStyle, available float32) float32 {
	if available <= 0 || value == "" {
		return 0
	}
	return min(canvas.TextWidthStyle(value, style)+1, available)
}

// hostPrivacyVNCTargetParts splits the one target that carries an account.
// Direct VNC targets have no account in their address column.
func hostPrivacyVNCTargetParts(host Host) (prefix, account, endpoint string, ok bool) {
	if host.Protocol != protocolVNC || !host.ViaSSH || host.User == "" {
		return "", "", "", false
	}
	prefix = "vnc+ssh://"
	account = host.User + "@"
	rest, found := strings.CutPrefix(host.target, prefix)
	if !found {
		return "", "", "", false
	}
	endpoint, found = strings.CutPrefix(rest, account)
	if !found {
		return "", "", "", false
	}
	return prefix, account, endpoint, true
}

func (state *State) hostPrivacyTargetWidth(canvas *gfx.Canvas, host Host, style gfx.TextStyle, available float32) float32 {
	if available <= 0 {
		return 0
	}
	prefix, account, endpoint, split := hostPrivacyVNCTargetParts(host)
	if !split {
		if state.hostPrivacy.addressHidden() && host.Protocol != protocolSerial {
			return hostPrivacyMaskedWidth(canvas, host.target, style, available)
		}
		return hostPrivacyVisibleWidth(canvas, host.target, style, available)
	}

	prefixWidth := hostPrivacyVisibleWidth(canvas, prefix, style, available)
	remaining := max(available-prefixWidth, 0)
	switch {
	case state.hostPrivacy.userHidden() && state.hostPrivacy.addressHidden():
		return min(prefixWidth+hostPrivacyMaskedWidth(canvas, account+endpoint, style, remaining), available)
	case state.hostPrivacy.userHidden():
		accountWidth := hostPrivacyMaskedWidth(canvas, account, style, remaining)
		endpointWidth := hostPrivacyVisibleWidth(canvas, endpoint, style, max(remaining-accountWidth, 0))
		return min(prefixWidth+accountWidth+endpointWidth, available)
	case state.hostPrivacy.addressHidden():
		accountWidth := hostPrivacyVisibleWidth(canvas, account, style, remaining)
		endpointWidth := hostPrivacyMaskedWidth(canvas, endpoint, style, max(remaining-accountWidth, 0))
		return min(prefixWidth+accountWidth+endpointWidth, available)
	default:
		return hostPrivacyVisibleWidth(canvas, host.target, style, available)
	}
}

func (state *State) paintHostPrivacyTarget(canvas *gfx.Canvas, bounds gfx.Rect, host Host, leading float32, style gfx.TextStyle) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	prefix, account, endpoint, split := hostPrivacyVNCTargetParts(host)
	if !split {
		if state.hostPrivacy.addressHidden() && host.Protocol != protocolSerial {
			state.paintHostPrivacyMask(canvas, bounds, host.target, style)
			return
		}
		paintHostGridLine(canvas, host.target, bounds, leading, style)
		return
	}

	prefixWidth := hostPrivacyVisibleWidth(canvas, prefix, style, bounds.Width)
	paintHostGridLine(canvas, prefix, gfx.Rect{X: bounds.X, Y: bounds.Y, Width: prefixWidth, Height: bounds.Height}, leading, style)
	x := bounds.X + prefixWidth
	remaining := max(bounds.X+bounds.Width-x, 0)
	if remaining <= 0 {
		return
	}

	switch {
	case state.hostPrivacy.userHidden() && state.hostPrivacy.addressHidden():
		state.paintHostPrivacyMask(canvas, gfx.Rect{X: x, Y: bounds.Y, Width: remaining, Height: bounds.Height}, account+endpoint, style)
	case state.hostPrivacy.userHidden():
		accountWidth := hostPrivacyMaskedWidth(canvas, account, style, remaining)
		state.paintHostPrivacyMask(canvas, gfx.Rect{X: x, Y: bounds.Y, Width: accountWidth, Height: bounds.Height}, account, style)
		x += accountWidth
		if x < bounds.X+bounds.Width {
			paintHostGridLine(canvas, endpoint, gfx.Rect{X: x, Y: bounds.Y, Width: bounds.X + bounds.Width - x, Height: bounds.Height}, leading, style)
		}
	case state.hostPrivacy.addressHidden():
		accountWidth := hostPrivacyVisibleWidth(canvas, account, style, remaining)
		paintHostGridLine(canvas, account, gfx.Rect{X: x, Y: bounds.Y, Width: accountWidth, Height: bounds.Height}, leading, style)
		x += accountWidth
		if x < bounds.X+bounds.Width {
			state.paintHostPrivacyMask(canvas, gfx.Rect{X: x, Y: bounds.Y, Width: bounds.X + bounds.Width - x, Height: bounds.Height}, endpoint, style)
		}
	default:
		paintHostGridLine(canvas, account+endpoint, gfx.Rect{X: x, Y: bounds.Y, Width: remaining, Height: bounds.Height}, leading, style)
	}
}

func (state *State) hostPrivacyConnectionWidth(canvas *gfx.Canvas, host Host, available float32) float32 {
	if available <= 0 {
		return 0
	}
	style := gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted}
	if host.Protocol == protocolVNC {
		return state.hostPrivacyTargetWidth(canvas, host, style, available)
	}
	targetOnly := host.Protocol == protocolSerial || (host.Protocol == protocolRDP && host.User == "")
	if targetOnly {
		return state.hostPrivacyTargetWidth(canvas, host, style, available)
	}

	user := state.hostSubtitle(host)
	userHidden := state.hostPrivacy.userHidden()
	addressHidden := state.hostPrivacy.addressHidden()
	switch {
	case userHidden && addressHidden:
		return hostPrivacyMaskedWidth(canvas, user+" · "+host.target, style, available)
	case userHidden:
		hiddenWidth := hostPrivacyMaskedWidth(canvas, user+" · ", style, available)
		targetWidth := canvas.TextWidthStyle(host.target, style) + 1
		return min(hiddenWidth+targetWidth, available)
	case addressHidden:
		userWidth := canvas.TextWidthStyle(user, style) + 1
		hiddenWidth := hostPrivacyMaskedWidth(canvas, " · "+host.target, style, max(available-userWidth, 0))
		return min(userWidth+hiddenWidth, available)
	default:
		return min(canvas.TextWidthStyle(user+" · "+host.target, style)+1, available)
	}
}

func (state *State) paintHostPrivacyConnection(canvas *gfx.Canvas, bounds gfx.Rect, host Host) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	style := gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted}
	if host.Protocol == protocolVNC {
		state.paintHostPrivacyTarget(canvas, bounds, host, 19, style)
		return
	}
	targetOnly := host.Protocol == protocolSerial || (host.Protocol == protocolRDP && host.User == "")
	if targetOnly {
		state.paintHostPrivacyTarget(canvas, bounds, host, 19, style)
		return
	}

	user := state.hostSubtitle(host)
	userHidden := state.hostPrivacy.userHidden()
	addressHidden := state.hostPrivacy.addressHidden()
	switch {
	case userHidden && addressHidden:
		state.paintHostPrivacyMask(canvas, bounds, user+" · "+host.target, style)
		return
	case userHidden:
		hidden := user + " · "
		hiddenWidth := hostPrivacyMaskedWidth(canvas, hidden, style, bounds.Width)
		state.paintHostPrivacyMask(canvas, gfx.Rect{X: bounds.X, Y: bounds.Y, Width: hiddenWidth, Height: bounds.Height}, hidden, style)
		x := bounds.X + hiddenWidth
		if x < bounds.X+bounds.Width {
			paintHostGridLine(canvas, host.target, gfx.Rect{X: x, Y: bounds.Y, Width: bounds.X + bounds.Width - x, Height: bounds.Height}, 19, style)
		}
		return
	case addressHidden:
		userWidth := hostPrivacyVisibleWidth(canvas, user, style, bounds.Width)
		paintHostGridLine(canvas, user, gfx.Rect{X: bounds.X, Y: bounds.Y, Width: userWidth, Height: bounds.Height}, 19, style)
		x := bounds.X + userWidth
		if x < bounds.X+bounds.Width {
			state.paintHostPrivacyMask(canvas, gfx.Rect{X: x, Y: bounds.Y, Width: bounds.X + bounds.Width - x, Height: bounds.Height}, " · "+host.target, style)
		}
		return
	default:
		paintHostGridLine(canvas, user+" · "+host.target, bounds, 19, style)
	}
}
