//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestHostColorPickerStagesNativeHexAndRestoresFocus(t *testing.T) {
	for _, scale := range []float32{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setLanguage(i18n.Chinese)
			state.openEditHostID(state.hosts[0].ID)
			state.editor.color = *text.NewEditor("#123456")
			var native app.TextInput
			view, err := headless.Open(app.Config{Width: 900, Height: 600, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
				TextInput: func(c app.TextInputContext) *app.TextInput {
					e := state.EditorFor(c.ID)
					if e == nil {
						return nil
					}
					native = app.TextInput{Editor: e}
					return &native
				}}, nil, headless.Options{Scale: float64(scale)})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			click := func(id ui.ID) {
				t.Helper()
				for _, h := range view.Snapshot().Hits {
					if h.ID == id && !h.Block {
						must(view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
						return
					}
				}
				t.Fatalf("missing %v", id)
			}
			must(view.Rest())
			click(hostPickerIDs.Trigger)
			must(view.Rest())
			if !state.editor.colorPicker.open || view.Snapshot().Focused != hostPickerIDs.Saturation {
				t.Fatal("picker did not take focus")
			}
			click(hostPickerIDs.Saturation)
			plane := state.editor.colorPicker.layout.Saturation
			must(view.Driver().Press(plane.X+plane.Width/2, plane.Y+plane.Height/2, ui.PointerButtonPrimary))
			must(view.Driver().Move(plane.X+plane.Width+15, plane.Y-15))
			must(view.Driver().Release(plane.X+plane.Width+15, plane.Y-15, ui.PointerButtonPrimary))
			if state.editor.colorPicker.draft.Saturation != 1 || state.editor.colorPicker.draft.Brightness != 1 {
				t.Fatal("captured selection drag lost target")
			}
			if state.editor.value(fieldColor) != "#123456" {
				t.Fatal("draft escaped popup")
			}
			must(view.Keys("Escape"))
			must(view.Rest())
			if state.modal != modalNewHost || state.editor.colorPicker.open || state.editor.value(fieldColor) != "#123456" || view.Snapshot().Focused != hostPickerIDs.Trigger {
				t.Fatal("escape canceled host or lost original/focus")
			}
			click(hostPickerIDs.Trigger)
			must(view.Rest())
			click(hostPickerIDs.Hex)
			must(view.Keys("Cmd+A"))
			must(view.Type("#abC"))
			must(view.Keys("Enter"))
			must(view.Rest())
			if state.editor.value(fieldColor) != "#AABBCC" || state.modal != modalNewHost || state.editor.colorPicker.open {
				t.Fatal("native hex entry did not apply only to form", state.editor.value(fieldColor))
			}
			click(hostPickerIDs.Trigger)
			must(view.Rest())
			click(hostPickerIDs.Hex)
			must(view.Keys("Cmd+A"))
			must(view.Type("#xyz"))
			must(view.Keys("Enter"))
			if !state.editor.colorPicker.open || !state.editor.colorPicker.invalid || state.editor.value(fieldColor) != "#AABBCC" {
				t.Fatal("invalid input applied")
			}
			must(view.Keys("Cmd+A"))
			view.Driver().SetClipboard("#8BD24F")
			must(view.Keys("Cmd+V"))
			must(view.Rest())
			must(view.Keys("Tab"))
			if view.Snapshot().Focused != hostPickerIDs.Clear {
				t.Fatal("Tab escaped picker focus scope")
			}
			if dir := os.Getenv("MOTTY_PICKER_SCREENSHOTS"); dir != "" {
				must(view.Screenshot(filepath.Join(dir, fmt.Sprintf("host-color-picker-%g.png", scale))))
			}
			click(hostPickerIDs.Apply)
			must(view.Rest())
			if state.editor.value(fieldColor) != "#8BD24F" {
				t.Fatal("apply lost color")
			}
			if dir := os.Getenv("MOTTY_PICKER_SCREENSHOTS"); dir != "" {
				must(view.Screenshot(filepath.Join(dir, fmt.Sprintf("host-color-form-%g.png", scale))))
			}
			click(hostPickerIDs.Trigger)
			must(view.Rest())
			click(hostPickerIDs.Saturation)
			must(view.Click(8, 585))
			must(view.Rest())
			if state.modal != modalNewHost || state.editor.colorPicker.open || state.editor.value(fieldColor) != "#8BD24F" {
				t.Fatalf("outside press: modal=%v open=%v color=%q", state.modal, state.editor.colorPicker.open, state.editor.value(fieldColor))
			}
			click(hostPickerIDs.Trigger)
			must(view.Rest())
			click(hostPickerIDs.Clear)
			must(view.Rest())
			if state.editor.value(fieldColor) != "" {
				t.Fatal("clear failed")
			}
			click(hitDialogCancel)
			if state.hosts[0].Color != "" {
				t.Fatal("outer cancel saved staged color")
			}
		})
	}
}

func TestHostFormSpacingKeepsProtocolControlsAboveFooter(t *testing.T) {
	for _, protocol := range []hostProtocol{protocolSSH, protocolTelnet, protocolSerial, protocolVNC, protocolRDP} {
		state := newTestState(t, testOptions(t))
		state.openHostDialog()
		state.editor.protocol = protocol
		c, _ := paintAt(state, ui.Size{Width: 900, Height: 600})
		footer, _ := hitByID(c, hitDialogSubmit)
		for _, h := range c.Hits() {
			if h.Role == ui.RoleTextField && h.Bounds.Y+h.Bounds.Height > footer.Bounds.Y {
				t.Fatalf("protocol %v overlaps footer %+v footer %+v", protocol, h.Bounds, footer.Bounds)
			}
		}
	}
}
