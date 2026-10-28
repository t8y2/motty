package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

// Viva's Windows backend currently delivers Space both as a native key and as
// committed WM_CHAR text. Once the key has sent its space, discard only the
// immediately following duplicate text event. Native console identity limits
// this compatibility guard to Windows; other backends keep their input paths.
func (t *terminal) noteSpaceKey(event ui.KeyEvent, accepted bool) {
	t.spaceTextPending = accepted && !event.Release && event.Key == ui.KeySpace &&
		event.Modifiers&^ui.ModifierShift == 0 &&
		event.Win32.VirtualKey == 32 && event.Win32.Unicode == ' '
}

// Any other text event, including paste or IME composition, ends the pairing.
// Key handlers clear it too, so a backend that delivers Space only once cannot
// leave a pending pair across a release or the next key.
func (t *terminal) duplicateSpaceText(event app.TextEvent) bool {
	pending := t.spaceTextPending
	t.spaceTextPending = false
	return pending && event.Kind == app.TextInsert && event.Text == " "
}
