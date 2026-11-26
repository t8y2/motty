package workbench

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"motty/internal/workbench/icons"

	"github.com/vivaui/viva/app/systemauth"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var appLockHits = tree.Feature("app-lock")

var (
	hitAppLock             = tree.ID(appLockHits, "lock")
	hitAppLockPassword     = tree.ID(appLockHits, "password")
	hitAppLockSubmit       = tree.ID(appLockHits, "submit")
	hitAppLockSystem       = tree.ID(appLockHits, "system")
	hitAppLockEnable       = tree.ID(appLockHits, "enable")
	hitAppLockToggle       = tree.ID(appLockHits, "protection-toggle")
	hitAppLockChange       = tree.ID(appLockHits, "change")
	hitAppLockDisable      = tree.ID(appLockHits, "disable")
	hitAppLockSystemToggle = tree.ID(appLockHits, "system-toggle")
	hitAppLockFormClose    = tree.ID(appLockHits, "form-close")
	hitAppLockFormDismiss  = tree.ID(appLockHits, "form-dismiss")
	hitAppLockFormSubmit   = tree.ID(appLockHits, "form-submit")
	hitAppLockFormFields   = tree.Sub(appLockHits, tree.Name("form-fields"))
)

type appLockUI struct {
	systemFailed               bool
	systemError                string
	password                   text.Editor
	field                      ui.Field
	bound, shown, active       bool
	error                      string
	errorSelection             ui.TextSelectionState // what the user selected of error, to copy it
	logo                       ui.Box
	formAction                 appLockAction
	formFields                 [3]appLockFormField
	formCount                  int
	formTitle, formDescription string
}

type appLockFormField struct {
	editor       text.Editor
	field        ui.Field
	label        string
	bound, shown bool
}

func (state *State) appLockSystemName() string {
	switch state.appSystemMethod() {
	case systemauth.VerificationWindowsHello:
		return "Windows Hello"
	case systemauth.VerificationTouchID:
		return "Touch ID"
	case systemauth.VerificationDevicePassword:
		return state.messages.AppLock.DevicePassword
	default:
		return state.messages.AppLock.SystemTitle
	}
}

func (state *State) appLockBusy() bool {
	return state.appProtection != nil && state.appProtection.busy
}

func appLockOwns(id gfx.ID) bool {
	if id.IsZero() {
		return false
	}
	return id.Scope() == appLockHits || tree.Parent(id.Scope()) == appLockHits ||
		id.Scope() == hitAppLockFormFields || tree.Parent(id.Scope()) == hitAppLockFormFields
}

func (state *State) resetAppLockUI() {
	state.lockUI = appLockUI{}
}

func (state *State) declareWorkbenchPage(c ui.C) {
	if state.appLocked() {
		state.declareAppLockPage(c)
		return
	}
	state.declareShell(c)
}

// paintAppLockPage paints a full, opaque page. No host, session, tooltip or
// business control is declared behind it, including a fullscreen desktop pane.
func (state *State) paintAppLockPage(canvas *gfx.Canvas, size gfx.Size) {
	state.viewport = size
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { state.clickAppLock(id) })
	}
	state.pixelScale = canvas.PixelScale()
	state.titlebarLeading, state.titlebarTrailing = canvas.TitlebarInsets()
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, state.pal.workspace)
	canvas.Fill(gfx.Rect{Width: size.Width, Height: windowBarHeight}, state.pal.shell)
	canvas.MoveRegion(gfx.Rect{
		X:      state.titlebarLeading,
		Width:  max(size.Width-state.titlebarLeading-state.titlebarTrailing, 0),
		Height: windowBarHeight,
	})
	state.windowPage().Draw(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, state.declareWorkbenchPage)
	if state.lockUI.logo.Rect().Width > 0 {
		state.paintLockup(canvas, state.lockUI.logo.Rect())
	}
	footer := ""
	for _, member := range state.profileWindows() {
		if len(member.sessions) > 0 {
			footer = state.messages.AppLock.KeepRunning
			break
		}
	}
	if state.appLockCold() {
		footer = state.messages.AppLock.RestoreAfterUnlock
	}
	if footer != "" {
		canvas.LabelCenter(footer, gfx.Rect{X: 20, Y: max(size.Height-62, windowBarHeight), Width: max(size.Width-40, 0), Height: 24}, gfx.TextStyle{Size: metrics.FontSizeS + 1, Color: state.pal.muted})
	}
}

func (state *State) declareAppLockPage(c ui.C) {
	u, m, pal := &state.lockUI, state.messages.AppLock, state.pal
	if !u.bound {
		u.field.Bind(&u.password)
		u.bound = true
	}
	u.field.Secure, u.field.Invalid = !u.shown, u.error != ""
	busy := state.appLockBusy()
	system := state.appSystemEnabled() && !u.systemFailed && state.appSystemAvailable()
	scale := min(metrics.FontSizeM/14, max(float32(.72), (c.Bounds().Height-windowBarHeight-96)/500))
	width := min(float32(400)*metrics.FontSizeM/14, max(c.Bounds().Width-48, 0))
	height := float32(468) * scale
	if !system {
		height -= 112 * scale
	}
	top := max(windowBarHeight+30, (c.Bounds().Height-height)/2-8)
	ui.Column(c).FillWidth().Items(ui.Center).Children(func() {
		ui.Column(c).Width(width).MarginTop(top).Children(func() {
			u.logo = ui.Slot(c).Size(210*scale, 55*scale).Self(ui.Center)
			ui.Text(c, m.Title).FillWidth().TextCenter().FontSize(30 * scale).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(40 * scale).MarginTop(34 * scale)
			if system {
				label := fmt.Sprintf(m.UnlockSystem, state.appLockSystemName())
				if busy {
					label = m.Working
				}
				icon := icons.Fingerprint
				if state.appSystemMethod() == systemauth.VerificationDevicePassword {
					icon = lucideIcons[iconKey]
				}
				ui.Button(c, label).Icon(icon).Gap(12).IconTransform(gfx.IconTransform{Scale: 1.5}).Height(max(40, 48*scale)).FillWidth().MarginTop(36*scale).Disabled(busy).Hit(hitAppLockSystem, label)
				ui.Row(c).FillWidth().Height(20*scale).Items(ui.Center).Gap(14).MarginSides(26*scale, 0, 24*scale, 0).Children(func() {
					ui.Row(c).Height(1).Grow(1).Background(pal.line)
					ui.Text(c, m.Divider).FontSize(metrics.FontSizeS + 1).Color(pal.muted)
					ui.Row(c).Height(1).Grow(1).Background(pal.line)
				})
			} else {
				ui.Row(c).Height(36 * scale)
			}
			ui.Text(c, m.Password).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(20)
			field := ui.TextField(c, &u.field, m.Password, m.PasswordPlaceholder, ui.WithID(hitAppLockPassword)).FillWidth().Height(max(34, 44*scale)).MarginTop(8 * scale).Disabled(busy).OnKey(state.appLockKey)
			icon, label := lucideIcons[iconEye], state.messages.ShowSecret
			if u.shown {
				icon, label = lucideIcons[iconEyeOff], state.messages.HideSecret
			}
			field.Action(icon, label)
			if field.ActionClicked() && !busy {
				u.shown = !u.shown
			}
			button := ui.Button(c, m.UnlockPassword).Height(max(34, 44*scale)).FillWidth().MarginTop(16*scale).Disabled(busy).Hit(hitAppLockSubmit, m.UnlockPassword)
			if system {
				button.Outline()
			}
			if u.error != "" {
				ui.TextSelectionArea(c, &u.errorSelection).Children(func() {
					ui.Paragraph(c, u.error, 0).FontSize(metrics.FontSizeS).Color(pal.danger).LineHeight(18).MarginTop(12)
				})
			}
		})
	})
	if !u.active && !busy {
		u.active = true
		c.Canvas().RequestFocus(hitAppLockPassword)
	}
}

func (state *State) appLockKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Repeat {
		return gfx.EventResult{Handled: true}
	}
	if event.Key == gfx.KeyEnter && !state.appLockBusy() {
		if _, composing := state.lockUI.password.Composition(); !composing {
			state.clickAppLock(hitAppLockSubmit)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	// Let the page's field retain navigation and normal editor keys. The outer
	// window route consumes anything it leaves, so no workspace shortcut runs.
	return gfx.EventResult{}
}

// Windows offers printable keys to OnShortcut before translating them into
// text. A key handled here never becomes text, so a lock field must be left
// for the native editor even when the reported target is stale or zero. This
// handler is reached only while the lock page or its dialog owns the screen;
// returning unhandled cannot dispatch a workspace shortcut behind it.
func (state *State) appLockInputShortcut(event gfx.KeyEvent) gfx.EventResult {
	// Windows offers Alt+Space here before opening its window menu.
	if runtime.GOOS == "windows" && event.Text == " " && event.Modifiers == gfx.ModifierAlt {
		return gfx.EventResult{}
	}
	text := event.Text != "" || event.Key == gfx.KeySpace
	if text && event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta|gfx.ModifierAlt) == 0 {
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true}
}

func (state *State) appLockEditor(hit gfx.ID) *text.Editor {
	if state.appLocked() {
		if hit == hitAppLockPassword && !state.appLockBusy() {
			return &state.lockUI.password
		}
		return nil
	}
	if state.modal == modalAppLock && hit.Scope() == hitAppLockFormFields {
		index := int(item(hit))
		if index >= 0 && index < state.lockUI.formCount && !state.appLockBusy() {
			return &state.lockUI.formFields[index].editor
		}
	}
	return nil
}

func (state *State) appLockFormKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Repeat || state.appLockBusy() {
		return gfx.EventResult{Handled: true}
	}
	switch event.Key {
	case gfx.KeyEscape:
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.KeyEnter:
		if editor := state.appLockEditor(event.Target); editor != nil {
			if _, composing := editor.Composition(); !composing {
				state.submitAppLockForm()
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}

func (state *State) clickAppLock(id gfx.ID) bool {
	u, m := &state.lockUI, state.messages.AppLock
	if state.appLockBusy() {
		return appLockOwns(id)
	}
	if state.appLocked() {
		switch id {
		case hitAppLockSubmit:
			password := u.password.String()
			if password == "" {
				u.error = m.Required
				return true
			}
			u.password = text.Editor{}
			state.startAppLockUIAction(appLockUnlock, password, "")
		case hitAppLockSystem:
			if state.appProtection.record.SystemID != "" {
				state.startAppLockUIAction(appLockSystemUnlock, "", "")
			}
		}
		return true
	}
	switch id {
	case hitAppLock:
		if state.appLockEnabled() {
			state.lockApplication()
		} else {
			state.openAppLockForm(appLockEnable)
		}
	case hitAppLockEnable:
		state.openAppLockForm(appLockEnable)
	case hitAppLockToggle:
		if state.appLockEnabled() {
			state.openAppLockForm(appLockDisable)
		} else {
			state.openAppLockForm(appLockEnable)
		}
	case hitAppLockChange:
		if state.appLockEnabled() {
			state.openAppLockForm(appLockChange)
		}
	case hitAppLockDisable:
		state.openAppLockForm(appLockDisable)
	case hitAppLockSystemToggle:
		if state.appLockEnabled() {
			if state.appProtection.record.SystemID != "" {
				state.startAppLockUIAction(appLockRevoke, "", "")
			} else if state.appSystemAvailable() {
				state.openAppLockForm(appLockEnroll)
			}
		}
	case hitAppLockFormClose, hitAppLockFormDismiss:
		if state.modal == modalAppLock {
			state.closeModal()
		}
	case hitAppLockFormSubmit:
		state.submitAppLockForm()
	default:
		return false
	}
	return true
}

func (state *State) startAppLockUIAction(action appLockAction, current, next string) {
	u := &state.lockUI
	u.error, u.systemError = "", ""
	state.runAppLockAction(action, current, next, func(err error) {
		if err != nil && action == appLockRevoke {
			u.systemError = state.appLockErrorText(err)
		} else if err != nil {
			u.error = state.appLockErrorText(err)
			if action == appLockSystemUnlock && errors.Is(err, systemauth.ErrUnavailable) {
				u.systemFailed = true
			}
			u.active = false
		} else {
			u.password = text.Editor{}
			u.shown, u.error = false, ""
			if state.modal == modalAppLock {
				state.closeModal()
			}
		}
		state.invalidateWindow()
	})
	state.invalidateWindow()
}

func (state *State) appLockErrorText(err error) string {
	m := state.messages.AppLock
	switch {
	case errors.Is(err, errLockWrongPassword):
		return m.WrongPassword
	case errors.Is(err, errLockCorrupt):
		return m.Corrupt
	case errors.Is(err, errLockPasswordTooShort):
		return m.Minimum
	case errors.Is(err, errLockPasswordTooLong):
		return m.PasswordTooLong
	case errors.Is(err, systemauth.ErrCanceled), errors.Is(err, context.Canceled):
		return ""
	case errors.Is(err, systemauth.ErrForegroundRequired):
		return m.SystemForeground
	case errors.Is(err, systemauth.ErrUnavailable):
		return m.SystemUnavailable
	default:
		return m.Error
	}
}

func (state *State) openAppLockForm(action appLockAction) {
	u, m := &state.lockUI, state.messages.AppLock
	u.formFields, u.formCount, u.error, u.formAction = [3]appLockFormField{}, 1, "", action
	u.formFields[0].label = m.CurrentPassword
	switch action {
	case appLockEnable:
		u.formTitle, u.formDescription, u.formCount = m.EnableTitle, m.EnableDescription, 2
		u.formFields[0].label, u.formFields[1].label = m.NewPassword, m.ConfirmPassword
	case appLockChange:
		u.formTitle, u.formDescription, u.formCount = m.ChangeTitle, m.ChangeDescription, 3
		u.formFields[1].label, u.formFields[2].label = m.NewPassword, m.ConfirmPassword
	case appLockDisable:
		u.formTitle, u.formDescription = m.DisableTitle, m.DisableDescription
	case appLockEnroll:
		u.formTitle, u.formDescription = m.EnrollTitle, m.EnrollDescription
	}
	state.modal = modalAppLock
}

func (state *State) submitAppLockForm() {
	u, m := &state.lockUI, state.messages.AppLock
	if state.modal != modalAppLock || state.appLockBusy() {
		return
	}
	current, next := u.formFields[0].editor.String(), ""
	if u.formAction == appLockEnable || u.formAction == appLockChange {
		index := 0
		if u.formAction == appLockChange {
			index = 1
		}
		next = u.formFields[index].editor.String()
		if err := validateNewLockPassword(next); err != nil {
			u.error = state.appLockErrorText(err)
			return
		}
		if next != u.formFields[index+1].editor.String() {
			u.error = m.Mismatch
			return
		}
		if u.formAction == appLockEnable {
			current = ""
		}
	}
	if current == "" && u.formAction != appLockEnable {
		u.error = m.Required
		return
	}
	state.startAppLockUIAction(u.formAction, current, next)
}

func (state *State) declareAppLockDialog(c ui.C) {
	u, m := &state.lockUI, state.messages.AppLock
	busy := state.appLockBusy()
	height := dialogContentHeight(c.Canvas(), *c.Theme(), dialogViewport(c), 520, u.formDescription, float32(u.formCount)*72+withError(0, u.error))
	dialog := ui.Dialog(c, ui.DialogSpec{Title: u.formTitle, Description: u.formDescription, Width: 520, Height: height, Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAppLockFormDismiss, CloseID: hitAppLockFormClose, InitialFocusID: hitAppLockFormFields.At(0)})
	dialog.Body(func() {
		ui.Column(c).Fill().PadX(4).Children(func() {
			for index := 0; index < u.formCount; index++ {
				field := &u.formFields[index]
				if !field.bound {
					field.field.Bind(&field.editor)
					field.bound = true
				}
				field.field.Secure, field.field.Invalid = !field.shown, u.error != ""
				ui.Column(c).Height(72).Children(func() {
					ui.Text(c, field.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text)
					box := ui.TextField(c, &field.field, field.label, "", ui.WithID(hitAppLockFormFields.At(index))).Height(metrics.ControlHeight).MarginTop(7).Disabled(busy)
					icon, label := lucideIcons[iconEye], state.messages.ShowSecret
					if field.shown {
						icon, label = lucideIcons[iconEyeOff], state.messages.HideSecret
					}
					box.Action(icon, label)
					if box.ActionClicked() && !busy {
						field.shown = !field.shown
					}
				})
			}
			state.problemLineC(c, u.error)
		})
	})
	dialog.Footer(func() {
		label := m.Save
		if busy {
			label = m.Working
		}
		state.dialogActionFooter(c, components.ButtonAction{ID: hitAppLockFormClose, Label: m.Cancel, Variant: components.ButtonSecondary, Disabled: busy},
			components.ButtonAction{ID: hitAppLockFormSubmit, Label: label, Disabled: busy})
	})
}

func (state *State) paintAppLockSettings(page *settingsPage) {
	m := state.messages.AppLock
	enabled, busy := state.appLockEnabled(), state.appLockBusy()
	page.group(m.SettingsTitle, func() {
		description := m.DisabledDescription
		if enabled {
			description = m.SettingsDescription
		}
		page.lineFor(hitAppLockToggle, m.Protection, description, aiSwitchWidth, func() {
			page.switchSlot(hitAppLockToggle, enabled, busy, m.Protection)
		})
		page.actionRow(m.ChangePassword, "", busy || !enabled, components.ButtonAction{ID: hitAppLockChange, Label: m.ChangeAction, Variant: components.ButtonOutline})
		description = m.SystemDisabled
		if state.appProtection.record.SystemID != "" {
			description = m.SystemDescription
		}
		if !state.appLockEnabled() {
			description = m.SystemBeforePassword
		} else if !state.appSystemAvailable() {
			description = m.SystemUnavailable
		}
		registered := state.appProtection.record.SystemID != ""
		page.lineFor(hitAppLockSystemToggle, m.SystemTitle, description, aiSwitchWidth, func() {
			page.switchSlot(hitAppLockSystemToggle, registered, busy || !enabled || !registered && !state.appSystemAvailable(), m.SystemTitle)
		})
		if message := state.lockUI.systemError; message != "" {
			page.growRow().PadX(18).Children(func() {
				state.problemLineC(page.c, message)
			})
		}
	})
}
