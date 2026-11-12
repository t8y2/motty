package workbench

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"motty/internal/charset"
)

// The encoding is a control of the icon row, next to the file transfer mode,
// not a labelled row of its own; a protocol that opens no shell has no byte
// stream to encode and does not ask for one.
func TestHostEncodingSharesTheIconRow(t *testing.T) {
	cases := []struct {
		protocol hostProtocol
		shown    bool
	}{
		{protocolSSH, true},
		{protocolTelnet, true},
		{protocolSerial, true},
		{protocolVNC, false},
		{protocolRDP, false},
	}
	for _, c := range cases {
		t.Run(protocolName(c.protocol), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.openEditHostID(hostByLabel(t, state, "Production").ID)
			state.editor.setProtocol(c.protocol)
			canvas, _ := paintAt(state, gfx.Size{Width: 900, Height: 900})
			body := &state.editor.form.body
			icon := body.iconField.Rect()
			hit, painted := hitByID(canvas, hitDialogEncoding)
			if !c.shown {
				if body.encoding.Valid() || painted {
					t.Fatalf("encoding %+v painted on a protocol without a shell", hit.Bounds)
				}
				return
			}
			if !painted {
				t.Fatal("the encoding field was not painted")
			}
			if !body.encoding.Valid() {
				t.Fatal("the encoding field has no slot in the form")
			}
			encoding := body.encoding.Rect()
			if encoding.Y != icon.Y || encoding.Height != icon.Height {
				t.Fatalf("encoding %+v is not in the icon row %+v", encoding, icon)
			}
			// The control lies to the right of the icon's, inside the row.
			if hit.Bounds.X < icon.X+icon.Width {
				t.Fatalf("encoding %+v overlaps the icon field %+v", hit.Bounds, icon)
			}
			if hit.Bounds.Y < icon.Y || hit.Bounds.Y+hit.Bounds.Height > icon.Y+icon.Height {
				t.Fatalf("encoding control %+v is outside the icon row %+v", hit.Bounds, icon)
			}
			// And the row it shares is below the tags: the encoding did not replace
			// a row of its own further up.
			if icon.Y <= body.tags.Rect().Y {
				t.Fatalf("icon row %+v is not below the tags %+v", icon, body.tags.Rect())
			}
		})
	}
}

func TestHostEncodingSurvivesTransportRoundTrip(t *testing.T) {
	for _, encoding := range []string{"", "GB18030", "GBK", "Big5", "Shift_JIS", "KOI8-R"} {
		t.Run("encoding="+encoding, func(t *testing.T) {
			original := Host{Label: "legacy", Address: "legacy.example.com", Port: 22, Encoding: encoding}
			original.finish()
			var restored Host
			if !restored.applyTransport(original.transport()) {
				t.Fatal("applyTransport rejected a valid encoding")
			}
			if restored.Encoding != encoding {
				t.Fatalf("encoding = %q, want %q", restored.Encoding, encoding)
			}
		})
	}
}

func TestHostEncodingIsOmittedForUTF8InJSON(t *testing.T) {
	// A host saved before the setting existed keeps its record byte-for-byte:
	// the UTF-8 default must not add a key.
	raw, err := json.Marshal(savedHost{ID: "h1", Label: "web", Address: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "encoding") {
		t.Fatalf("UTF-8 default wrote an encoding key: %s", raw)
	}
	var decoded savedHost
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Encoding != "" {
		t.Fatalf("decoded encoding = %q, want empty", decoded.Encoding)
	}
}

func TestHostEncodingIsSavedAndLoaded(t *testing.T) {
	// Drive the real catalog through decodeHosts, which is what loadState uses.
	entry := savedHost{ID: "h1", Label: "legacy", Address: "legacy.example.com", Port: 22,
		savedTransport: savedTransport{Encoding: "GB18030"}}
	hosts, _ := decodeHosts([]savedHost{entry}, nil, 1)
	if len(hosts) != 1 {
		t.Fatalf("decodeHosts kept %d hosts, want 1", len(hosts))
	}
	if hosts[0].Encoding != "GB18030" {
		t.Fatalf("loaded encoding = %q, want GB18030", hosts[0].Encoding)
	}
	// A damaged encoding is dropped rather than opened with a guess.
	broken := entry
	broken.Encoding = "not-a-charset"
	if hosts, _ := decodeHosts([]savedHost{broken}, nil, 1); len(hosts) != 0 {
		t.Fatal("a host with an unknown encoding was loaded")
	}
}

func TestHostEncodingSurvivesCatalogExportImport(t *testing.T) {
	host := Host{Label: "legacy", Address: "legacy.example.com", Port: 22, Encoding: "Big5"}
	host.finish()
	entry := savedHostFromHost(host)
	if entry.Encoding != "Big5" {
		t.Fatalf("exported encoding = %q, want Big5", entry.Encoding)
	}
	state := newTestState(t, testOptions(t))
	restored, ok := state.hostFromCatalogEntry(entry)
	if !ok {
		t.Fatal("hostFromCatalogEntry rejected an exported host")
	}
	if restored.Encoding != "Big5" {
		t.Fatalf("imported encoding = %q, want Big5", restored.Encoding)
	}
}

func TestHostEditorEncodingBinding(t *testing.T) {
	editor := &hostEditor{}
	// New host: UTF-8 default.
	editor.reset()
	if editor.encoding != "" {
		t.Fatalf("new host encoding = %q, want empty (UTF-8)", editor.encoding)
	}
	host := editor.host(1)
	if host.Encoding != "" {
		t.Fatalf("built host encoding = %q, want empty", host.Encoding)
	}
	// Editing a host with GB18030 pre-fills the form.
	existing := Host{Label: "legacy", Address: "legacy.example.com", Port: 22, Encoding: "GB18030"}
	existing.finish()
	editor.load(existing)
	if editor.encoding != "GB18030" {
		t.Fatalf("loaded form encoding = %q, want GB18030", editor.encoding)
	}
	built := editor.host(existing.ID)
	if built.Encoding != "GB18030" {
		t.Fatalf("rebuilt host encoding = %q, want GB18030", built.Encoding)
	}
	// Reset returns to the UTF-8 default.
	editor.reset()
	if editor.encoding != "" {
		t.Fatalf("reset encoding = %q, want empty", editor.encoding)
	}
}

func TestHostEncodingPickerIsSearchableAndFixed(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.modal = modalNewHost
	p := state.hostEncodingPicker()
	// Every offered charset is a choice, including the UTF-8 default.
	if len(p.control.Choices()) != len(charset.Encodings) {
		t.Fatalf("picker has %d choices, want %d", len(p.control.Choices()), len(charset.Encodings))
	}
	if p.control.AllowCreate {
		t.Fatal("the encoding picker must not create arbitrary charsets")
	}
	// A search alias filters the list, e.g. "gb" for the Chinese encodings.
	p.control.Query.Insert("gb")
	choices := p.control.Choices()
	if len(choices) == 0 || len(choices) >= len(charset.Encodings) {
		t.Fatalf("query %q did not filter: %d choices", "gb", len(choices))
	}
	found := false
	for _, choice := range choices {
		if choice.Value == "GB18030" {
			found = true
		}
	}
	if !found {
		t.Fatal("searching gb did not find GB18030")
	}
}

func TestHostEncodingPickerChooseAndClear(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.modal = modalNewHost
	p := state.hostEncodingPicker()
	// Choose GBK from the unfiltered list.
	choices := p.control.Choices()
	for index, choice := range choices {
		if choice.Value == "GBK" {
			if !p.control.Choose(index) {
				t.Fatal("Choose(GBK) failed")
			}
			break
		}
	}
	state.syncHostEncoding()
	if state.editor.encoding != "GBK" {
		t.Fatalf("chosen encoding = %q, want GBK", state.editor.encoding)
	}
	// Clear returns to the UTF-8 default.
	p.control.Clear()
	state.syncHostEncoding()
	if state.editor.encoding != "" {
		t.Fatalf("cleared encoding = %q, want empty (UTF-8)", state.editor.encoding)
	}
}

func TestHostEncodingSurvivesARealSaveAndReload(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	state := newTestState(t, options)
	host := Host{ID: state.nextHostID, Label: "legacy", Address: "legacy.example.com", Port: 22, Encoding: "GB18030"}
	state.nextHostID++
	host.finish()
	state.hosts = append(state.hosts, host)
	state.save()
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if len(saved.Hosts) != 1 {
		t.Fatalf("reloaded %d hosts, want 1", len(saved.Hosts))
	}
	if saved.Hosts[0].Encoding != "GB18030" {
		t.Fatalf("reloaded encoding = %q, want GB18030", saved.Hosts[0].Encoding)
	}
	restored, _ := decodeHosts(saved.Hosts, nil, 1)
	if len(restored) != 1 || restored[0].Encoding != "GB18030" {
		t.Fatalf("decoded hosts lost the encoding: %+v", restored)
	}
}

func TestSameSavedHostNoticesEncoding(t *testing.T) {
	left := savedHost{ID: "h1", savedTransport: savedTransport{Encoding: "GBK"}}
	right := left
	right.Encoding = "Big5"
	if sameSavedHost(left, right) {
		t.Fatal("sameSavedHost ignored a changed encoding")
	}
	right.Encoding = "GBK"
	if !sameSavedHost(left, right) {
		t.Fatal("sameSavedHost reported equal hosts as different")
	}
}
