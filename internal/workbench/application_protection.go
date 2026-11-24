package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/devicekey"
	"github.com/vivaui/viva/gfx"
	"motty/internal/configstore"
	"motty/internal/protection"
	"motty/internal/vault"
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

// All windows of one profile share exactly one unlock and one outstanding
// operation. Native authentication and the KDF run off the event loop.
type applicationProtection struct {
	path                   string
	gate                   *protection.Gate
	openErr                error
	recoveryPending        bool
	enabled, systemEnabled bool
	locked                 atomic.Bool
	migrating              atomic.Bool
	busy                   bool
	lockAfterOperation     bool
	cancel                 context.CancelFunc
	retryOpen              bool
	expectVault            bool
	lockPreferences        configstore.LockPreferences
	droppedWindows         map[string][]byte
	supportChecked         bool
	systemMethod           devicekey.VerificationMethod
	results                chan func()
}

func newApplicationProtection(path string, keys vault.Keystore) *applicationProtection {
	if keys == nil {
		keys = defaultKeystore(filepath.Join(filepath.Dir(path), "vault.json"))
	}
	p := &applicationProtection{path: path, results: make(chan func(), 1)}
	opts := protection.Options{
		Directory: filepath.Dir(path), ConfigPath: path, ConfigFormat: configFormat,
		Keystore: keys, DeviceStore: systemKeystore{},
		RevokeDevice: func(profile string, blob []byte) error {
			return revokeDevice(context.Background(), devicekey.Options{Service: keyService("Motty application lock"), Account: profile}, blob)
		},
	}
	p.gate, p.openErr = protection.Open(opts)
	if p.openErr == nil && p.gate.PendingMigration() {
		// A terminated disable operation can leave a plaintext staging copy.
		// Never clean a different process's live migration.
		if lease, err := profileLease(path, true); err == nil {
			// Unlock retries a failed cleanup under its own exclusive lease.
			_ = p.gate.CleanupPreparation()
			lease.Close()
		}
	}
	p.updateStatus()
	if p.openErr == nil && p.gate.HasDeviceCleanup() {
		// An unavailable OS credential store may have outlived the committed
		// disable. Retry on a separate gate so no native call holds the UI's
		// gate mutex while the user opens or locks a window.
		go func() {
			lease, err := profileLease(path, true)
			if err != nil {
				return
			}
			defer lease.Close()
			if cleanup, err := protection.Open(opts); err == nil {
				_ = cleanup.RetryDeviceCleanup()
			}
		}()
	}
	return p
}

func (p *applicationProtection) updateStatus() {
	if p.openErr != nil {
		p.locked.Store(true)
		return
	}
	p.enabled = p.gate.Enabled()
	p.recoveryPending = p.gate.PendingMigration()
	p.systemEnabled = !p.recoveryPending && p.enabled && len(p.gate.DeviceBlob()) > 0
	p.locked.Store(p.gate.Locked() || p.recoveryPending)
}

// PrepareProfile probes protection before the launcher compacts window records.
// As for window creation itself, callers run this on the UI thread.
func (group *Group) PrepareProfile(path string, keys vault.Keystore) bool {
	path = filepath.Clean(path)
	if group.protection == nil || group.protection.path != path {
		group.protection = newApplicationProtection(path, keys)
	}
	return group.protection.locked.Load() || group.protection.migrating.Load()
}

func (s *State) attachProtection() {
	path, keys := s.options.StatePath, s.options.Keystore
	if g := s.options.Group; g != nil {
		g.PrepareProfile(path, keys)
		s.appProtection = g.protection
		s.options.StatePath = g.protection.path
	} else {
		s.appProtection = newApplicationProtection(path, keys)
	}
	open := s.options.OpenContentWindow
	if open != nil {
		s.options.OpenContentWindow = func(config app.Config, canvas *gfx.Canvas) (*app.ContentWindow, error) {
			if s.appAccessBlocked() {
				return nil, errAppLocked
			}
			w, err := open(config, canvas)
			if w != nil && err == nil {
				s.protectedChildren = slices.DeleteFunc(s.protectedChildren, (*app.ContentWindow).Closed)
				s.protectedChildren = append(s.protectedChildren, w)
			}
			return w, err
		}
	}
}

func (s *State) appLockEnabled() bool { return s.appProtection != nil && s.appProtection.enabled }
func (s *State) appLocked() bool      { return s.appProtection != nil && s.appProtection.locked.Load() }
func (s *State) appMigrating() bool {
	return s.appProtection != nil && s.appProtection.migrating.Load()
}
func (s *State) appAccessBlocked() bool { return s.appLocked() || s.appMigrating() }
func (s *State) appLockCold() bool      { return s.profileDeferred }

// ProfileLocked gates native text input before it resolves the focused control.
func (s *State) ProfileLocked() bool { return s.appAccessBlocked() }
func (s *State) appSystemEnabled() bool {
	return s.appProtection != nil && s.appProtection.systemEnabled
}
func (s *State) appSystemMethod() devicekey.VerificationMethod {
	p := s.appProtection
	if p == nil || !p.enabled {
		return devicekey.VerificationUnavailable
	}
	if !p.supportChecked {
		p.systemMethod = devicekey.Method()
		p.supportChecked = true
	}
	return p.systemMethod
}

func (s *State) appSystemAvailable() bool {
	return s.appSystemMethod() != devicekey.VerificationUnavailable
}

func (s *State) profileWindows() []*State {
	if s.options.Group == nil {
		if s.closed {
			return nil
		}
		return []*State{s}
	}
	return slices.DeleteFunc(s.options.Group.windows(), func(w *State) bool { return w.closed || w.appProtection != s.appProtection })
}

func (s *State) bindVaultClock() {
	config := s.config
	if config == nil || s.secrets.vault == nil {
		return
	}
	s.secrets.vault.SetStamper(func() (string, error) {
		stamp, err := config.NextStamp()
		if err == nil {
			s.requestSync()
		}
		return string(stamp), err
	})
}

func (s *State) lockApplication() {
	if !s.appLockEnabled() || s.appAccessBlocked() {
		return
	}
	windows := s.profileWindows()
	for _, w := range windows {
		w.flushNoteDraft()
		w.save()
	}
	s.sealProfile()
}

// Stop readers immediately, including when a native prompt or a KDF will
// deliver its result later. Migration already holds all data files exclusively.
func (s *State) sealProfile() {
	p := s.appProtection
	p.locked.Store(true)
	p.supportChecked = false
	if p.busy {
		p.lockAfterOperation = true
		if p.cancel != nil {
			p.cancel()
		}
	}
	if !p.migrating.Load() && p.gate != nil {
		p.gate.Lock()
	}
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

func (s *State) prepareMigration() error {
	windows := s.profileWindows()
	for _, w := range windows {
		if w.config == nil || w.saveBlocked {
			return errors.New(w.noteText("The current configuration cannot be migrated.", "当前配置无法迁移。"))
		}
		if !w.flushNoteDraft() {
			return errors.New(w.noteText("Finish editing the note before migrating.", "请完成笔记编辑后再迁移。"))
		}
		if err := w.saveWithResultMode(false); err != nil {
			return err
		}
	}
	p := s.appProtection
	_, vaultErr := os.Stat(filepath.Join(filepath.Dir(p.path), "vault.json"))
	if vaultErr != nil && !errors.Is(vaultErr, os.ErrNotExist) {
		return vaultErr
	}
	p.expectVault = vaultErr == nil || p.enabled
	p.lockPreferences = configstore.LockPreferences{
		Language: s.savedLanguage(), Theme: s.appearanceMode.saved(),
		Corners: s.cornerStyle.saved(), IconColor: s.iconColor.saved(),
		InterfaceTextSize: int(s.interfaceTextSize), InterfaceFont: s.uiFont.family,
		InterfaceFontMain: s.uiFont.main, InterfaceFontBold: s.uiFont.bold,
	}
	p.migrating.Store(true)
	var closeErr error
	for _, w := range windows {
		w.stopSyncTimer()
		w.cancelProfileSync()
		if w.ai.run != nil {
			w.ai.run.cancel()
		}
		for _, child := range w.protectedChildren {
			child.SetConcealed(true)
		}
		closeErr = errors.Join(closeErr, w.config.Close())
		w.config = nil
		w.secrets.vault = nil
		w.syncShortcutCapture()
		w.syncMenuBar()
		w.invalidateWindow()
	}
	return closeErr
}

// OpenShared normally creates a missing profile on first launch. A window
// rebinding to an existing generation must never create an empty replacement.
func (s *State) checkProfileFiles() error {
	p := s.appProtection
	if p.enabled {
		return p.gate.ValidateProtectedFiles()
	}
	info, err := os.Stat(p.path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 100 { // SQLite database header
		return configstore.ErrCorrupt
	}
	if p.expectVault {
		_, err = os.Stat(filepath.Join(filepath.Dir(p.path), "vault.json"))
	}
	return err
}

// Rebind existing windows to the new files without restoring or redialing their
// running sessions. A new window opened during the operation starts deferred.
func (s *State) resumeProfile() error {
	var result error
	windows := s.profileWindows()
	if len(windows) > 0 {
		if err := s.checkProfileFiles(); err != nil {
			return err
		}
	}
	for _, w := range windows {
		if w.profileDeferred && w.options.ProfileUnlocked != nil {
			if err := w.options.ProfileUnlocked(); err != nil {
				return err
			}
			w.options.ProfileUnlocked = nil
		}
	}
	for _, w := range windows {
		switch {
		case w.profileDeferred:
			if err := w.loadApplicationProfile(true); err != nil {
				result = errors.Join(result, err)
				continue
			}
			w.profileDeferred = false
		case w.config == nil:
			db, _, _, report, err := openConfigWithRecovery(w.options.StatePath, w.windowID, false)
			if err == nil && (db == nil || report.outcome == loadTooNew) {
				err = errors.New("cannot reopen the profile after migration")
			}
			if err != nil {
				result = errors.Join(result, err)
				continue
			}
			if err := w.initSecrets(false); err != nil {
				db.Close()
				result = errors.Join(result, err)
				continue
			}
			// The model still corresponds to its own pre-migration baseline.
			// Adopting a peer's newer snapshot here would turn its edits into
			// apparent changes made by this window at its next save.
			w.config = db
			w.bindVaultClock()
		}
		w.syncPending = w.sync.configured() && !w.sync.manual
		w.invalidateWindow()
	}
	if result == nil {
		for _, w := range windows {
			for _, child := range w.protectedChildren {
				child.SetConcealed(false)
			}
		}
	}
	return result
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
	if p == nil || p.busy {
		done(errors.New(s.noteText("Another security operation is running.", "另一项安全操作正在进行。")))
		return
	}
	if p.openErr != nil {
		done(p.openErr)
		return
	}
	unlock := action == appLockUnlock || action == appLockSystemUnlock
	if unlock != s.appLocked() {
		done(errAppLocked)
		return
	}
	migrate := action == appLockEnable || action == appLockDisable
	var lease *os.File
	var preparationErr error
	recovery := unlock && p.recoveryPending
	if recovery && action == appLockSystemUnlock {
		done(protection.ErrMigrationPending)
		return
	}
	if migrate || recovery {
		var err error
		lease, err = profileLease(s.options.StatePath, true)
		if err != nil {
			done(err)
			return
		}
		if migrate {
			preparationErr = s.prepareMigration()
			if preparationErr != nil && !p.migrating.Load() {
				lease.Close()
				done(preparationErr)
				return
			}
		}
	}
	p.busy = true
	if recovery {
		p.migrating.Store(true)
	}
	for _, w := range s.profileWindows() {
		w.invalidateWindow()
	}
	opts := devicekey.Options{Service: keyService("Motty application lock"), Account: p.gate.ProfileID(), Reason: s.noteText("Unlock Motty", "解锁 Motty")}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelLockAction = cancel
	p.cancel = cancel
	post, invalidate := s.post, s.options.Invalidate
	go func() {
		err := preparationErr
		if err == nil {
			err = performAppLockAction(ctx, p.gate, action, current, next, opts)
		}
		cancel()
		finish := func() {
			s.cancelLockAction = nil
			p.busy = false
			p.cancel = nil
			// External readers must stay blocked until every window has a
			// validated store and all deferred windows have loaded.
			p.migrating.Store(true)
			if p.lockAfterOperation || (unlock && s.closed) {
				p.gate.Lock()
				p.lockAfterOperation = false
				if unlock && p.gate.Enabled() {
					err = errors.Join(context.Canceled, err)
				}
			}
			p.updateStatus()
			ordinaryRecovery := !p.enabled && !p.recoveryPending &&
				(errors.Is(err, protection.ErrMigrationRolledBack) || errors.Is(err, protection.ErrDeviceCleanup))
			if unlock && err != nil && !ordinaryRecovery && !p.locked.Load() {
				p.gate.Lock()
				s.sealProfile()
			} else if !p.locked.Load() && (migrate || unlock) {
				p.retryOpen = false
				if recovery && !p.enabled && (err == nil || errors.Is(err, protection.ErrDeviceCleanup)) {
					p.expectVault = true // a recovered disable wrote an ordinary vault
				}
				if resumeErr := s.resumeProfile(); resumeErr != nil {
					err = errors.Join(err, resumeErr)
					p.retryOpen = !p.enabled
					p.gate.Lock()
					s.sealProfile()
				} else {
					for _, w := range s.profileWindows() {
						if w != s {
							if w.modal == modalAppLock {
								w.closeModal()
							}
							w.resetAppLockUI()
						}
					}
				}
			}
			if !p.locked.Load() && len(p.droppedWindows) != 0 {
				s.flushDroppedWindowRecords()
			}
			if lease != nil {
				lease.Close()
			}
			p.migrating.Store(false)
			if !s.closed {
				done(err)
			}
			if len(s.profileWindows()) == 0 && p.enabled && !p.locked.Load() {
				p.gate.Lock()
				p.locked.Store(true)
			}
			for _, w := range s.profileWindows() {
				if w.closed {
					continue // a preceding afterProfileUnlock callback closed this window
				}
				if w != s && !p.retryOpen && (errors.Is(err, protection.ErrMigrationRolledBack) || errors.Is(err, protection.ErrDeviceCleanup)) {
					message := w.appLockErrorText(err)
					if p.locked.Load() {
						w.lockUI.error = message
					} else {
						w.lockUI.notice, w.status = message, message
					}
				}
				w.syncShortcutCapture()
				w.syncMenuBar()
				w.invalidateWindow()
				if next := w.afterProfileUnlock; next != nil && !w.appAccessBlocked() {
					w.afterProfileUnlock = nil
					next()
				}
			}
		}
		if post != nil {
			post(finish)
		} else {
			p.results <- finish
			if invalidate != nil {
				invalidate()
			}
		}
	}()
}

func performAppLockAction(ctx context.Context, g *protection.Gate, action appLockAction, current, next string, opts devicekey.Options) error {
	switch action {
	case appLockEnable:
		return g.Enable(next)
	case appLockChange:
		return g.ChangePassword(current, next)
	case appLockDisable:
		return g.Disable(current)
	case appLockUnlock:
		return g.Unlock(current)
	case appLockEnroll:
		key, err := g.EnrollmentKey(current)
		if err != nil {
			return err
		}
		defer clear(key)
		blob, err := devicekey.Enroll(ctx, opts, key)
		if err != nil {
			return err
		}
		if err = g.SetDeviceBlob(blob); err != nil {
			_ = revokeDevice(context.Background(), opts, blob)
		}
		return err
	case appLockRevoke:
		key, err := g.EnrollmentKey(current)
		if err != nil {
			return err
		}
		clear(key)
		blob, err := g.DeviceForRemoval()
		if err != nil {
			return err
		}
		if len(blob) > 0 {
			if err := revokeDevice(ctx, opts, blob); err != nil {
				return err
			}
		}
		return g.SetDeviceBlob(nil)
	case appLockSystemUnlock:
		blob := g.DeviceBlob()
		if len(blob) == 0 {
			return devicekey.ErrNotFound
		}
		key, err := devicekey.Unseal(ctx, opts, blob)
		if err != nil {
			return err
		}
		defer clear(key)
		if err := ctx.Err(); err != nil {
			return err
		}
		return g.UnlockWithKey(key)
	}
	return errors.New("unknown application lock operation")
}

func revokeDevice(ctx context.Context, opts devicekey.Options, blob []byte) error {
	err := devicekey.Revoke(ctx, opts, blob)
	if errors.Is(err, devicekey.ErrNotFound) {
		return nil
	}
	return err
}
