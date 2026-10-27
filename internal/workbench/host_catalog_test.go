package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

func TestHostCatalogTreeFilterSelectionAndBatchMoveCompose(t *testing.T) {
	state := newTestState(t, testOptions(t))
	servers := state.hostGroupIndex("Servers")
	if servers < 0 {
		t.Fatalf("groups = %+v", state.hostGroups)
	}
	state.Click(hitHostGroupOpenList.At(servers))
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostList.At(int(state.hosts[0].ID))); ok {
		t.Fatal("collapsed group still exposes a host row")
	}
	if _, ok := hitByID(canvas, hitHostList.At(int(state.hosts[2].ID))); !ok {
		t.Fatal("collapsing Servers hid Network")
	}

	typeSearch(state, "Production")
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostList.At(int(state.hosts[0].ID))); !ok {
		t.Fatal("filter did not reveal a match inside a collapsed group")
	}
	state.Click(hitHostCheckList.At(int(state.hosts[0].ID)))
	state.search = text.Editor{}
	state.syncVisible()
	if !state.hostSelected(state.hosts[0].ID) {
		t.Fatal("clearing the filter lost the hidden selection")
	}

	clickHostAction(state, hitMoveHostGroup)
	network := state.hostGroupIndex("Network")
	state.Click(hitHostGroupItemList.At(network))
	if state.hosts[0].Group != "Network" || !state.hostSelected(state.hosts[0].ID) {
		t.Fatalf("batch move result host=%+v selection=%v", state.hosts[0], state.hostSelections)
	}
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostList.At(int(state.hosts[0].ID))); !ok {
		t.Fatal("moved selected host was not placed in the expanded target branch")
	}
}

func TestHostTagsAndCompactIconEditPersistAndFilter(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitHostList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitEditHost)
	state.editor.tags = text.Editor{}
	for _, tag := range []string{"prod", "critical", "PROD"} {
		typeInto(state, hitDialogTags, tag)
		state.Key(ui.KeyEvent{Target: hitDialogTags, Key: ui.KeyEnter})
	}
	state.Click(hitHostTagsDismiss)
	state.Click(hitDialogIcon)
	state.Click(hitDialogIconItemList.At(int(hostIconTerminal)))
	state.Click(hitDialogSubmit)
	host := state.hosts[0]
	if host.Tags != "prod, critical" || host.Icon != hostIconTerminal {
		t.Fatalf("edited host = %+v", host)
	}
	typeSearch(state, "critical")
	if len(state.visible) != 1 || state.hosts[state.visible[0]].ID != host.ID {
		t.Fatalf("tag filter = %v", state.visible)
	}
	restarted := newTestState(t, options)
	if restarted.hosts[0].Tags != "prod, critical" || restarted.hosts[0].Icon != hostIconTerminal || restarted.hosts[0].CatalogID != host.CatalogID {
		t.Fatalf("restored host = %+v", restarted.hosts[0])
	}
}

func TestHostCatalogExportImportRoundTripContainsNoSecrets(t *testing.T) {
	exportPath := filepath.Join(t.TempDir(), "catalog.json")
	options := testOptions(t)
	var saveOptions app.SaveFileOptions
	options.SaveFile = func(options app.SaveFileOptions) (string, error) {
		saveOptions = options
		return exportPath, nil
	}
	state := newTestState(t, options)
	setHostTags(&state.hosts[0], []string{"prod", "critical"})
	state.hosts[0].Icon = hostIconGlobe
	servers := state.hostGroupIndex("Servers")
	state.toggleHostGroup(servers)
	state.Click(hitExportHosts)
	if saveOptions.DefaultExtension != "json" || saveOptions.SuggestedName != "motty-hosts.json" {
		t.Fatalf("save options = %+v", saveOptions)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(data))
	for _, forbidden := range []string{"password", "passphrase", "secret", "identity_files", "private_key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("export contains forbidden field %q: %s", forbidden, data)
		}
	}
	var document hostCatalogDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Format != hostCatalogFormat || document.Version != hostCatalogSchemaVersion || len(document.Hosts) != 3 {
		t.Fatalf("document = %+v", document)
	}

	importOptions := freshOptions(t)
	importOptions.OpenFiles = func(options app.OpenFileOptions) ([]string, error) {
		if options.Title == "" || len(options.Filters) != 1 {
			t.Fatalf("open options = %+v", options)
		}
		return []string{exportPath}, nil
	}
	imported := New(importOptions)
	t.Cleanup(imported.Close)
	imported.Click(hitImportHostCatalog)
	if len(imported.hosts) != len(state.hosts) {
		t.Fatalf("imported hosts = %+v", imported.hosts)
	}
	for index, want := range state.hosts {
		got := imported.hosts[index]
		if got.CatalogID != want.CatalogID || got.Label != want.Label || got.Group != want.Group || got.Address != want.Address || got.User != want.User || got.Port != want.Port || got.Icon != want.Icon || formatHostTags(got) != formatHostTags(want) {
			t.Fatalf("round trip host %d: got=%+v want=%+v", index, got, want)
		}
	}
	if imported.hostGroups[imported.hostGroupIndex("Servers")].Expanded {
		t.Fatal("collapsed group did not round trip")
	}

	imported.Click(hitImportHostCatalog)
	if len(imported.hosts) != 3 || !strings.Contains(imported.status, "3") {
		t.Fatalf("duplicate import hosts=%d status=%q", len(imported.hosts), imported.status)
	}
}

func TestHostCatalogStillImportsVersionOneDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "motty-hosts-v1.json")
	document := `{"format":"motty-host-catalog","version":1,"hosts":[{"id":"host_old","label":"Old","address":"old.example.com","port":22}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := readHostCatalog(path)
	if err != nil || len(read.Hosts) != 1 || read.Hosts[0].Label != "Old" {
		t.Fatalf("version-one catalog = %+v, %v", read, err)
	}
}

func TestHostCatalogExportsCheckedSubsetAndHandlesPickerErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected.json")
	options := testOptions(t)
	options.SaveFile = func(app.SaveFileOptions) (string, error) { return path, nil }
	state := newTestState(t, options)
	state.Click(hitHostCheckList.At(int(state.hosts[1].ID)))
	state.Click(hitExportHosts)
	document, err := readHostCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Hosts) != 1 || document.Hosts[0].Label != "Staging" || len(document.Groups) != 1 || document.Groups[0].Name != "Servers" {
		t.Fatalf("selected export = %+v", document)
	}

	options = freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return nil, errors.New("picker failed") }
	state = newTestState(t, options)
	state.Click(hitImportHostCatalog)
	if len(state.hosts) != 0 || !strings.Contains(state.status, "picker failed") {
		t.Fatalf("failed import hosts=%d status=%q", len(state.hosts), state.status)
	}
}

func TestHostCatalogTreeSteadyPaintIsAllocationFree(t *testing.T) {
	state := newTestState(t, testOptions(t))
	setHostTags(&state.hosts[0], []string{"prod", "critical"})
	state.hosts[0].Icon = hostIconTerminal
	state.hostDataChanged()
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("steady host catalog paint allocated %v times", allocations)
	}
}

func BenchmarkPaintHostCatalogTree(b *testing.B) {
	state := newTestState(b, testOptions(b))
	setHostTags(&state.hosts[0], []string{"prod", "critical"})
	state.hosts[0].Icon = hostIconTerminal
	state.hostDataChanged()
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ResetTimer()
	b.ReportMetric(float64(unsafe.Sizeof(Host{})), "bytes/host")
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// A host can be given a distribution's badge by hand, the choice survives a restart,
// and editing the host again starts from it instead of from the automatic icon.
func TestHostIconCanBeASystemBadgeAndSurvivesEditingAndRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitHostList.At(int(state.hosts[0].ID)))
	clickHostAction(state, hitEditHost)
	state.Click(hitDialogIcon)
	state.Click(hitDialogIconItemList.At(int(hostIconOf(sshconn.SystemUbuntu))))
	state.Click(hitDialogSubmit)
	if got := state.hosts[0].Icon; got != hostIconOf(sshconn.SystemUbuntu) {
		t.Fatalf("icon after choosing Ubuntu = %v", got)
	}
	clickHostAction(state, hitEditHost)
	if state.editor.icon != hostIconOf(sshconn.SystemUbuntu) {
		t.Fatalf("editing the host started from icon %v, want the one it has", state.editor.icon)
	}
	restarted := newTestState(t, options)
	if got := restarted.hosts[0].Icon; got != hostIconOf(sshconn.SystemUbuntu) || got.String() != "ubuntu" {
		t.Fatalf("restored icon = %v (%q)", got, got.String())
	}
}

// The list of icons has more entries than fit, so it scrolls, and an entry far
// down it can still be reached and chosen.
func TestHostIconListScrollsToTheEntriesBelow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	clickHostAction(state, hitAddHost)
	state.Click(hitDialogIcon)
	canvas, _ := paintedTexts(state)
	last := hitDialogIconItemList.At(int(hostIconDesktop))
	if _, ok := hitByID(canvas, last); ok {
		t.Fatal("the last entry of the icon list is reachable before scrolling")
	}
	state.Scroll(ui.ScrollEvent{X: state.popups.surface.X + 20, Y: state.popups.surface.Y + 20, DeltaY: 100000})
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, last); !ok {
		t.Fatal("the last entry of the icon list is out of reach after scrolling to the end")
	}
	state.Click(last)
	if state.editor.icon != hostIconDesktop || state.editor.iconOpen {
		t.Fatalf("after choosing the last entry: icon %v, open %v", state.editor.icon, state.editor.iconOpen)
	}
}

// With a secret that can be checked or removed the row has three buttons; the
// code field must keep room to show what it says, so the scan and paste buttons
// go on a row of their own under it.
func TestOTPFieldKeepsItsWidthWhateverButtonsItHas(t *testing.T) {
	for _, checked := range []bool{false, true} {
		state := newTestState(t, testOptions(t))
		clickHostAction(state, hitAddHost)
		state.Click(hitDialogAdvanced)
		state.editor.otpChecked = checked
		canvas, _ := paintedTexts(state)
		state.editor.form.offset = state.editor.form.geometry.MaxOffsetY
		canvas, _ = paintedTexts(state)
		// The field's label shares its identity, so take the input: the taller one.
		var field ui.Hit
		for _, hit := range canvas.Hits() {
			if hit.ID == hitDialogOTP && hit.Bounds.Height > field.Bounds.Height {
				field = hit
			}
		}
		if field.Bounds.Width < 250 {
			t.Fatalf("checked=%v: the code field is %v wide, too narrow for its placeholder", checked, field.Bounds.Width)
		}
		scan, _ := hitByID(canvas, hitDialogOTPScan)
		if scan.Bounds.Y <= field.Bounds.Y+field.Bounds.Height-1 {
			t.Fatalf("checked=%v: scan %+v should sit under the field %+v", checked, scan.Bounds, field.Bounds)
		}
		for _, id := range []ui.ID{hitDialogOTPScan, hitDialogOTPPaste, hitDialogForwardX11, hitDialogNoHistory} {
			hit, found := hitByID(canvas, id)
			if !found || hit.Bounds.Y+hit.Bounds.Height > float32(state.windowRect().Height) {
				t.Fatalf("checked=%v: control %v is missing or off screen: %+v", checked, id, hit)
			}
		}
		x11, _ := hitByID(canvas, hitDialogForwardX11)
		history, _ := hitByID(canvas, hitDialogNoHistory)
		if x11.Bounds.Y+x11.Bounds.Height > history.Bounds.Y {
			t.Fatalf("checked=%v: the two switches overlap: %+v and %+v", checked, x11.Bounds, history.Bounds)
		}
	}
}

// The card view shows a host's tags, as the list does: they are one of the
// things a host is found by.
func TestHostCardsShowTheirTags(t *testing.T) {
	state := newTestState(t, testOptions(t))
	setHostTags(&state.hosts[0], []string{"prod", "web"})
	state.hostView = hostViewGrid
	_, texts := paintedTexts(state)
	found := map[string]bool{}
	for _, text := range texts {
		found[text] = true
	}
	if !found["prod"] || !found["web"] {
		t.Fatalf("the card does not show the host's tags; texts: %v", texts)
	}
}

func TestDesktopHostsGetAMonitorIconByDefault(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemUnknown, true); !reflect.DeepEqual(art, icons.Monitor) {
		t.Fatal("an automatic icon for a desktop host is not the monitor")
	}
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemUnknown, false); !reflect.DeepEqual(art, icons.Server) {
		t.Fatal("an automatic icon for a non-desktop host is not the server")
	}
	if art := state.hostIconArt(hostIconAuto, sshconn.SystemWindows, true); reflect.DeepEqual(art, icons.Monitor) {
		t.Fatal("a known system lost its badge on a desktop host")
	}
	if art := state.hostIconArt(hostIconDesktop, sshconn.SystemUnknown, false); !reflect.DeepEqual(art, icons.Monitor) {
		t.Fatal("the explicit desktop icon is not the monitor")
	}
	icon, ok := parseHostIcon("desktop")
	if !ok || icon != hostIconDesktop || icon.String() != "desktop" {
		t.Fatalf("desktop icon = %v, %v", icon, ok)
	}
}

func TestSelectedGridCardKeepsItsIconBadgeVisible(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if fill := state.hostBadgeFill(state.pal.accentSoft); fill == state.pal.accentSoft {
		t.Fatal("a sequential host icon badge disappears on the selected card")
	}
	if fill := state.hostBadgeFill(state.pal.surface); fill != state.pal.accentSoft {
		t.Fatalf("an unselected card changed its badge fill to %v", fill)
	}
}
