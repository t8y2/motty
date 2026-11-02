//go:build darwin || windows || linux

package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/filecontent"
	"motty/internal/i18n"
)

func documentFixture(t *testing.T) *fileDocument {
	t.Helper()
	owner := New(freshOptions(t))
	owner.setLanguage(i18n.Chinese)
	t.Cleanup(owner.Close)
	doc := &fileDocument{
		owner:    owner,
		name:     "配置.yaml",
		location: "/etc/很长的目录/" + strings.Repeat("路径/", 30) + "配置.yaml",
		source:   "docker-ubuntu · 127.0.0.1",
		local:    true,
	}
	decoded, err := filecontent.Decode([]byte("name: 中文\n# configuration\nenabled: true\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	doc.setText(decoded, nil)
	return doc
}

type documentTestHost struct{ windows []*documentTestWindow }

type documentTestWindow struct {
	config app.Config
	closed func()
	done   bool
	shows  int
}

func (w *documentTestWindow) Close() {
	if !w.done {
		w.done = true
		if w.config.OnClosed != nil {
			w.config.OnClosed()
		}
		w.closed()
	}
}

func (w *documentTestWindow) Hide() {}
func (w *documentTestWindow) Show() { w.shows++ }

func (w *documentTestWindow) Frame() (ui.Rect, bool) {
	return ui.Rect{Width: float32(w.config.Width), Height: float32(w.config.Height)}, true
}

func (h *documentTestHost) OpenWindow(config app.Config, _ *ui.Canvas, closed func()) (app.WindowControl, error) {
	w := &documentTestWindow{config: config, closed: closed}
	h.windows = append(h.windows, w)
	return w, nil
}

func (h *documentTestHost) AddStatusItem(app.StatusItemConfig) (app.StatusItemControl, error) {
	return nil, errors.New("not used")
}

func (h *documentTestHost) SetDockBadge(string) {}

func (h *documentTestHost) Quit() {
	for _, w := range h.windows {
		w.Close()
	}
}
