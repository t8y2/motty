package workbench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/systemauth"
	"github.com/vivaui/viva/gfx"
)

var errAppLocked = errors.New("Motty is locked")

type appLockAction uint8

const (
	appLockEnable appLockAction = iota
	appLockChange
	appLockDisable
	appLockEnroll
	appLockRevoke
	appLockUnlock
	appLockSystemUnlock
)

const appLockSystemService = "Motty application lock"
const appLockSystemMarker = "enabled"

type appLockSystem struct {
	method func() systemauth.VerificationMethod
	verify func(context.Context, string) error
	load   func(string, string) ([]byte, error)
	store  func(string, string, []byte) error
	remove func(string, string) error
}

var nativeAppLockSystem = appLockSystem{
	method: systemauth.Method, verify: systemauth.Verify,
	load: app.LoadSecret, store: app.StoreSecret, remove: app.DeleteSecret,
}

type applicationProtection struct {
	system             appLockSystem
	path, verifierPath string
	record             lockVerifier
	openErr            error
	locked             atomic.Bool
	busy               bool
	generation         uint64
	method             systemauth.VerificationMethod
	supportChecked     bool
	results            chan func()
}

func newApplicationProtection(config string) *applicationProtection {
	p := &applicationProtection{path: filepath.Clean(config), results: make(chan func(), 1), system: nativeAppLockSystem}
	if config == "" {
		return p
	}
	p.verifierPath = filepath.Join(filepath.Dir(config), "app-lock.json")
	p.record, p.openErr = readLockVerifier(p.verifierPath)
	p.locked.Store(p.openErr != nil || p.record.Version != 0)
	return p
}

func (system appLockSystem) enabled(id string) bool {
	if id == "" {
		return false
	}
	value, err := system.load(keyService(appLockSystemService), id)
	return err == nil && string(value) == appLockSystemMarker
}

func (s *State) attachProtection() {
	path := filepath.Clean(s.options.StatePath)
	if group := s.options.Group; group != nil {
		if group.protection == nil || group.protection.path != path {
			group.protection = newApplicationProtection(s.options.StatePath)
		}
		s.appProtection = group.protection
	} else {
		s.appProtection = newApplicationProtection(s.options.StatePath)
	}
	open := s.options.OpenContentWindow
	if open == nil {
		return
	}
	s.options.OpenContentWindow = func(config app.Config, canvas *gfx.Canvas) (*app.ContentWindow, error) {
		if s.appLocked() {
			return nil, errAppLocked
		}
		forward := func(next func(gfx.KeyEvent) gfx.EventResult) func(gfx.KeyEvent) gfx.EventResult {
			return func(event gfx.KeyEvent) gfx.EventResult {
				if s.appLocked() {
					return gfx.EventResult{Handled: true}
				}
				if result := s.appLockShortcut(event, &config); result.Handled {
					return result
				}
				if next != nil {
					return next(event)
				}
				return gfx.EventResult{}
			}
		}
		config.OnKey, config.OnShortcut = forward(config.OnKey), forward(config.OnShortcut)
		window, err := open(config, canvas)
		if err == nil && window != nil {
			s.protectedChildren = slices.DeleteFunc(s.protectedChildren, (*app.ContentWindow).Closed)
			s.protectedChildren = append(s.protectedChildren, window)
		}
		return window, err
	}
}

func (s *State) appLockEnabled() bool {
	return s.appProtection != nil && s.appProtection.record.Version != 0
}
func (s *State) appLocked() bool        { return s.appProtection != nil && s.appProtection.locked.Load() }
func (s *State) appAccessBlocked() bool { return s.appLocked() }
func (s *State) ProfileLocked() bool    { return s.appLocked() }
func (s *State) appLockCold() bool      { return s.profileDeferred }

func (s *State) appSystemEnabled() bool {
	// Reading the OS credential store can display a prompt or block.
	// Only a deliberate system unlock needs to load its local marker.
	return s.appProtection != nil && s.appProtection.record.SystemID != ""
}
func (s *State) appSystemMethod() systemauth.VerificationMethod {
	p := s.appProtection
	if p == nil || !s.appLockEnabled() {
		return systemauth.VerificationUnavailable
	}
	if !p.supportChecked {
		p.method = p.system.method()
		p.supportChecked = true
	}
	return p.method
}
func (s *State) appSystemAvailable() bool {
	return s.appSystemMethod() != systemauth.VerificationUnavailable
}

func (s *State) profileWindows() []*State {
	if group := s.options.Group; group != nil {
		return slices.DeleteFunc(group.windows(), func(w *State) bool { return w.closed || w.appProtection != s.appProtection })
	}
	if s.closed {
		return nil
	}
	return []*State{s}
}

func (s *State) lockApplication() {
	if !s.appLockEnabled() || s.appLocked() {
		return
	}
	for _, w := range s.profileWindows() {
		w.flushNoteDraft()
		w.save()
	}
	s.sealProfile()
}

func (s *State) sealProfile() {
	p := s.appProtection
	p.locked.Store(p.record.Version != 0 || p.openErr != nil)
	p.generation++
	p.supportChecked = false
	for _, w := range s.profileWindows() {
		if w.modal == modalAppLock {
			w.closeModal()
		}
		w.resetAppLockUI()
		w.pendingFocus = gfx.ID{}
		w.reveal.reset()
		w.secrets.unlockShown = false
		w.stopSyncTimer()
		w.cancelProfileSync()
		if w.ai.run != nil {
			w.ai.run.cancel()
		}
		w.closeTabPreview()
		w.syncShortcutCapture()
		for _, child := range w.protectedChildren {
			child.SetConcealed(true)
		}
		w.syncMenuBar()
		w.invalidateWindow()
	}
}

func (s *State) resumeApplication(windows []*State) {
	for _, w := range windows {
		if w.profileDeferred {
			w.loadApplicationProfile()
			w.profileDeferred = false
		}
		w.syncPending = w.sync.configured() && !w.sync.manual
		for _, child := range w.protectedChildren {
			child.SetConcealed(false)
		}
		w.syncShortcutCapture()
		w.syncMenuBar()
		w.invalidateWindow()
	}
}

func (s *State) drainAppLockResults() {
	if p := s.appProtection; p != nil {
		select {
		case finish := <-p.results:
			finish()
		default:
		}
	}
}

func (s *State) runAppLockAction(action appLockAction, current, next string, done func(error)) {
	p := s.appProtection
	if p == nil || p.busy || p.verifierPath == "" {
		done(errAppLocked)
		return
	}
	if p.openErr != nil {
		done(p.openErr)
		return
	}
	unlock := action == appLockUnlock || action == appLockSystemUnlock
	if unlock != s.appLocked() || (action == appLockEnable) == s.appLockEnabled() {
		done(errAppLocked)
		return
	}
	p.busy = true
	record, path, generation := p.record, p.verifierPath, p.generation
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelLockAction = cancel
	reason := s.messages.AppLock.SystemPrompt
	post := s.post
	system := p.system
	var wake []func()
	for _, window := range s.profileWindows() {
		if window.options.Invalidate != nil {
			wake = append(wake, window.options.Invalidate)
		}
	}
	go func() {
		defer cancel()
		updated, err := system.perform(ctx, record, path, action, current, next, reason)
		finish := func() {
			s.cancelLockAction = nil
			p.busy = false
			p.record = updated
			windows := s.profileWindows()
			if err == nil {
				if unlock && (s.closed || generation != p.generation) {
					err = context.Canceled
				} else if unlock || action == appLockDisable {
					p.locked.Store(false)
					s.resumeApplication(windows)
				} else if action == appLockEnable && (generation != p.generation || len(windows) == 0) {
					s.sealProfile()
				}
			}
			if !s.closed {
				done(err)
			}
			for _, w := range windows {
				w.invalidateWindow()
				if run := w.afterProfileUnlock; run != nil && !w.appLocked() {
					w.afterProfileUnlock = nil
					run()
				}
			}
		}
		if post != nil {
			// Window posts may be discarded during teardown. Wake on any
			// sibling may adopt the same completion from the shared channel.
			p.results <- finish
			post(s.drainAppLockResults)
		} else {
			p.results <- finish
		}
		for _, invalidate := range wake {
			invalidate()
		}
	}()
}

func (system appLockSystem) perform(ctx context.Context, record lockVerifier, path string, action appLockAction, current, next, reason string) (lockVerifier, error) {
	service := keyService(appLockSystemService)
	switch action {
	case appLockChange, appLockDisable, appLockEnroll, appLockUnlock:
		if err := record.verify(current); err != nil {
			return record, err
		}
	}
	if err := ctx.Err(); err != nil {
		return record, err
	}
	switch action {
	case appLockEnable, appLockChange:
		updated, err := newLockVerifier(next, record.SystemID)
		if err != nil {
			return record, err
		}
		if err = ctx.Err(); err != nil {
			return record, err
		}
		if err = updated.save(path); err != nil {
			return record, err
		}
		return updated, nil
	case appLockDisable, appLockRevoke:
		updated := record
		updated.SystemID = ""
		var err error
		if action == appLockDisable {
			err = os.Remove(path)
			updated = lockVerifier{}
		} else {
			err = updated.save(path)
		}
		if err != nil {
			return record, err
		}
		if record.SystemID != "" {
			if err = system.remove(service, record.SystemID); err != nil && !errors.Is(err, app.ErrSecretNotFound) {
				log.Printf("motty: remove retired app-lock setting: %v", err)
			}
		}
		return updated, nil
	case appLockEnroll:
		if err := system.verify(ctx, reason); err != nil {
			return record, err
		}
		if err := ctx.Err(); err != nil {
			return record, err
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return record, err
		}
		updated := record
		updated.SystemID = hex.EncodeToString(id[:])
		if err := system.store(service, updated.SystemID, []byte(appLockSystemMarker)); err != nil {
			return record, err
		}
		if err := updated.save(path); err != nil {
			_ = system.remove(service, updated.SystemID)
			return record, err
		}
		if record.SystemID != "" {
			_ = system.remove(service, record.SystemID)
		}
		return updated, nil
	case appLockSystemUnlock:
		if !system.enabled(record.SystemID) {
			return record, systemauth.ErrUnavailable
		}
		return record, system.verify(ctx, reason)
	case appLockUnlock:
		return record, nil
	}
	return record, errAppLocked
}
