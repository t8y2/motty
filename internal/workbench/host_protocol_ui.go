package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/serial"
	"motty/internal/sshconn"
)

// The protocol part of the host dialog: which way the host is reached, and for
// a serial port the device, the line and the flow control.

// hostProtocolUiHits is the place of the controls this file paints.
var hostProtocolUiHits = tree.Feature("host-protocol-ui")

var (
	hitDialogViaSSH               = tree.ID(hostProtocolUiHits, "dialog-via-ssh")
	hitDialogClipboard            = tree.ID(hostProtocolUiHits, "dialog-clipboard")
	hitDialogConsole              = tree.ID(hostProtocolUiHits, "dialog-console")
	hitDialogRemoteAudio          = tree.ID(hostProtocolUiHits, "dialog-remote-audio")
	hitDialogStandardSecurity     = tree.ID(hostProtocolUiHits, "dialog-standard-security")
	hitDialogStandardSecurityHelp = tree.ID(hostProtocolUiHits, "dialog-standard-security-help")
	hitDialogLegacyTLS            = tree.ID(hostProtocolUiHits, "dialog-legacy-tls")
	hitDialogLegacyTLSHelp        = tree.ID(hostProtocolUiHits, "dialog-legacy-tls-help")
	hitDialogEffectWallpaper      = tree.ID(hostProtocolUiHits, "dialog-effect-wallpaper")
	hitDialogEffectFontSmoothing  = tree.ID(hostProtocolUiHits, "dialog-effect-font-smoothing")
	hitDialogEffectComposition    = tree.ID(hostProtocolUiHits, "dialog-effect-composition")
	hitDialogEffectWindowDrag     = tree.ID(hostProtocolUiHits, "dialog-effect-window-drag")
	hitDialogEffectAnimations     = tree.ID(hostProtocolUiHits, "dialog-effect-animations")
	hitDialogEffectThemes         = tree.ID(hostProtocolUiHits, "dialog-effect-themes")
	hitDialogEffectCursorShadow   = tree.ID(hostProtocolUiHits, "dialog-effect-cursor-shadow")
	hitDialogChromeless           = tree.ID(hostProtocolUiHits, "dialog-chromeless")
	hitDialogPortsTrigger         = tree.ID(hostProtocolUiHits, "dialog-ports-trigger")
	hitDialogAlgorithms           = tree.ID(hostProtocolUiHits, "dialog-algorithms")
	hitDialogLoginCommand         = tree.ID(hostProtocolUiHits, "dialog-login-command")
	hitDialogLoginCommandPolicy   = tree.ID(hostProtocolUiHits, "dialog-login-command-policy")
	hitDialogAdvanced             = tree.ID(hostProtocolUiHits, "dialog-advanced")
	hitDialogForwardX11           = tree.ID(hostProtocolUiHits, "dialog-forward-x11")
	hitDialogAgentForward         = tree.ID(hostProtocolUiHits, "dialog-agent-forward")
	hitDialogInteractiveOnly      = tree.ID(hostProtocolUiHits, "dialog-interactive-only")
	hitDialogNoHistory            = tree.ID(hostProtocolUiHits, "dialog-no-history")
	hitDialogIdentitiesOnly       = tree.ID(hostProtocolUiHits, "dialog-identities-only")
	hitDialogIdentitiesOnlyHelp   = tree.ID(hostProtocolUiHits, "dialog-identities-only-help")
	hitDialogAgentAccess          = tree.ID(hostProtocolUiHits, "dialog-agent-access")
	hitDialogAgentAccessHelp      = tree.ID(hostProtocolUiHits, "dialog-agent-access-help")
	hitDialogOTP                  = tree.ID(hostProtocolUiHits, "dialog-otp")
	hitDialogOTPRemove            = tree.ID(hostProtocolUiHits, "dialog-otp-remove")
	hitDialogOTPScan              = tree.ID(hostProtocolUiHits, "dialog-otp-scan")
	hitDialogOTPPaste             = tree.ID(hostProtocolUiHits, "dialog-otp-paste")
)

// maxListedPorts bounds the picker: a machine with more serial devices than
// this is not choosing from a list.
const maxListedPorts = 12

// hostConnectionType is one entry of the host form's connection-type choice.
// Mosh is a connection type to the user even though the saved host is an SSH
// host whose terminal transport is mosh.
type hostConnectionType uint8

const (
	connectionSSH hostConnectionType = iota
	connectionMosh
	connectionTelnet
	connectionSerial
	connectionVNC
	connectionRDP
)

func (kind hostConnectionType) protocol() hostProtocol {
	switch kind {
	case connectionMosh, connectionSSH:
		return protocolSSH
	case connectionTelnet:
		return protocolTelnet
	case connectionSerial:
		return protocolSerial
	case connectionVNC:
		return protocolVNC
	case connectionRDP:
		return protocolRDP
	}
	return protocolSSH
}

func (kind hostConnectionType) terminal() hostTerminal {
	if kind == connectionMosh {
		return terminalMosh
	}
	return terminalSSH
}

// connectionType is the form's current connection type.
func (editor *hostEditor) connectionType() hostConnectionType {
	if editor.protocol == protocolSSH {
		if editor.terminal == terminalMosh {
			return connectionMosh
		}
		return connectionSSH
	}
	switch editor.protocol {
	case protocolTelnet:
		return connectionTelnet
	case protocolSerial:
		return connectionSerial
	case protocolVNC:
		return connectionVNC
	case protocolRDP:
		return connectionRDP
	}
	return connectionSSH
}

// The segmented choices of the host form. Each is a Choice of the form's own
// page, declared at the row its layout reserved.
var (
	hostConnectionChoices  = [...]hostConnectionType{connectionSSH, connectionTelnet, connectionSerial, connectionVNC, connectionRDP, connectionMosh}
	hostFlowChoices        = [...]serial.Flow{serial.FlowNone, serial.FlowHardware, serial.FlowSoftware}
	hostAlgoChoices        = [...]algoMode{algoDefault, algoCompatible, algoCustom}
	hostLoginPolicyChoices = [...]loginFailurePolicy{loginFailureStop, loginFailureContinue}
)

// hostConnectionChoiceLabel names one connection type as the form's choice
// shows it.
func (state *State) hostConnectionChoiceLabel(kind hostConnectionType) string {
	if kind == connectionMosh {
		return state.messages.Tools.ProtocolMosh
	}
	return state.hostProtocolChoiceLabel(kind.protocol())
}

// hostProtocolChoiceLabel names one protocol as the form's choice shows it.
func (state *State) hostProtocolChoiceLabel(protocol hostProtocol) string {
	switch protocol {
	case protocolTelnet:
		return state.messages.Tools.ProtocolTelnet
	case protocolSerial:
		return state.messages.Tools.ProtocolSerial
	case protocolVNC:
		return state.messages.Desktop.ProtocolVNC
	case protocolRDP:
		return state.messages.Desktop.ProtocolRDP
	default:
		return state.messages.Tools.ProtocolSSH
	}
}

// hostFlowChoiceLabel names one flow control as the form's choice shows it.
func (state *State) hostFlowChoiceLabel(flow serial.Flow) string {
	switch flow {
	case serial.FlowHardware:
		return state.messages.Tools.FlowHardware
	case serial.FlowSoftware:
		return state.messages.Tools.FlowSoftware
	default:
		return state.messages.Tools.FlowNone
	}
}

// hostAlgoChoiceLabel names one algorithm mode as the form's choice shows it.
func (state *State) hostAlgoChoiceLabel(mode algoMode) string {
	switch mode {
	case algoCompatible:
		return state.messages.Tools.AlgoCompatible
	case algoCustom:
		return state.messages.Tools.AlgoCustom
	default:
		return state.messages.Tools.AlgoDefault
	}
}

// hostLoginPolicyChoiceLabel names one post-login failure policy as the form's choice shows it.
func (state *State) hostLoginPolicyChoiceLabel(policy loginFailurePolicy) string {
	switch policy {
	case loginFailureContinue:
		return state.messages.Tools.LoginCommandContinueOnError
	default:
		return state.messages.Tools.LoginCommandStopOnError
	}
}

// declareHostProtocolChoice declares the connection-type choice in the form's
// page at the slot the body reserved for it. It writes editor.connection; a
// change maps that to the protocol and terminal transport the form then shows,
// and applyProtocol is what follows a real protocol change.
func (state *State) declareHostProtocolChoice(c ui.C) ui.Box {
	editor := &state.editor
	editor.connectionBefore = editor.connectionType()
	editor.connection = editor.connectionBefore
	choice := ui.Choice(c, &editor.connection, hostConnectionChoices[:], state.hostConnectionChoiceLabel).Fill()
	if choice.Changed() {
		editor.applyConnection(editor.connectionBefore)
		state.dialogError = ""
	}
	return choice
}

// declareHostFlowChoice declares the serial flow-control choice in the form's
// page. The click that already wrote editor.flow only has the form's error to
// clear.
func (state *State) declareHostFlowChoice(c ui.C) ui.Box {
	choice := hostFormChoice(c, state.messages.Tools.FieldFlow, hostFormRowHeight, &state.editor.flow, hostFlowChoices[:], state.hostFlowChoiceLabel)
	if choice.Changed() {
		state.dialogError = ""
	}
	return choice
}

// declareHostAlgoChoice declares the SSH algorithm choice in the form's page.
func (state *State) declareHostAlgoChoice(c ui.C) ui.Box {
	choice := hostFormChoice(c, state.messages.Tools.AlgoField, hostFormRowHeight, &state.editor.algoMode, hostAlgoChoices[:], state.hostAlgoChoiceLabel)
	if choice.Changed() {
		state.dialogError = ""
	}
	return choice
}

// declareHostEndpointSlots lays the endpoint block out in the form's page: the
// address and the port, and the credentials under them, by the protocol. The
// fields are page controls; the roles a protocol does not use alias one it does,
// so the event pass of a protocol change can still paint the protocol now in
// effect.
func (state *State) declareHostEndpointSlots(c ui.C, parent ui.Box, rowHeight float32) {
	editor := &state.editor
	form := &editor.form
	slots := &editor.form.body.endpointSlots
	*slots = hostEndpointSlots{}
	m, tools := state.messages, state.messages.Tools
	parent.Children(func() {
		switch editor.protocol {
		case protocolSerial:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.address, &form.fieldAddress, hitDialogAddress, &editor.address, tools.FieldDevice, tools.FieldDevicePlaceholder, false, rowHeight)
				slots.picker = ui.Slot(c).WidthPercent(30)
				state.declareHostPortsSelect(c, slots.picker, rowHeight)
			})
			// The flow control is a three-way choice: it takes the row under
			// the line settings rather than sharing one with them, where its
			// labels would be cut.
			slots.second = ui.Slot(c).Height(rowHeight)
			state.declareHostTextField(c, slots.second, &form.fieldPort, hitDialogPort, &editor.port, tools.FieldLine, tools.FieldLinePlaceholder, false, rowHeight)
			slots.third = ui.Slot(c).Height(rowHeight)
		case protocolTelnet:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.address, &form.fieldAddress, hitDialogAddress, &editor.address, m.FieldAddress, m.FieldAddressPlaceholder, false, rowHeight)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
				state.declareHostTextField(c, slots.third, &form.fieldPort, hitDialogPort, &editor.port, m.FieldPort, "", false, rowHeight)
			})
		case protocolVNC:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.address, &form.fieldAddress, hitDialogAddress, &editor.address, m.FieldAddress, m.FieldAddressPlaceholder, false, rowHeight)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
				state.declareHostTextField(c, slots.third, &form.fieldPort, hitDialogPort, &editor.port, m.FieldPort, "", false, rowHeight)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.second = ui.Slot(c).Grow(1)
				label := state.messages.Desktop.FieldVNCUser
				hint := state.hostUserHint(state.messages.Desktop.VNCUserHint)
				if editor.viaSSH {
					label = state.messages.Desktop.FieldSSHUser
					hint = state.hostUserHint(state.hostSystemUserHint())
				}
				state.declareHostTextField(c, slots.second, &form.fieldUser, hitDialogUser, &editor.user, label, hint, false, rowHeight)
				slots.password = ui.Slot(c).Grow(1)
				state.declareHostPasswordField(c, slots.password, m.FieldVNCPassword)
			})
		case protocolSSH:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.address, &form.fieldAddress, hitDialogAddress, &editor.address, m.FieldAddress, m.FieldAddressPlaceholder, false, rowHeight)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
				state.declareHostTextField(c, slots.third, &form.fieldPort, hitDialogPort, &editor.port, m.FieldPort, "", false, rowHeight)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.second = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.second, &form.fieldUser, hitDialogUser, &editor.user, m.FieldUser, state.hostUserHint(state.hostSystemUserHint()), false, rowHeight)
				slots.authentication = ui.Slot(c).Grow(1)
				state.declareHostAuthenticationChoice(c, slots.authentication, rowHeight)
				if editor.auth == authPrivateKey {
					slots.identity = ui.Slot(c).Grow(1)
				} else if editor.auth.usesPassword() {
					slots.password = ui.Slot(c).Grow(1)
					state.declareHostPasswordField(c, slots.password, m.FieldPassword)
				}
			})
			if state.hostAsksKeyPassphrase() {
				slots.passphrase = ui.Slot(c).Height(rowHeight)
				state.declareHostTextField(c, slots.passphrase, &form.fieldPassphrase, hitDialogPassphrase, &editor.passphrase, m.KeyPassphraseLabel, state.hostPassphraseHint(), true, rowHeight)
			}
		case protocolRDP:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.address, &form.fieldAddress, hitDialogAddress, &editor.address, m.FieldAddress, m.FieldAddressPlaceholder, false, rowHeight)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
				state.declareHostTextField(c, slots.third, &form.fieldPort, hitDialogPort, &editor.port, m.FieldPort, "", false, rowHeight)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.second = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.second, &form.fieldUser, hitDialogUser, &editor.user, state.messages.Desktop.FieldRDPUser, state.hostUserHint(state.messages.Desktop.RDPUserHint), false, rowHeight)
				slots.password = ui.Slot(c).Grow(1)
				state.declareHostPasswordField(c, slots.password, m.FieldPassword)
			})
		default:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.address, &form.fieldAddress, hitDialogAddress, &editor.address, m.FieldAddress, m.FieldAddressPlaceholder, false, rowHeight)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
				state.declareHostTextField(c, slots.third, &form.fieldPort, hitDialogPort, &editor.port, m.FieldPort, "", false, rowHeight)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.second = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, slots.second, &form.fieldUser, hitDialogUser, &editor.user, m.FieldUser, state.hostUserHint(state.hostSystemUserHint()), false, rowHeight)
				slots.password = ui.Slot(c).Grow(1)
				state.declareHostPasswordField(c, slots.password, m.FieldPassword)
			})
		}
	})
	// A role this protocol has no row for aliases one it does, so a protocol
	// change can paint the new protocol in the frame's event pass.
	if !slots.picker.Valid() {
		slots.picker = slots.address
	}
	if !slots.second.Valid() {
		slots.second = slots.address
	}
	if !slots.authentication.Valid() {
		slots.authentication = slots.second
	}
	if !slots.password.Valid() {
		slots.password = slots.second
	}
	if !slots.identity.Valid() {
		slots.identity = slots.second
	}
	if !slots.passphrase.Valid() {
		slots.passphrase = slots.third
	}
}

// declareHostTextField declares one labelled text field of the endpoint block in
// the page and remembers it for the form's tab ring.
func (state *State) declareHostTextField(c ui.C, parent ui.Box, field *dialogField, id gfx.ID, editor *text.Editor, label, placeholder string, secret bool, height float32) {
	parent.Children(func() {
		field.declare(c, state, editor, dialogFieldSpec{ID: id, Label: label, Placeholder: placeholder, Secret: secret, Height: height})
	})
	state.editor.form.body.addField(id, field)
}

func (state *State) declareHostTextAreaField(c ui.C, parent ui.Box, field *dialogField, id gfx.ID, editor *text.Editor, label, placeholder string, height float32) {
	parent.Children(func() {
		field.declare(c, state, editor, dialogFieldSpec{ID: id, Label: label, Placeholder: placeholder, Multiline: true, Height: height})
	})
	state.editor.form.body.addField(id, field)
}

// declareHostAuthenticationChoice declares the authentication policy as a page
// select over the protocols' fixed choices. A change closes the pickers over the
// field and refills the secrets the new policy needs.
func (state *State) declareHostAuthenticationChoice(c ui.C, parent ui.Box, rowHeight float32) {
	editor := &state.editor
	form := &editor.form
	var choices [authCount]hostAuth
	count := authCount
	if !sshconn.KerberosAvailable {
		count = authKerberos
	}
	for index := authAutomatic; index < count; index++ {
		choices[int(index)] = index
	}
	label := state.messages.FieldAuthentication
	parent.Children(func() {
		ui.FormField(c, &form.authField, label).FillWidth().Height(rowHeight).Children(func() {
			form.authBox = ui.Select(c, &editor.auth, choices[:count], state.authLabel).Label(label)
			if form.authBox.Changed() {
				editor.identityOpen, editor.credentialOpen = false, false
				state.dialogError = ""
				c.After(func() { state.prefillKeyPassphrase() })
			}
		})
	})
}

// declareHostPortsSelect declares the detected serial ports as a page select
// under the form's board. A choice writes the device into the device field.
func (state *State) declareHostPortsSelect(c ui.C, parent ui.Box, rowHeight float32) {
	editor, form := &state.editor, &state.editor.form
	// A serial device can be plugged in while the form is open: the old picker
	// looked the machine's ports up each time it was opened, and the page select
	// reads them again on the press that opens it.
	if canvas := c.Canvas(); canvas != nil && canvas.Clicked(hitDialogPortsTrigger) {
		editor.ports = serialPorts()
	}
	form.portChoices = form.portChoices[:0]
	count := min(len(editor.ports), maxListedPorts)
	for index := 0; index < count; index++ {
		form.portChoices = append(form.portChoices, editor.ports[index].Device)
	}
	form.portChoice = editor.value(fieldAddress)
	label := state.messages.Tools.PortsPicker
	parent.Children(func() {
		ui.FormField(c, &form.portsField, label).FillWidth().Height(rowHeight).Children(func() {
			form.portsBox = ui.Select(c, &form.portChoice, form.portChoices, func(device string) string { return device }).
				Label(label).Placeholder("…").Small()
			if form.portsBox.Changed() {
				editor.address = text.Editor{}
				editor.address.Insert(form.portChoice)
				state.dialogError = ""
			}
		})
	})
}

// declareHostPasswordField declares the password row's title as a page form
// field and keeps the slot its hand-painted addons and text go in.
func (state *State) declareHostPasswordField(c ui.C, parent ui.Box, title string) {
	form := &state.editor.form
	parent.Children(func() {
		// The title labels the hand-painted control: a press on it focuses and
		// clicks the field, as BeginField's For did.
		form.passwordField.For = hitDialogPassword
		ui.FormField(c, &form.passwordField, title).FillWidth().Height(float32(hostFormRowHeight)).Children(func() {
			form.passwordControl = ui.Slot(c).Fill()
		})
	})
}

// paintHostEndpointRows paints the endpoint controls a page select cannot show:
// the identity picker's import row and the shared-password field with its
// addons. Everything else in the endpoint block is the page's.
func (state *State) paintHostEndpointRows(canvas *gfx.Canvas) {
	m, editor := state.messages, &state.editor
	slots := &editor.form.body.endpointSlots
	password := slots.password.Rect()
	if editor.form.passwordControl.Valid() {
		password = editor.form.passwordControl.Rect()
	}
	switch editor.protocol {
	case protocolSSH:
		if editor.auth == authPrivateKey {
			state.paintHostIdentityChoice(canvas, slots.identity.Rect())
		} else if editor.auth.usesPassword() {
			state.paintHostPasswordWithCredential(canvas, password, m.FieldPassword, state.hostPasswordHint())
		}
	case protocolVNC:
		state.paintHostPasswordWithCredential(canvas, password, m.FieldVNCPassword, state.hostPasswordHint())
	case protocolRDP:
		state.paintHostPasswordWithCredential(canvas, password, m.FieldPassword, state.hostPasswordHint())
	}
}

func (state *State) hostPasswordHint() string {
	switch state.hostDialogMode {
	case hostDialogEdit:
		return state.messages.FieldPasswordKeepHint
	case hostDialogCopy:
		return state.messages.FieldPasswordCopyHint
	default:
		return state.messages.FieldPasswordHint
	}
}

// clickHostProtocol handles the protocol controls of the host dialog. Its
// segmented choices are pages now; the clicks they deliver write their own value.
func (state *State) clickHostProtocol(id gfx.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogViaSSH:
		// A route owns the connection of a host that has one; the same-machine
		// tunnel cannot also apply.
		if !state.desktopRouteConfigured() {
			editor.viaSSH = !editor.viaSSH
		}
	case id == hitDialogClipboard:
		editor.clipboard = !editor.clipboard
	case id == hitDialogChromeless:
		editor.chromeless = !editor.chromeless
	case id == hitDialogConsole:
		editor.console = !editor.console
	case id == hitDialogRemoteAudio:
		editor.noAudio = !editor.noAudio
	case id == hitDialogStandardSecurity:
		editor.standardSecurity = !editor.standardSecurity
	case id == hitDialogLegacyTLS:
		editor.legacyTLS = !editor.legacyTLS
	case state.clickDesktopQuality(id):
		// An RDP effect switch flipped its own setting.
	case id == hitDialogAdvanced:
		editor.advancedOpen = !editor.advancedOpen
	case id == hitDialogForwardX11:
		editor.forwardX11 = !editor.forwardX11
	case id == hitDialogAgentForward:
		editor.agentForward = !editor.agentForward
	case id == hitDialogInteractiveOnly:
		editor.interactiveOnly = !editor.interactiveOnly
	case id == hitDialogIdentitiesOnly:
		editor.identitiesOnly = !editor.identitiesOnly
	case id == hitDialogNoHistory:
		editor.noHistory = !editor.noHistory
	case id == hitDialogLoginCommandPolicy:
		if editor.loginFailurePolicy == loginFailureStop {
			editor.loginFailurePolicy = loginFailureContinue
		} else {
			editor.loginFailurePolicy = loginFailureStop
		}
	case id == hitDialogAgentAccess:
		editor.agentAccess = !editor.agentAccess
		editor.agentAccessChanged = true
	case state.clickHostAuthentication(id):
	case state.clickHostCredential(id):
	case state.clickHostColor(id):
	case id == hitDialogOTPRemove:
		editor.otpRemove = !editor.otpRemove
	case id == hitDialogOTPScan:
		state.scanOTPImage()
		return true // it sets the dialog's error itself
	case id == hitDialogOTPPaste:
		state.pasteOTPImage()
		return true
	default:
		return false
	}
	state.dialogError = ""
	return true
}

// hostFormRowHeight is the height of one labelled row of the host dialog.
const hostFormRowHeight = 70

// hostPortWidth is the width of the port field: the label and five digits fit,
// and the address keeps the rest of the row.
const hostPortWidth = 84

// hostEndpointRows is how many labelled rows the endpoint block of the form
// takes: the address with the port and, under it, the credentials. A serial
// port takes three (its device, its line, and the flow control) and Telnet one
// (Telnet has no credentials in the form).
func (state *State) hostEndpointRows() int {
	switch state.editor.protocol {
	case protocolSerial:
		return 3
	case protocolTelnet:
		return 1
	case protocolSSH:
		if state.hostAsksKeyPassphrase() {
			return 3
		}
		return 2
	}
	return 2
}

// hostAsksKeyPassphrase reports whether the form takes the passphrase of the
// key the host is pinned to: only a single, encrypted key has one to take. With
// every key in play there is no one passphrase to ask for here.
func (state *State) hostAsksKeyPassphrase() bool {
	editor := &state.editor
	if editor.protocol != protocolSSH || editor.auth != authPrivateKey || editor.identity == "" {
		return false
	}
	identity, ok := state.identityForFingerprint(editor.identity)
	return ok && identity.Encrypted && !identity.SecurityKey
}

func (state *State) hostPassphraseHint() string {
	if state.hostDialogMode == hostDialogEdit {
		return state.messages.FieldPasswordKeepHint
	}
	return state.messages.FieldPasswordHint
}

// Size the body from its rows; short viewports scroll instead of shrinking them.
// The fixed rows are Label/Color, Group/Tags and the Icon row, which carries the
// file transfer mode and, on a protocol with a shell, the encoding.
func (state *State) hostFormHeight() float32 {
	height := float32(2+44+(3+state.hostEndpointRows())*hostFormRowHeight) + state.hostDialogExtra()
	if state.editor.protocol != protocolSerial {
		height += hostFormRowHeight
	}
	if state.editor.protocol == protocolSSH {
		// The terminal transport choice (with its hint when mosh is chosen)
		// sits under the protocol choice.
		if state.editor.terminal == terminalMosh {
			height += 36
		}
	}
	if state.editor.protocol == protocolSSH || state.editor.protocol == protocolVNC || state.editor.protocol == protocolRDP {
		// The jump route row sits under the proxy row: SSH hosts and remote
		// desktops are both reached through one.
		height += hostFormRowHeight
	}
	if state.dialogError != "" {
		height += 32
		if state.isHostDialogKeyLost() {
			height += 38
		}
	}
	return height
}

// The Advanced section of an SSH host is the algorithm choice, the one-time code
// secret with its buttons, and two groups of switches under their own headings:
// the session ones ("Forward X11", "Forward ssh-agent", "Don't record commands")
// and the one that opens the host to AI agents, which explains itself in a help
// tooltip.
//
// A switch's hit region is 32px high, so consecutive rows keep that distance (a
// 20px row and 12px above it) for their hit regions not to overlap, and all of
// them are the same distance apart.
const (
	// otpFieldHeight is the label and input of the one-time code secret, with
	// only the little room under it that keeps its buttons tied to it.
	otpFieldHeight = 58
	// groupHeadingHeight is a rule and the small heading under it.
	groupHeadingHeight = 40
)

// sshSwitchRowsHeight is the room of the Advanced section's two switch groups:
// the session switches and the AI-agent switch. The first row sits 4px under its
// heading, the others 12px under the row above, and 16px are left at the end.
// Automatic authentication brings one session switch more: the agent-identity
// restriction.
func (state *State) sshSwitchRowsHeight() float32 {
	height := float32(groupHeadingHeight + (4 + 20) + 3*(12+20) + groupHeadingHeight + (4 + 20) + 16)
	if state.editor.auth == authAutomatic {
		height += 12 + 20
	}
	return height
}

// advancedHeaderHeight is the row that opens the Advanced section of an SSH
// host, with the gap above it.
const advancedHeaderHeight = 48

// advancedHeaderGap is the space above that row, and the same space is left under
// it: 8px each, around its 32px.
const advancedHeaderGap = 8

const loginCommandAreaHeight = 104

// sshAdvancedRowsHeight is the room of the SSH-only part of an open Advanced
// section: the algorithm choice (a third row for a custom list), the login
// command and failure policy, the one-time code secret with its buttons, and
// the switches.
func (state *State) sshAdvancedRowsHeight() float32 {
	if state.editor.protocol != protocolSSH {
		return 0
	}
	rows := float32(1)
	if state.editor.algoMode == algoCustom {
		rows++
	}
	return rows*hostFormRowHeight + loginCommandAreaHeight + hostFormRowHeight + otpFieldHeight + otpButtonsRow + state.sshSwitchRowsHeight()
}

// idleAdvancedRowsHeight is the room of the idle keepalive rows: a heading, the
// interval, the custom interval and unit when chosen, and the send content with
// its custom field when the probe is on.
func (state *State) idleAdvancedRowsHeight() float32 {
	if !idleKeepAliveProtocol(state.editor.protocol) {
		return 0
	}
	height := float32(groupHeadingHeight + hostFormRowHeight)
	if state.editor.idleKeepAlive == idleKeepAliveCustom {
		height += hostFormRowHeight
	}
	if state.editor.idleKeepAlive != idleKeepAliveOff {
		height += hostFormRowHeight
		if state.editor.idleInputValue() == idleInputCustom {
			height += hostFormRowHeight
		}
	}
	return height + 16
}

// advancedSectionHeight is the room of a terminal host's Advanced section: its
// row alone while closed, and open the protocol's rows plus the idle keepalive.
func (state *State) advancedSectionHeight() float32 {
	if !state.editor.advancedOpen {
		return advancedHeaderHeight
	}
	return advancedHeaderHeight + state.sshAdvancedRowsHeight() + state.idleAdvancedRowsHeight()
}

// hostDialogExtra is the height the protocol-specific rows add to the host dialog.
func (state *State) hostDialogExtra() float32 {
	if state.editor.protocol == protocolVNC || state.editor.protocol == protocolRDP {
		return state.desktopRowsHeight()
	}
	if !idleKeepAliveProtocol(state.editor.protocol) {
		return 0
	}
	return state.advancedSectionHeight()
}

// otpButtonsRow is the room of the second row of the one-time code secret: the
// scan, paste and remove buttons go below the field, which would otherwise be
// left with too little width for its own placeholder.
const otpButtonsRow = 38

// declareHostAdvancedHeader declares the row that opens or closes an SSH host's
// less-used settings, and the chevron it paints, in the form's page.
func (state *State) declareHostAdvancedHeader(c ui.C) (header, icon ui.Box) {
	pal, open := state.pal, state.editor.advancedOpen
	label := state.messages.Tools.AdvancedOptions
	header = ui.Row(c).FillWidth().MarginTop(advancedHeaderGap).Height(32).PadSides(0, 0, 0, 2).Gap(8).Items(ui.Center).
		Radius(radius.control).Hover(ui.Look{}.Background(pal.surfaceSoft)).
		HitWith(hitDialogAdvanced, label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: open})
	header.Children(func() {
		icon = ui.Slot(c).Size(16, 16)
		ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(20)
	})
	return header, icon
}

// paintAdvancedSection draws the row that opens or closes an SSH host's
// less-used settings and, while open, the settings under it.
func (state *State) paintAdvancedSection(canvas *gfx.Canvas) {
	pal, open := state.pal, state.editor.advancedOpen
	header := state.editor.form.body.advancedHeader
	icon := state.editor.form.body.advancedHeaderIcon
	state.rememberHostControl(hitDialogAdvanced, header.Rect())
	chevron := iconChevronRight
	if open {
		chevron = iconChevronDown
	}
	paintIcon(canvas, uint8(chevron), icon.Rect(), pal.muted)
	if open {
		if state.editor.form.layoutProtocol == protocolSSH {
			state.paintAlgorithmRows(canvas)
		}
	}
}

// The desktop switches and the SSH algorithm rows are slots of the host form's
// own page.
const (
	// The widths of the one-time code row's buttons.
	algoRemoveWidth, algoScanWidth, algoPasteWidth = 84, 132, 96
)

// hostVNCRowsBody is where the desktop switches' body put each checkbox row.
type hostVNCRowsBody struct {
	standardSecurityHelp, legacyTLSHelp ui.Box
}

// hostAlgoRowsBody is where the SSH algorithm rows put each control.
type hostAlgoRowsBody struct {
	algo, custom, login, loginPolicy, otp, scan, paste, remove ui.Box
	identitiesOnlyHelp, agentHelp                              ui.Box
}

// paintAlgorithmRows draws the SSH algorithm choice and, for a custom one, the
// list that is allowed, then the one-time code secret. The rows were declared
// with the host form's page; this paints into the slots it left.
func (state *State) paintAlgorithmRows(canvas *gfx.Canvas) {
	m, editor := state.messages.Tools, &state.editor
	b := &editor.form.algoBody
	state.rememberHostControl(hitDialogOTPScan, b.scan.Rect())
	state.rememberHostControl(hitDialogOTPPaste, b.paste.Rect())
	if editor.otpSaved {
		state.rememberHostControl(hitDialogOTPRemove, b.remove.Rect())
	}
	if editor.auth == authAutomatic {
		state.paintHelpIcon(canvas, hitDialogIdentitiesOnlyHelp, m.IdentitiesOnlyNote, b.identitiesOnlyHelp.Rect())
	}
	// After the checkbox, whose words reach to the end of the row, so that this is
	// the hit on top where the two meet.
	state.paintHelpIcon(canvas, hitDialogAgentAccessHelp, m.AgentAccessNote, b.agentHelp.Rect())
}

// declareHostAlgoRows lays out the SSH algorithm choice, the custom list, the
// login command, the one-time code row and the session and agent switches.
func (state *State) declareHostAlgoRows(c ui.C) {
	m, editor, pal := state.messages.Tools, &state.editor, state.pal
	form := &editor.form
	b := &editor.form.algoBody
	*b = hostAlgoRowsBody{}
	customMode := editor.form.algoLayout == algoCustom
	rowHeight := float32(hostFormRowHeight)
	ui.Column(c).FillWidth().Children(func() {
		b.algo = ui.Slot(c).Height(rowHeight)
		if customMode {
			b.custom = ui.Slot(c).Height(rowHeight)
			state.declareHostTextField(c, b.custom, &form.fieldAlgoCustom, hitDialogAlgorithms, &editor.algorithms, m.AlgoCustomField, m.AlgoCustomPlaceholder, false, rowHeight)
		}
		b.login = ui.Slot(c).Height(loginCommandAreaHeight)
		state.declareHostTextAreaField(c, b.login, &form.fieldLoginCommand, hitDialogLoginCommand, &editor.loginCommand, m.LoginCommandField, m.LoginCommandPlaceholder, loginCommandAreaHeight)
		b.loginPolicy = hostFormChoice(c, m.LoginCommandPolicyField, rowHeight, &editor.loginFailurePolicy, hostLoginPolicyChoices[:], state.hostLoginPolicyChoiceLabel)
		if b.loginPolicy.Changed() {
			state.dialogError = ""
		}
		ui.Row(c).Height(otpFieldHeight).Gap(8).Children(func() {
			b.otp = ui.Slot(c).Grow(1)
			placeholder := m.OTPPlaceholder
			if editor.otpSaved {
				placeholder = m.OTPSavedPlaceholder
				if editor.otpRemove {
					placeholder = m.OTPRemovingPlaceholder
				}
			}
			state.declareHostTextField(c, b.otp, &form.fieldOTP, hitDialogOTP, &editor.otp, m.OTPField, placeholder, false, otpFieldHeight)
		})
		ui.Row(c).Height(otpButtonsRow).Gap(8).Children(func() {
			b.scan = ui.Button(c, m.OTPScan).Small().Outline().Hit(hitDialogOTPScan, m.OTPScan).Size(algoScanWidth, 30).Self(ui.Start)
			b.paste = ui.Button(c, m.OTPPaste).Small().Outline().Hit(hitDialogOTPPaste, m.OTPPaste).Size(algoPasteWidth, 30).Self(ui.Start)
			if editor.otpSaved {
				label := m.OTPRemove
				if editor.otpRemove {
					label = m.OTPKeep
				}
				b.remove = ui.Button(c, label).Small().Outline().Hit(hitDialogOTPRemove, label).Size(algoRemoveWidth, 30).Self(ui.Start)
			}
		})
		heading := func(title string) {
			ui.Column(c).Height(groupHeadingHeight).Children(func() {
				ui.Column(c).Height(1).MarginSides(12, 0, 0, 0).Background(withAlpha(pal.line, 0.7))
				ui.Text(c, title).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).MarginSides(8, 0, 0, 2)
			})
		}
		heading(m.AdvancedSession)
		state.declareDialogCheck(c, hitDialogForwardX11, m.X11Forward, editor.forwardX11, 4, 0)
		state.declareDialogCheck(c, hitDialogAgentForward, m.AgentForward, editor.agentForward, 12, 0)
		state.declareDialogCheck(c, hitDialogNoHistory, state.messages.History.HostNoRecord, editor.noHistory, 12, 0)
		state.declareDialogCheck(c, hitDialogInteractiveOnly, m.InteractiveOnly, editor.interactiveOnly, 12, 0)
		if editor.auth == authAutomatic {
			_, b.identitiesOnlyHelp = state.declareDialogCheckHelp(c, hitDialogIdentitiesOnly, m.IdentitiesOnly, editor.identitiesOnly, 12, 0)
		}
		heading(m.AdvancedAgents)
		_, b.agentHelp = state.declareDialogCheckHelp(c, hitDialogAgentAccess, m.AgentAccess, editor.agentAccess, 4, 0)
		state.declareHostIdleKeepAliveRows(c)
	})
}

// paintOTPRow draws the one-time code secret: a field for a new one, a button to
// read it from a QR code image, and for a
// saved one a button, in its own rectangle, to take it away. The secret itself
// is never drawn.
// vncRowsHeight is the room of the three switches every remote desktop host has.
const vncRowsHeight = 4 + 20 + (12 + 20) + (12 + 20) + 16

// rdpRowsHeight adds the four switches only an RDP host has: the console
// session, remote audio, Standard RDP Security and legacy TLS.
const rdpRowsHeight = vncRowsHeight + 4*(12+20)

// desktopRowsHeight is the room paintVNCRows takes for the protocol on screen.
func (state *State) desktopRowsHeight() float32 {
	if state.editor.protocol == protocolRDP {
		return rdpRowsHeight + desktopQualityRowsHeight(protocolRDP)
	}
	return vncRowsHeight + desktopQualityRowsHeight(protocolVNC)
}

// paintVNCRows paints the switches of a VNC or RDP host in the slots the host
// form's page laid out: connecting through SSH, sharing the clipboard, showing
// the picture without the bars around it and, for RDP, asking Windows for its
// console (administrative) session, playing remote sound, choosing Standard
// RDP Security and allowing the legacy TLS suites. A host whose route carries
// the connection cannot also use the same-machine tunnel, so that switch is
// dimmed and inert.
func (state *State) paintVNCRows(canvas *gfx.Canvas) {
	editor, m := &state.editor, state.messages.Desktop
	b := &editor.form.vncBody
	// The rows this frame laid out are the ones before a protocol click wrote
	// its new value at the end of the declaration: painting the console row
	// needs the RDP rows on the page, not just RDP in effect.
	if editor.form.layoutProtocol == protocolRDP {
		// After the checkbox, whose words reach to the end of the row, so that
		// this is the hit on top where the two meet.
		state.paintHelpIcon(canvas, hitDialogLegacyTLSHelp, m.LegacyTLSNote, b.legacyTLSHelp.Rect())
		state.paintHelpIcon(canvas, hitDialogStandardSecurityHelp, m.StandardSecurityNote, b.standardSecurityHelp.Rect())
	}
}

// declareHostVNCRows lays out the switches of a VNC or RDP host: connecting
// through SSH, sharing the clipboard, showing the picture without the bars
// around it and, for RDP, asking Windows for its console (administrative)
// session, playing remote sound, choosing Standard RDP Security and allowing
// the legacy TLS suites.
func (state *State) declareHostVNCRows(c ui.C) {
	editor, m := &state.editor, state.messages.Desktop
	b := &editor.form.vncBody
	*b = hostVNCRowsBody{}
	viaLabel, routed := m.ViaSSH, state.desktopRouteConfigured()
	if routed {
		viaLabel = m.ViaSSHRouted
	}
	ui.Column(c).Fill().Children(func() {
		if routed {
			state.declareDialogCheckDisabled(c, hitDialogViaSSH, viaLabel, editor.viaSSH, 4, 0)
		} else {
			state.declareDialogCheck(c, hitDialogViaSSH, viaLabel, editor.viaSSH, 4, 0)
		}
		state.declareDialogCheck(c, hitDialogClipboard, m.ShareClipboard, editor.clipboard, 12, 0)
		state.declareDialogCheck(c, hitDialogChromeless, m.HideBars, editor.chromeless, 12, 0)
		if editor.protocol == protocolRDP {
			state.declareDialogCheck(c, hitDialogConsole, m.ConsoleSession, editor.console, 12, 0)
			state.declareDialogCheck(c, hitDialogRemoteAudio, m.RemoteAudio, !editor.noAudio, 12, 0)
			_, b.legacyTLSHelp = state.declareDialogCheckHelp(c, hitDialogLegacyTLS, m.LegacyTLS, editor.legacyTLS, 12, 0)
			_, b.standardSecurityHelp = state.declareDialogCheckHelp(c, hitDialogStandardSecurity, m.StandardSecurity, editor.standardSecurity, 12, 0)
		}
		ui.Slot(c).Height(16)
		state.declareDesktopQualityRows(c)
	})
}
