//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestForwardCreationDisabledThroughNativeDriver(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
			t.Run(fmt.Sprintf("%s/%g", language.String(), scale), func(t *testing.T) {
				_, options := newWorld(t)
				state := newTestState(t, options)
				state.hosts = nil
				state.setLanguage(language)
				state.Click(hitPortForwarding)
				view, err := headless.Open(app.Config{Title: "Action feedback", Width: 1100, Height: 760, Tree: Tree(), Paint: state.Paint, OnResize: state.Resize, OnPointer: state.Pointer, OnKey: state.Key}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				buttons := 0
				for _, hit := range view.Snapshot().Hits {
					if hit.ID != hitForwardAdd {
						continue
					}
					buttons++
					if !hit.Disabled {
						t.Fatal("forward creation is enabled without an SSH host")
					}
					if err = view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				if buttons != 2 || state.modal != modalNone || len(state.fileNotices.items) != 0 {
					t.Fatalf("disabled controls reacted to a click: buttons=%d modal=%d notices=%v", buttons, state.modal, state.fileNotices.items)
				}
				if dir := os.Getenv("MOTTY_ACTION_NOTICE_SCREENSHOTS"); dir != "" && scale == 1.25 {
					if err = view.Screenshot(filepath.Join(dir, "forward-disabled-"+language.String()+".png")); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
