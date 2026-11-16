//go:build darwin || windows || linux

package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

func TestLocalProxyDialogJourneyInBothLanguages(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			world, options := newSharingWorld(t)
			options.PreferredLanguages = func() []string { return []string{language} }
			clipboardFails := false
			var clipboard string
			options.SetClipboardText = func(value string) error {
				if clipboardFails {
					return errors.New("clipboard unavailable")
				}
				clipboard = value
				return nil
			}
			state := newTestState(t, options)
			world.connectFirst(t, state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			session.term.shell.(*fakeShell).say("tester@server:~$ ")
			state.Wake()
			state.openLocalProxy(session)
			var nativeInput app.TextInput
			// CheckLayout uses reference metrics, so layout must use them too.
			view, err := headless.Open(app.Config{
				Title: "Local proxy", Width: 900, Height: 640, Tree: Tree(),
				Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
				TextInput: func(context app.TextInputContext) *app.TextInput {
					if context.Terminal {
						return state.TerminalInput(context)
					}
					if editor := state.EditorFor(context.ID); editor != nil {
						nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
						return &nativeInput
					}
					return nil
				},
			}, nil, headless.Options{Scale: 1, ReferenceText: true})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			check := func(name string) {
				t.Helper()
				if err := view.Wait(300 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if issues := headless.CheckLayout(view.Snapshot()); len(issues) > 0 {
					t.Fatalf("%s layout: %+v", name, issues)
				}
				if dir := os.Getenv("MOTTY_PROXY_SCREENSHOTS"); dir != "" {
					if err := view.Screenshot(filepath.Join(dir, language+"-"+name+".png")); err != nil {
						t.Fatal(err)
					}
				}
			}
			check("idle")
			if err := view.ClickLabel(state.messages.LocalProxy.More); err != nil {
				t.Fatal(err)
			}
			check("details")
			if err := view.ClickLabel(state.messages.LocalProxy.Less); err != nil {
				t.Fatal(err)
			}
			target, _ := state.localProxyTarget(session)
			state.localProxyManager().operations[target] = &localProxyOperation{
				target: target, cancel: func() {}, localPort: 7897, remotePort: 43217,
				phase: localProxyConnected, users: make(map[localProxySubscription]struct{}),
			}
			if err := view.Resize(900, 640); err != nil {
				t.Fatal(err)
			}
			check("connected")
			clipboardFails = true
			if err := view.ClickLabel(state.messages.LocalProxy.EnableCopy); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalLocalProxy {
				t.Fatal("clipboard error dismissed the dialog")
			}
			check("copy-error")
			clipboardFails = false
			if err := view.ClickLabel(state.messages.LocalProxy.EnableCopy); err != nil {
				t.Fatal(err)
			}
			if clipboard != localProxyEnableCommand(43217) || state.modal != modalNone {
				t.Fatal("copy did not return with the configured endpoint")
			}
			if got, want := view.Snapshot().Focused, state.activeTerminalFocusHit(); got != want {
				t.Fatalf("terminal focus = %v, want %v", got, want)
			}
		})
	}
}
