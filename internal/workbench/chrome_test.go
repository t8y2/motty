package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func hitByID(canvas *ui.Canvas, id ui.ID) (ui.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if hit.ID == id {
			return hit, true
		}
	}
	return ui.Hit{}, false
}

func TestChromeHasOnlyTheControlsThatDoSomething(t *testing.T) {
	state := newTestState(t, testOptions(t))
	canvas, texts := paintedTexts(state)

	for _, gone := range []string{"Notes", "Logs", "Vaults", "SFTP", "Scripts"} {
		if contains(texts, gone) {
			t.Errorf("%q is still painted", gone)
		}
	}
	// Every painted hit is one the state handles; none is a decoration waiting
	// for a feature.
	wanted := map[ui.ID]bool{
		hitHosts: true, hitKeychain: true, hitProxies: true, hitPortForwarding: true,
		hitKnownHosts: true, hitSettings: true, hitWindowSettings: true, hitWindowLocalTerminal: true, hitSearch: true, hitConnect: true,
		hitAddHost: true, hitHostFilterAll: true, hitImportMenu: true, hitLanguage: true, hitTheme: true, hitHome: true, hitNewTab: true,
	}
	// A development build also declares the component gallery.
	for _, id := range devOnlyShellHits() {
		wanted[id] = true
	}
	for _, hit := range canvas.Hits() {
		// The window chrome is what the shell declares itself; everything else
		// (host rows, the palette, the assistant, the menus) is another place's.
		if hit.ID.Scope() != shellHits || hit.Block {
			continue
		}
		if !wanted[hit.ID] {
			t.Errorf("unexpected hit %d (%q)", hit.ID, hit.Label)
		}
		delete(wanted, hit.ID)
	}
	for id := range wanted {
		t.Errorf("hit %d is not painted", id)
	}
}

func TestHostActionsAppearOnlyWhileSomethingIsChecked(t *testing.T) {
	state := newTestState(t, testOptions(t))
	actions := []ui.ID{hitExportHosts, hitEditHost, hitCopyHost, hitMoveHostGroup, hitDeleteHost}
	canvas, _ := paintedTexts(state)
	for _, id := range actions {
		if _, ok := hitByID(canvas, id); ok {
			t.Errorf("action %d is painted with nothing checked", id)
		}
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	canvas, _ = paintedTexts(state)
	for _, id := range actions {
		if _, ok := hitByID(canvas, id); !ok {
			t.Errorf("action %d is not painted with a host checked", id)
		}
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitDeleteHost); ok {
		t.Error("the actions stayed after the last check was removed")
	}
}

func TestWholeCatalogExportIsInTheImportMenu(t *testing.T) {
	options := testOptions(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return "", app.ErrCanceled } // never a real dialog
	state := newTestState(t, options)
	state.Click(hitImportMenu)
	canvas, _ := paintedTexts(state)
	hit, ok := hitByID(canvas, hitExportHosts)
	if !ok || hit.Disabled {
		t.Fatalf("no usable export in the import menu: %+v, %v", hit, ok)
	}
	state.Click(hitExportHosts)
	if state.importMenu {
		t.Fatal("choosing it left the menu open")
	}
}

func TestThemeToggleSwitchesPaletteAndComponentTheme(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.appearance != appearanceDark || state.Theme().Background != darkPalette.workspace {
		t.Fatal("the default is not the dark palette")
	}
	darkClear := state.WindowColor()

	state.Click(hitTheme)
	if state.appearance != appearanceLight {
		t.Fatal("the toggle did not switch to light")
	}
	theme := state.Theme()
	if theme.Background != lightPalette.workspace || theme.Foreground != lightPalette.text ||
		theme.Primary != lightPalette.accent || theme.Border != lightPalette.line {
		t.Fatalf("the component theme did not follow the palette: %+v", theme)
	}
	if state.WindowColor() == darkClear {
		t.Fatal("the window color did not follow the theme")
	}
	canvas, _ := paintedTexts(state)
	theme2, _ := hitByID(canvas, hitTheme)
	if theme2.Label != state.messages.LabelThemeToDark {
		t.Fatalf("label after switching = %q", theme2.Label)
	}

	state.Click(hitTheme)
	if state.appearance != appearanceDark || state.Theme().Background != darkPalette.workspace {
		t.Fatal("the second toggle did not return to dark")
	}
}

// Text must stay readable on whatever it sits on. This is a contrast floor, not
// a design review: it catches a palette entry set to the wrong value.
func TestPalettesKeepTextReadable(t *testing.T) {
	luminance := func(c ui.Color) float64 {
		channel := func(v float32) float64 {
			x := float64(v)
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(c.Red) + 0.7152*channel(c.Green) + 0.0722*channel(c.Blue)
	}
	ratio := func(a, b ui.Color) float64 {
		la, lb := luminance(a), luminance(b)
		if la < lb {
			la, lb = lb, la
		}
		return (la + 0.05) / (lb + 0.05)
	}
	for name, pal := range map[string]*palette{"dark": &darkPalette, "light": &lightPalette} {
		for _, check := range []struct {
			what    string
			fg, bg  ui.Color
			minimum float64
		}{
			{"text on workspace", pal.text, pal.workspace, 7},
			{"text on shell", pal.text, pal.shell, 7},
			{"text on surface", pal.text, pal.surface, 7},
			{"muted on workspace", pal.muted, pal.workspace, 4.5},
			{"muted on shell", pal.muted, pal.shell, 4.5},
			{"muted on surface", pal.muted, pal.surface, 4.5},
			{"faint on workspace", pal.faint, pal.workspace, 3},
			{"accent label on soft tint", pal.accent, pal.accentSoft, 3},
			{"label on accent button", pal.onAccent, pal.accent, 4.5},
			{"label on hot accent button", pal.onAccent, pal.accentHot, 4.5},
			{"success on workspace", pal.success, pal.workspace, 3},
			{"danger on workspace", pal.danger, pal.workspace, 3},
			{"text on selected row", pal.text, pal.surfaceHot, 7},
		} {
			if got := ratio(check.fg, check.bg); got < check.minimum {
				t.Errorf("%s: %s contrast %.2f is below %.1f", name, check.what, got, check.minimum)
			}
		}
	}
}

func TestDialogFieldsShowEachHintOnlyOnce(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	_, texts := paintedTexts(state)
	m := state.messages
	for _, placeholder := range []string{m.FieldLabelPlaceholder, m.FieldAddressPlaceholder, m.FieldUserPlaceholder, m.FieldGroupPlaceholder} {
		count := 0
		for _, value := range texts {
			if value == placeholder {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%q is painted %d times, want exactly once (as the placeholder)", placeholder, count)
		}
	}
}

func TestDialogButtonsFitTheirLabelsAndSitAtTheRightEdge(t *testing.T) {
	cases := []struct {
		name   string
		open   func(*State, *fakeConn)
		layout []ui.ID
	}{
		{"new host", func(state *State, _ *fakeConn) { state.Click(hitAddHost) }, []ui.ID{hitDialogCancel, hitDialogSubmit}},
		{"host key", func(state *State, fake *fakeConn) {
			fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseHostKeyPrompt; s.HostKey = testHostKey })
			state.Wake()
		}, []ui.ID{hitHostKeyReject, hitHostKeyOnce, hitHostKeyTrust}},
		{"password", func(state *State, fake *fakeConn) {
			fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhasePasswordPrompt; s.Attempt = 1 })
			state.Wake()
		}, []ui.ID{hitPasswordCancel, hitPasswordSubmit}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			world, options := newWorld(t)
			state := newTestState(t, options)
			fake := connect(t, state, world)
			test.open(state, fake)
			canvas, _ := paintedTexts(state)
			theme := state.Theme()

			var previousRight float32
			var last ui.Hit
			for index, id := range test.layout {
				hit, ok := hitByID(canvas, id)
				if !ok {
					t.Fatalf("button %d is not painted", id)
				}
				fit := components.BeginButton(canvas, theme, ui.ID{}, hit.Bounds).SizeToFit(hit.Label).BoundsWidth()
				if math.Abs(float64(hit.Bounds.Width-fit)) > 0.5 {
					t.Errorf("%q is %.1f wide, its label fits in %.1f", hit.Label, hit.Bounds.Width, fit)
				}
				if hit.Bounds.Width > canvas.TextWidth(hit.Label, theme.Metrics.FontSizeM)+40 {
					t.Errorf("%q has more than 20px of padding per side", hit.Label)
				}
				if index > 0 && math.Abs(float64(hit.Bounds.X-previousRight-theme.Metrics.GapItem)) > 0.5 {
					t.Errorf("gap before %q is %.1f, want %.1f", hit.Label, hit.Bounds.X-previousRight, theme.Metrics.GapItem)
				}
				previousRight = hit.Bounds.X + hit.Bounds.Width
				last = hit
			}
			// The last button ends where the dialog's content ends.
			var content ui.Rect
			for _, hit := range canvas.Hits() {
				if hit.Block {
					content = hit.Bounds
				}
			}
			padding := theme.Metrics.PadSurface + theme.Metrics.GapItem
			if want := content.X + content.Width - padding; math.Abs(float64(last.Bounds.X+last.Bounds.Width-want)) > 0.5 {
				t.Errorf("last button ends at %.1f, the footer's right edge is %.1f", last.Bounds.X+last.Bounds.Width, want)
			}
		})
	}
}

func TestPaintDoesNotAllocateInEitherThemeOrWithADialogOpen(t *testing.T) {
	for _, appearance := range []appearance{appearanceDark, appearanceLight} {
		for _, open := range []struct {
			name string
			do   func(*State)
		}{
			{"hosts", func(*State) {}},
			{"new host dialog", func(state *State) { state.Click(hitAddHost) }},
			{"empty", func(state *State) { state.hosts = nil; state.selectedHostID = 0 }},
		} {
			state := newTestState(t, testOptions(t))
			state.setAppearance(appearance)
			open.do(state)
			var canvas ui.Canvas
			size := ui.Size{Width: 1400, Height: 900}
			state.Paint(&canvas, size)
			allocations := testing.AllocsPerRun(50, func() {
				canvas.Reset()
				state.Paint(&canvas, size)
			})
			if allocations != 0 {
				t.Errorf("%v / %s: paint allocated %v times", appearance, open.name, allocations)
			}
		}
	}
}

// Text must not change size when a field gains focus: the native editor takes
// over at the size the field declared, which is the size it painted. Both are
// the workbench's body size.
func TestFieldTextHasTheSameSizeFocusedAndUnfocused(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.editor.label.Insert("Production")
	canvas, _ := paintedTexts(state)

	var hit ui.Hit
	for _, candidate := range canvas.Hits() {
		if candidate.ID == hitDialogLabel {
			hit = candidate
		}
	}
	var painted ui.Text
	for _, candidate := range canvas.Texts() {
		if candidate.Value == "Production" {
			painted = candidate
		}
	}
	if hit.ID.IsZero() || painted.Value == "" {
		t.Fatalf("hit=%+v painted=%+v", hit, painted)
	}
	if painted.Style.Size != metrics.FontSizeM || hit.TextSize != painted.Style.Size {
		t.Fatalf("painted size %v, declared to the native editor %v, want both %v", painted.Style.Size, hit.TextSize, metrics.FontSizeM)
	}
	if hit.TextBounds.X != painted.Bounds.X {
		t.Fatalf("native editor would start at x=%v, the painted text at %v", hit.TextBounds.X, painted.Bounds.X)
	}
}
