package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/serial"
)

// The protocol part of the host dialog: which way the host is reached, and for
// a serial port the device, the line and the flow control.

// hostProtocolUiHits is the place of the controls this file paints.
var hostProtocolUiHits = tree.Feature("host-protocol-ui")

var (
	hitDialogViaSSH               = tree.ID(hostProtocolUiHits, "dialog-via-ssh")
	hitDialogClipboard            = tree.ID(hostProtocolUiHits, "dialog-clipboard")
	hitDialogConsole              = tree.ID(hostProtocolUiHits, "dialog-console")
	hitDialogStandardSecurity     = tree.ID(hostProtocolUiHits, "dialog-standard-security")
	hitDialogStandardSecurityHelp = tree.ID(hostProtocolUiHits, "dialog-standard-security-help")
	hitDialogChromeless           = tree.ID(hostProtocolUiHits, "dialog-chromeless")
	hitDialogPortsTrigger         = tree.ID(hostProtocolUiHits, "dialog-ports-trigger")
	hitDialogPortsDismiss         = tree.ID(hostProtocolUiHits, "dialog-ports-dismiss")
	hitDialogAlgorithms           = tree.ID(hostProtocolUiHits, "dialog-algorithms")
	hitDialogLoginCommand         = tree.ID(hostProtocolUiHits, "dialog-login-command")
	hitDialogAdvanced             = tree.ID(hostProtocolUiHits, "dialog-advanced")
	hitDialogForwardX11           = tree.ID(hostProtocolUiHits, "dialog-forward-x11")
	hitDialogAgentForward         = tree.ID(hostProtocolUiHits, "dialog-agent-forward")
	hitDialogInteractiveOnly      = tree.ID(hostProtocolUiHits, "dialog-interactive-only")
	hitDialogNoHistory            = tree.ID(hostProtocolUiHits, "dialog-no-history")
	hitDialogAgentAccess          = tree.ID(hostProtocolUiHits, "dialog-agent-access")
	hitDialogAgentAccessHelp      = tree.ID(hostProtocolUiHits, "dialog-agent-access-help")
	hitDialogOTP                  = tree.ID(hostProtocolUiHits, "dialog-otp")
	hitDialogOTPRemove            = tree.ID(hostProtocolUiHits, "dialog-otp-remove")
	hitDialogOTPScan              = tree.ID(hostProtocolUiHits, "dialog-otp-scan")
	hitDialogOTPPaste             = tree.ID(hostProtocolUiHits, "dialog-otp-paste")
	hitDialogPortsItemList        = tree.Sub(hostProtocolUiHits, tree.Name("dialog-ports-item")) // + index of a detected port
)

// maxListedPorts bounds the picker: a machine with more serial devices than
// this is not choosing from a list.
const maxListedPorts = 12

// The segmented choices of the host form. Each is a Choice of the form's own
// page, declared at the row its layout reserved.
var (
	hostProtocolChoices = [...]hostProtocol{protocolSSH, protocolTelnet, protocolSerial, protocolVNC, protocolRDP}
	hostFlowChoices     = [...]serial.Flow{serial.FlowNone, serial.FlowHardware, serial.FlowSoftware}
	hostAlgoChoices     = [...]algoMode{algoDefault, algoCompatible, algoCustom}
)

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

// declareHostProtocolChoice declares the protocol choice in the form's page at
// the slot the body reserved for it. It writes editor.protocol, so applyProtocol
// is what follows the switch.
func (state *State) declareHostProtocolChoice(c ui.C) ui.Box {
	editor := &state.editor
	editor.protocolBefore = editor.protocol
	choice := ui.Choice(c, &editor.protocol, hostProtocolChoices[:], state.hostProtocolChoiceLabel).Fill()
	if choice.Changed() {
		editor.applyProtocol(editor.protocolBefore)
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
// painter fills the slots this leaves with the dialog's fields.
func (state *State) declareHostEndpointSlots(c ui.C, parent ui.Box, rowHeight float32) {
	editor := &state.editor
	slots := &editor.form.body.endpointSlots
	*slots = hostEndpointSlots{}
	parent.Children(func() {
		switch editor.protocol {
		case protocolSerial:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				slots.picker = ui.Slot(c).WidthPercent(30)
			})
			// The flow control is a three-way choice: it takes the row under
			// the line settings rather than sharing one with them, where its
			// labels would be cut.
			slots.second = ui.Slot(c).Height(rowHeight)
			slots.third = ui.Slot(c).Height(rowHeight)
		case protocolTelnet:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
			})
		case protocolVNC:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.second = ui.Slot(c).Grow(1)
				slots.password = ui.Slot(c).Grow(1)
			})
		case protocolSSH:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.second = ui.Slot(c).Grow(1)
				slots.authentication = ui.Slot(c).Grow(1)
				if editor.auth == authPrivateKey {
					slots.identity = ui.Slot(c).Grow(1)
				} else if editor.auth != authAgent {
					slots.password = ui.Slot(c).Grow(1)
				}
			})
			if state.hostAsksKeyPassphrase() {
				slots.passphrase = ui.Slot(c).Height(rowHeight)
			}
		default:
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.address = ui.Slot(c).Grow(1)
				slots.third = ui.Slot(c).Width(hostPortWidth).Shrink(0)
			})
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				slots.second = ui.Slot(c).Grow(1)
				slots.password = ui.Slot(c).Grow(1)
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

// paintHostEndpointRows draws where the host is: its address and, beside it, the
// port it is reached on, and on the row below the credentials the protocol takes.
// A serial port takes the device, its line and, on its own row, the flow
// control; a Telnet host has no credentials in the form, so it takes one row.
// The slots were declared with the form's page; the switch reads the protocol now
// in effect, as the event pass of a protocol click still paints it.
func (state *State) paintHostEndpointRows(canvas *gfx.Canvas) {
	m, tools := state.messages, state.messages.Tools
	editor := &state.editor
	slots := &editor.form.body.endpointSlots
	address, picker, second := slots.address, slots.picker, slots.second
	password, third := slots.password, slots.third
	authentication, identity, passphrase := slots.authentication, slots.identity, slots.passphrase
	switch editor.protocol {
	case protocolSerial:
		state.paintDialogField(canvas, hitDialogAddress, fieldAddress, tools.FieldDevice, tools.FieldDevicePlaceholder, address.Rect())
		state.paintPortsTrigger(canvas, picker.Rect())
		state.paintDialogField(canvas, hitDialogPort, fieldPort, tools.FieldLine, tools.FieldLinePlaceholder, second.Rect())
	case protocolTelnet:
		state.paintDialogField(canvas, hitDialogAddress, fieldAddress, m.FieldAddress, m.FieldAddressPlaceholder, address.Rect())
		state.paintDialogField(canvas, hitDialogPort, fieldPort, m.FieldPort, "", third.Rect())
	case protocolRDP:
		state.paintDialogField(canvas, hitDialogAddress, fieldAddress, m.FieldAddress, m.FieldAddressPlaceholder, address.Rect())
		state.paintDialogField(canvas, hitDialogUser, fieldUser, state.messages.Desktop.FieldRDPUser, state.messages.Desktop.RDPUserHint, second.Rect())
		state.paintSecureDialogField(canvas, hitDialogPassword, fieldPassword, m.FieldPassword, state.hostPasswordHint(), password.Rect())
		state.paintDialogField(canvas, hitDialogPort, fieldPort, m.FieldPort, "", third.Rect())
	case protocolVNC:
		state.paintDialogField(canvas, hitDialogAddress, fieldAddress, m.FieldAddress, m.FieldAddressPlaceholder, address.Rect())
		if editor.viaSSH {
			state.paintDialogField(canvas, hitDialogUser, fieldUser, state.messages.Desktop.FieldSSHUser, m.FieldUserPlaceholder, second.Rect())
		} else {
			state.paintDialogField(canvas, hitDialogUser, fieldUser, state.messages.Desktop.FieldVNCUser, state.messages.Desktop.VNCUserHint, second.Rect())
		}
		state.paintSecureDialogField(canvas, hitDialogPassword, fieldPassword, m.FieldVNCPassword, state.hostPasswordHint(), password.Rect())
		state.paintDialogField(canvas, hitDialogPort, fieldPort, m.FieldPort, "", third.Rect())
	case protocolSSH:
		state.paintDialogField(canvas, hitDialogAddress, fieldAddress, m.FieldAddress, m.FieldAddressPlaceholder, address.Rect())
		state.paintDialogField(canvas, hitDialogUser, fieldUser, m.FieldUser, m.FieldUserPlaceholder, second.Rect())
		state.paintHostAuthenticationChoice(canvas, authentication.Rect())
		if editor.auth == authPrivateKey {
			state.paintHostIdentityChoice(canvas, identity.Rect())
			if state.hostAsksKeyPassphrase() {
				state.paintSecureDialogField(canvas, hitDialogPassphrase, fieldPassphrase, m.KeyPassphraseLabel, state.hostPassphraseHint(), passphrase.Rect())
			}
		} else if editor.auth != authAgent {
			state.paintSecureDialogField(canvas, hitDialogPassword, fieldPassword, m.FieldPassword, state.hostPasswordHint(), password.Rect())
		}
		state.paintDialogField(canvas, hitDialogPort, fieldPort, m.FieldPort, "", third.Rect())
	default:
		state.paintDialogField(canvas, hitDialogAddress, fieldAddress, m.FieldAddress, m.FieldAddressPlaceholder, address.Rect())
		state.paintDialogField(canvas, hitDialogUser, fieldUser, m.FieldUser, m.FieldUserPlaceholder, second.Rect())
		state.paintSecureDialogField(canvas, hitDialogPassword, fieldPassword, m.FieldPassword, state.hostPasswordHint(), password.Rect())
		state.paintDialogField(canvas, hitDialogPort, fieldPort, m.FieldPort, "", third.Rect())
	}
	state.paintHostCredentialMenu(canvas)
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

// paintPortsTrigger draws the picker of detected ports: a small select that lists
// the machine's serial ports and puts the chosen one in the device field.
func (state *State) paintPortsTrigger(canvas *gfx.Canvas, bounds gfx.Rect) {
	m, editor := state.messages.Tools, &state.editor
	content := components.BeginField(canvas, state.Theme(), bounds).Paint(m.PortsPicker, "", "")
	state.rememberHostControl(hitDialogPortsTrigger, content)
	portal := state.beginHostFormPopup(canvas, state.editor.portsOpen)
	defer state.endHostFormPopup(canvas, portal)
	count := min(len(editor.ports), maxListedPorts)
	popupWidth := max(content.Width, 340)
	selectControl := components.BeginSelect(canvas, state.Theme(), content, components.SelectOptions{
		TriggerID: hitDialogPortsTrigger, Label: m.PortsPicker, Placeholder: "…", Open: editor.portsOpen, Size: components.SelectSizeSmall,
		Content: gfx.Rect{X: content.X + content.Width - popupWidth, Y: content.Y + content.Height + 4, Width: popupWidth, Height: float32(8 + max(count, 1)*32)},
		Layer:   dialogLayer + 1, Scope: dialogScope, DismissID: hitDialogPortsDismiss, Viewport: state.windowRect(),
		Scroll: state.selectScroll(editor.portsOpen, hitDialogPortsDismiss),
	})
	defer selectControl.End()
	if editor.portsOpen {
		state.menuPlaced(selectControl.ContentBounds())
	}
	if !editor.portsOpen {
		return
	}
	if count == 0 {
		selectControl.AddItem(components.SelectItem{ID: hitDialogPortsItemList.At(0), Label: m.PortsNone, Disabled: true})
		return
	}
	for index := 0; index < count; index++ {
		port := editor.ports[index]
		selectControl.AddItem(components.SelectItem{
			ID: hitDialogPortsItemList.At(index), Label: port.Device, Value: port.Description,
			Selected: port.Device == editor.value(fieldAddress),
		})
	}
}

// clickHostProtocol handles the protocol controls of the host dialog. Its
// segmented choices are pages now; the clicks they deliver write their own value.
func (state *State) clickHostProtocol(id gfx.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogViaSSH:
		editor.viaSSH = !editor.viaSSH
	case id == hitDialogClipboard:
		editor.clipboard = !editor.clipboard
	case id == hitDialogChromeless:
		editor.chromeless = !editor.chromeless
	case id == hitDialogConsole:
		editor.console = !editor.console
	case id == hitDialogStandardSecurity:
		editor.standardSecurity = !editor.standardSecurity
	case id == hitDialogAdvanced:
		editor.advancedOpen = !editor.advancedOpen
	case id == hitDialogForwardX11:
		editor.forwardX11 = !editor.forwardX11
	case id == hitDialogAgentForward:
		editor.agentForward = !editor.agentForward
	case id == hitDialogInteractiveOnly:
		editor.interactiveOnly = !editor.interactiveOnly
	case id == hitDialogNoHistory:
		editor.noHistory = !editor.noHistory
	case id == hitDialogAgentAccess:
		editor.agentAccess = !editor.agentAccess
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
	case id == hitDialogPortsTrigger:
		editor.ports = serial.Ports()
		editor.portsOpen = !editor.portsOpen
	case id == hitDialogPortsDismiss:
		editor.portsOpen = false
	case id.Scope() == hitDialogPortsItemList:
		if index := item(id); index < len(editor.ports) {
			editor.address = text.Editor{}
			editor.address.Insert(editor.ports[index].Device)
		}
		editor.portsOpen = false
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
	return ok && identity.Encrypted
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
		// The jump route row sits under the proxy row.
		height += hostFormRowHeight
	}
	if state.dialogError != "" {
		height += 32
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
	// sshSwitchRowsHeight is both groups: the first row sits 4px under its
	// heading, the others 12px under the row above, and 16px are left at the end.
	sshSwitchRowsHeight = groupHeadingHeight + (4 + 20) + 3*(12+20) + groupHeadingHeight + (4 + 20) + 16
)

// advancedHeaderHeight is the row that opens the Advanced section of an SSH
// host, with the gap above it.
const advancedHeaderHeight = 48

// advancedHeaderGap is the space above that row, and the same space is left under
// it: 8px each, around its 32px.
const advancedHeaderGap = 8

// advancedSectionHeight is the room of an SSH host's Advanced section: its row
// alone while closed, and open the algorithm choice (a third row for a custom
// list), the one-time code secret with its buttons, and the switches.
func (state *State) advancedSectionHeight() float32 {
	if !state.editor.advancedOpen {
		return advancedHeaderHeight
	}
	rows := float32(1)
	if state.editor.algoMode == algoCustom {
		rows++
	}
	return advancedHeaderHeight + rows*hostFormRowHeight + hostFormRowHeight + otpFieldHeight + otpButtonsRow + sshSwitchRowsHeight
}

// hostDialogExtra is the height the protocol-specific rows add to the host dialog.
func (state *State) hostDialogExtra() float32 {
	if state.editor.protocol == protocolVNC || state.editor.protocol == protocolRDP {
		return state.desktopRowsHeight()
	}
	if state.editor.protocol != protocolSSH {
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
		state.paintAlgorithmRows(canvas)
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
	tunnel, clipboard, chromeless, console, standardSecurity, standardSecurityHelp ui.Box
}

// hostAlgoRowsBody is where the SSH algorithm rows put each control.
type hostAlgoRowsBody struct {
	algo, custom, login, otp, scan, paste, remove ui.Box
	x11, agentFwd, noHistory, interactive         ui.Box
	agentAccess, agentHelp                        ui.Box
}

// paintAlgorithmRows draws the SSH algorithm choice and, for a custom one, the
// list that is allowed, then the one-time code secret. The rows were declared
// with the host form's page; this paints into the slots it left.
func (state *State) paintAlgorithmRows(canvas *gfx.Canvas) {
	m, editor := state.messages.Tools, &state.editor
	// The choice may write a new mode while the page's declaration delivered the
	// click; the rows this frame laid out are the ones before it, so draw the
	// custom field only when its row was laid out.
	customMode := editor.form.algoLayout == algoCustom

	b := &editor.form.algoBody
	if customMode {
		state.paintDialogField(canvas, hitDialogAlgorithms, fieldAlgorithms, m.AlgoCustomField, m.AlgoCustomPlaceholder, b.custom.Rect())
	}
	state.paintDialogField(canvas, hitDialogLoginCommand, fieldLoginCommand, m.LoginCommandField, m.LoginCommandPlaceholder, b.login.Rect())
	var removeRect gfx.Rect
	if editor.otpSaved {
		removeRect = b.remove.Rect()
	}
	state.paintOTPRow(canvas, b.otp.Rect(), b.scan.Rect(), b.paste.Rect(), removeRect)
	state.paintDialogCheckbox(canvas, hitDialogForwardX11, m.X11Forward, editor.forwardX11, b.x11)
	state.paintDialogCheckbox(canvas, hitDialogAgentForward, m.AgentForward, editor.agentForward, b.agentFwd)
	state.paintDialogCheckbox(canvas, hitDialogNoHistory, state.messages.History.HostNoRecord, editor.noHistory, b.noHistory)
	state.paintDialogCheckbox(canvas, hitDialogInteractiveOnly, m.InteractiveOnly, editor.interactiveOnly, b.interactive)
	state.paintDialogCheckbox(canvas, hitDialogAgentAccess, m.AgentAccess, editor.agentAccess, b.agentAccess)
	// After the checkbox, whose words reach to the end of the row, so that this is
	// the hit on top where the two meet.
	state.paintHelpIcon(canvas, hitDialogAgentAccessHelp, m.AgentAccessNote, b.agentHelp.Rect())
}

// declareHostAlgoRows lays out the SSH algorithm choice, the custom list, the
// login command, the one-time code row and the session and agent switches.
func (state *State) declareHostAlgoRows(c ui.C) {
	m, editor, pal := state.messages.Tools, &state.editor, state.pal
	b := &editor.form.algoBody
	*b = hostAlgoRowsBody{}
	customMode := editor.form.algoLayout == algoCustom
	rowHeight := float32(hostFormRowHeight)
	ui.Column(c).FillWidth().Children(func() {
		b.algo = ui.Slot(c).Height(rowHeight)
		if customMode {
			b.custom = ui.Slot(c).Height(rowHeight)
		}
		b.login = ui.Slot(c).Height(rowHeight)
		ui.Row(c).Height(otpFieldHeight).Gap(8).Children(func() {
			b.otp = ui.Slot(c).Grow(1)
		})
		ui.Row(c).Height(otpButtonsRow).Gap(8).Children(func() {
			b.scan = ui.Slot(c).Size(algoScanWidth, 30).Self(ui.Start)
			b.paste = ui.Slot(c).Size(algoPasteWidth, 30).Self(ui.Start)
			b.remove = ui.Slot(c).Size(algoRemoveWidth, 30).Self(ui.Start)
		})
		heading := func(title string) {
			ui.Column(c).Height(groupHeadingHeight).Children(func() {
				ui.Column(c).Height(1).MarginSides(12, 0, 0, 0).Background(withAlpha(pal.line, 0.7))
				ui.Text(c, title).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).MarginSides(8, 0, 0, 2)
			})
		}
		heading(m.AdvancedSession)
		b.x11 = state.declareDialogCheckboxC(c, m.X11Forward, 4, 0)
		b.agentFwd = state.declareDialogCheckboxC(c, m.AgentForward, 12, 0)
		b.noHistory = state.declareDialogCheckboxC(c, state.messages.History.HostNoRecord, 12, 0)
		b.interactive = state.declareDialogCheckboxC(c, m.InteractiveOnly, 12, 0)
		heading(m.AdvancedAgents)
		b.agentAccess, b.agentHelp = state.declareDialogCheckboxHelpC(c, m.AgentAccess, 4, 0)
	})
}

// paintOTPRow draws the one-time code secret: a field for a new one, a button to
// read it from a QR code image, and for a
// saved one a button, in its own rectangle, to take it away. The secret itself
// is never drawn.
func (state *State) paintOTPRow(canvas *gfx.Canvas, field, scan, paste, remove gfx.Rect) {
	m, editor := state.messages.Tools, &state.editor
	placeholder := m.OTPPlaceholder
	if editor.otpSaved {
		placeholder = m.OTPSavedPlaceholder
		if editor.otpRemove {
			placeholder = m.OTPRemovingPlaceholder
		}
	}
	state.paintDialogField(canvas, hitDialogOTP, fieldOTP, m.OTPField, placeholder, field)
	components.BeginButton(canvas, state.Theme(), hitDialogOTPScan, scan).Small().Outline(m.OTPScan)
	state.rememberHostControl(hitDialogOTPScan, scan)
	components.BeginButton(canvas, state.Theme(), hitDialogOTPPaste, paste).Small().Outline(m.OTPPaste)
	state.rememberHostControl(hitDialogOTPPaste, paste)
	if editor.otpSaved {
		label := m.OTPRemove
		if editor.otpRemove {
			label = m.OTPKeep
		}
		components.BeginButton(canvas, state.Theme(), hitDialogOTPRemove, remove).Small().Outline(label)
		state.rememberHostControl(hitDialogOTPRemove, remove)
	}
}

// vncRowsHeight is the room of the three switches every remote desktop host has.
const vncRowsHeight = 4 + 20 + (12 + 20) + (12 + 20) + 16

// rdpRowsHeight adds the two switches only an RDP host has: the console
// session and Standard RDP Security.
const rdpRowsHeight = vncRowsHeight + 2*(12+20)

// desktopRowsHeight is the room paintVNCRows takes for the protocol on screen.
func (state *State) desktopRowsHeight() float32 {
	if state.editor.protocol == protocolRDP {
		return rdpRowsHeight
	}
	return vncRowsHeight
}

// paintVNCRows paints the switches of a VNC or RDP host in the slots the host
// form's page laid out: connecting through SSH, sharing the clipboard, showing
// the picture without the bars around it and, for RDP, asking Windows for its
// console (administrative) session and choosing Standard RDP Security.
func (state *State) paintVNCRows(canvas *gfx.Canvas) {
	editor, m := &state.editor, state.messages.Desktop
	b := &editor.form.vncBody
	state.paintDialogCheckbox(canvas, hitDialogViaSSH, m.ViaSSH, editor.viaSSH, b.tunnel)
	state.paintDialogCheckbox(canvas, hitDialogClipboard, m.ShareClipboard, editor.clipboard, b.clipboard)
	state.paintDialogCheckbox(canvas, hitDialogChromeless, m.HideBars, editor.chromeless, b.chromeless)
	// The rows this frame laid out are the ones before a protocol click wrote
	// its new value at the end of the declaration: painting the console row
	// needs the RDP rows on the page, not just RDP in effect.
	if editor.form.layoutProtocol == protocolRDP {
		state.paintDialogCheckbox(canvas, hitDialogConsole, m.ConsoleSession, editor.console, b.console)
		state.paintDialogCheckbox(canvas, hitDialogStandardSecurity, m.StandardSecurity, editor.standardSecurity, b.standardSecurity)
		// After the checkbox, whose words reach to the end of the row, so that
		// this is the hit on top where the two meet.
		state.paintHelpIcon(canvas, hitDialogStandardSecurityHelp, m.StandardSecurityNote, b.standardSecurityHelp.Rect())
	}
}

// declareHostVNCRows lays out the switches of a VNC or RDP host: connecting
// through SSH, sharing the clipboard, showing the picture without the bars
// around it and, for RDP, asking Windows for its console (administrative)
// session and choosing Standard RDP Security.
func (state *State) declareHostVNCRows(c ui.C) {
	editor, m := &state.editor, state.messages.Desktop
	b := &editor.form.vncBody
	*b = hostVNCRowsBody{}
	ui.Column(c).Fill().Children(func() {
		b.tunnel = state.declareDialogCheckboxC(c, m.ViaSSH, 4, 0)
		b.clipboard = state.declareDialogCheckboxC(c, m.ShareClipboard, 12, 0)
		b.chromeless = state.declareDialogCheckboxC(c, m.HideBars, 12, 0)
		if editor.protocol == protocolRDP {
			b.console = state.declareDialogCheckboxC(c, m.ConsoleSession, 12, 0)
			b.standardSecurity, b.standardSecurityHelp = state.declareDialogCheckboxHelpC(c, m.StandardSecurity, 12, 0)
		}
	})
}
