package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/serial"
)

// The protocol part of the host dialog: which way the host is reached, and for
// a serial port the device, the line and the flow control.

// hostProtocolUiHits is the place of the controls this file paints.
var hostProtocolUiHits = tree.Feature("host-protocol-ui")

var (
	hitDialogProtocolSSH     = tree.ID(hostProtocolUiHits, "dialog-protocol-ssh")
	hitDialogProtocolTelnet  = tree.ID(hostProtocolUiHits, "dialog-protocol-telnet")
	hitDialogProtocolSerial  = tree.ID(hostProtocolUiHits, "dialog-protocol-serial")
	hitDialogProtocolVNC     = tree.ID(hostProtocolUiHits, "dialog-protocol-vnc")
	hitDialogProtocolRDP     = tree.ID(hostProtocolUiHits, "dialog-protocol-rdp")
	hitDialogViaSSH          = tree.ID(hostProtocolUiHits, "dialog-via-ssh")
	hitDialogClipboard       = tree.ID(hostProtocolUiHits, "dialog-clipboard")
	hitDialogConsole         = tree.ID(hostProtocolUiHits, "dialog-console")
	hitDialogFlowNone        = tree.ID(hostProtocolUiHits, "dialog-flow-none")
	hitDialogFlowHardware    = tree.ID(hostProtocolUiHits, "dialog-flow-hardware")
	hitDialogFlowSoftware    = tree.ID(hostProtocolUiHits, "dialog-flow-software")
	hitDialogPortsTrigger    = tree.ID(hostProtocolUiHits, "dialog-ports-trigger")
	hitDialogPortsDismiss    = tree.ID(hostProtocolUiHits, "dialog-ports-dismiss")
	hitDialogAlgoDefault     = tree.ID(hostProtocolUiHits, "dialog-algo-default")
	hitDialogAlgoCompatible  = tree.ID(hostProtocolUiHits, "dialog-algo-compatible")
	hitDialogAlgoCustom      = tree.ID(hostProtocolUiHits, "dialog-algo-custom")
	hitDialogAlgorithms      = tree.ID(hostProtocolUiHits, "dialog-algorithms")
	hitDialogAdvanced        = tree.ID(hostProtocolUiHits, "dialog-advanced")
	hitDialogForwardX11      = tree.ID(hostProtocolUiHits, "dialog-forward-x11")
	hitDialogAgentForward    = tree.ID(hostProtocolUiHits, "dialog-agent-forward")
	hitDialogNoHistory       = tree.ID(hostProtocolUiHits, "dialog-no-history")
	hitDialogInitScript      = tree.ID(hostProtocolUiHits, "dialog-init-script")
	hitDialogAgentAccess     = tree.ID(hostProtocolUiHits, "dialog-agent-access")
	hitDialogAgentAccessHelp = tree.ID(hostProtocolUiHits, "dialog-agent-access-help")
	hitDialogOTP             = tree.ID(hostProtocolUiHits, "dialog-otp")
	hitDialogOTPRemove       = tree.ID(hostProtocolUiHits, "dialog-otp-remove")
	hitDialogOTPScan         = tree.ID(hostProtocolUiHits, "dialog-otp-scan")
	hitDialogOTPPaste        = tree.ID(hostProtocolUiHits, "dialog-otp-paste")
	hitDialogPortsItemList   = tree.Sub(hostProtocolUiHits, tree.Name("dialog-ports-item")) // + index of a detected port
	hitDialogIconDismiss     = tree.ID(hostProtocolUiHits, "dialog-icon-dismiss")
	hitDialogIconItemList    = tree.Sub(hostProtocolUiHits, tree.Name("dialog-icon-item")) // + a hostIcon
)

// maxListedPorts bounds the picker: a machine with more serial devices than
// this is not choosing from a list.
const maxListedPorts = 12

func (state *State) paintHostProtocolSwitch(canvas *ui.Canvas, bounds ui.Rect) {
	m := state.messages.Tools
	options := [...]components.SegmentOption{
		{ID: hitDialogProtocolSSH, Label: m.ProtocolSSH},
		{ID: hitDialogProtocolTelnet, Label: m.ProtocolTelnet},
		{ID: hitDialogProtocolSerial, Label: m.ProtocolSerial},
		{ID: hitDialogProtocolVNC, Label: state.messages.Desktop.ProtocolVNC},
		{ID: hitDialogProtocolRDP, Label: state.messages.Desktop.ProtocolRDP},
	}
	selected := options[state.editor.protocol].ID
	state.rememberHostControl(selected, bounds)
	components.BeginSegmentedControl(canvas, state.Theme(), bounds, options[:], selected).Paint()
}

// paintHostEndpointRows draws where the host is: its address and, beside it, the
// port it is reached on, and on the row below the credentials the protocol takes.
// A serial port takes the device, its line and, on its own row, the flow
// control; a Telnet host has no credentials in the form, so it takes one row.
func (state *State) paintHostEndpointRows(canvas *ui.Canvas, x, y, width, rowHeight float32) {
	m, tools := state.messages, state.messages.Tools
	editor := &state.editor
	var address, picker, second, password, third, authentication, identity, passphrase flex.Box
	f := state.partLayout.Begin(canvas, state.Theme(), ui.Rect{X: x, Y: y, Width: width, Height: float32(state.hostEndpointRows()) * rowHeight})
	f.Column().Fill().Children(func() {
		switch editor.protocol {
		case protocolSerial:
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				address = f.Slot().Grow(1)
				picker = f.Slot().WidthPercent(30)
			})
			// The flow control is a three-way choice: it takes the row under
			// the line settings rather than sharing one with them, where its
			// labels would be cut.
			second = f.Slot().Height(rowHeight)
			third = f.Slot().Height(rowHeight)
		case protocolTelnet:
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				address = f.Slot().Grow(1)
				third = f.Slot().Width(hostPortWidth).Shrink(0)
			})
		case protocolVNC:
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				address = f.Slot().Grow(1)
				third = f.Slot().Width(hostPortWidth).Shrink(0)
			})
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				if editor.viaSSH {
					second = f.Slot().Grow(1)
				}
				password = f.Slot().Grow(1)
			})
		case protocolSSH:
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				address = f.Slot().Grow(1)
				third = f.Slot().Width(hostPortWidth).Shrink(0)
			})
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				second = f.Slot().Grow(1)
				authentication = f.Slot().Grow(1)
				if editor.auth == authPrivateKey {
					identity = f.Slot().Grow(1)
				} else if editor.auth != authAgent {
					password = f.Slot().Grow(1)
				}
			})
			if state.hostAsksKeyPassphrase() {
				passphrase = f.Slot().Height(rowHeight)
			}
		default:
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				address = f.Slot().Grow(1)
				third = f.Slot().Width(hostPortWidth).Shrink(0)
			})
			f.Row().Height(rowHeight).Gap(8).Children(func() {
				second = f.Slot().Grow(1)
				password = f.Slot().Grow(1)
			})
		}
	})
	f.End()
	switch editor.protocol {
	case protocolSerial:
		state.paintDialogField(canvas, hitDialogAddress, fieldAddress, tools.FieldDevice, tools.FieldDevicePlaceholder, address.Rect())
		state.paintPortsTrigger(canvas, picker.Rect())
		state.paintDialogField(canvas, hitDialogPort, fieldPort, tools.FieldLine, tools.FieldLinePlaceholder, second.Rect())
		content := components.BeginField(canvas, state.Theme(), third.Rect()).Paint(tools.FieldFlow, "", "")
		options := [...]components.SegmentOption{
			{ID: hitDialogFlowNone, Label: tools.FlowNone},
			{ID: hitDialogFlowHardware, Label: tools.FlowHardware},
			{ID: hitDialogFlowSoftware, Label: tools.FlowSoftware},
		}
		components.BeginSegmentedControl(canvas, state.Theme(), content, options[:], options[editor.flow].ID).Paint()
		state.rememberHostControl(options[editor.flow].ID, content)
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
func (state *State) paintPortsTrigger(canvas *ui.Canvas, bounds ui.Rect) {
	m, editor := state.messages.Tools, &state.editor
	content := components.BeginField(canvas, state.Theme(), bounds).Paint(m.PortsPicker, "", "")
	state.rememberHostControl(hitDialogPortsTrigger, content)
	portal := state.beginHostFormPopup(canvas, state.editor.portsOpen)
	defer state.endHostFormPopup(canvas, portal)
	count := min(len(editor.ports), maxListedPorts)
	popupWidth := max(content.Width, 340)
	selectControl := components.BeginSelect(canvas, state.Theme(), content, components.SelectOptions{
		TriggerID: hitDialogPortsTrigger, Label: m.PortsPicker, Placeholder: "…", Open: editor.portsOpen, Size: components.SelectSizeSmall,
		Content: ui.Rect{X: content.X + content.Width - popupWidth, Y: content.Y + content.Height + 4, Width: popupWidth, Height: float32(8 + max(count, 1)*32)},
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

// clickHostProtocol handles the protocol controls of the host dialog.
func (state *State) clickHostProtocol(id ui.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogProtocolSSH:
		editor.setProtocol(protocolSSH)
	case id == hitDialogProtocolTelnet:
		editor.setProtocol(protocolTelnet)
	case id == hitDialogProtocolSerial:
		editor.setProtocol(protocolSerial)
	case id == hitDialogProtocolVNC:
		editor.setProtocol(protocolVNC)
	case id == hitDialogProtocolRDP:
		editor.setProtocol(protocolRDP)
	case id == hitDialogViaSSH:
		editor.viaSSH = !editor.viaSSH
	case id == hitDialogClipboard:
		editor.clipboard = !editor.clipboard
	case id == hitDialogConsole:
		editor.console = !editor.console
	case id == hitDialogFlowNone:
		editor.flow = serial.FlowNone
	case id == hitDialogFlowHardware:
		editor.flow = serial.FlowHardware
	case id == hitDialogFlowSoftware:
		editor.flow = serial.FlowSoftware
	case id == hitDialogAlgoDefault:
		editor.algoMode = algoDefault
	case id == hitDialogAlgoCompatible:
		editor.algoMode = algoCompatible
	case id == hitDialogAlgoCustom:
		editor.algoMode = algoCustom
	case id == hitDialogAdvanced:
		editor.advancedOpen = !editor.advancedOpen
	case id == hitDialogForwardX11:
		editor.forwardX11 = !editor.forwardX11
	case id == hitDialogAgentForward:
		editor.agentForward = !editor.agentForward
	case id == hitDialogNoHistory:
		editor.noHistory = !editor.noHistory
	case id == hitDialogAgentAccess:
		editor.agentAccess = !editor.agentAccess
	case state.clickHostAuthentication(id):
	case state.clickTransferChoice(id):
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
	case id == hitDialogIcon:
		editor.iconOpen = !editor.iconOpen
		editor.iconReveal = editor.iconOpen
	case id == hitDialogIconDismiss:
		editor.iconOpen = false
	case id.Scope() == hitDialogIconItemList:
		if choice := hostIcon(item(id)); choice.valid() {
			editor.icon = choice
		}
		editor.iconOpen = false
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
func (state *State) hostFormHeight() float32 {
	height := float32(2+44+(3+state.hostEndpointRows())*hostFormRowHeight) + state.hostDialogExtra()
	if state.editor.protocol != protocolSerial {
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
	// initScriptFieldHeight is the label and input of an SSH host's init script.
	initScriptFieldHeight = 58
	// sshSwitchRowsHeight is both groups: the first row sits 4px under its
	// heading, the others 12px under the row above, the init script's field
	// follows, and 16px are left at the end.
	sshSwitchRowsHeight = groupHeadingHeight + (4 + 20) + 2*(12+20) + groupHeadingHeight + (4 + 20) + 12 + initScriptFieldHeight + 16
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
	return advancedHeaderHeight + rows*hostFormRowHeight + otpFieldHeight + otpButtonsRow + sshSwitchRowsHeight
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

// paintAdvancedSection draws the row that opens or closes an SSH host's
// less-used settings and, while open, the settings under it.
func (state *State) paintAdvancedSection(canvas *ui.Canvas, bounds ui.Rect) {
	pal, open := state.pal, state.editor.advancedOpen
	label := state.messages.Tools.AdvancedOptions
	chevron := iconChevronRight
	if open {
		chevron = iconChevronDown
	}
	var icon flex.Box
	header := ui.Rect{X: bounds.X, Y: bounds.Y + advancedHeaderGap, Width: bounds.Width, Height: 32}
	state.rememberHostControl(hitDialogAdvanced, header)
	f := state.partLayout.Begin(canvas, state.Theme(), header)
	f.Row().Fill().PadSides(0, 0, 0, 2).Gap(8).Items(flex.Center).
		Radius(radius.control).HoverBackground(pal.surfaceSoft).
		HitWith(hitDialogAdvanced, label, ui.HitOptions{Role: ui.RoleButton, Expanded: open}).
		Children(func() {
			icon = f.Slot().Size(16, 16)
			f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(20)
		})
	f.End()
	paintIcon(canvas, uint8(chevron), icon.Rect(), pal.muted)
	if open {
		state.paintAlgorithmRows(canvas, bounds.X, bounds.Y+advancedHeaderHeight, bounds.Width, hostFormRowHeight)
	}
}

// paintAlgorithmRows draws the SSH algorithm choice and, for a custom one, the
// list that is allowed, then the one-time code secret.
func (state *State) paintAlgorithmRows(canvas *ui.Canvas, x, y, width, rowHeight float32) {
	m, editor, pal := state.messages.Tools, &state.editor, state.pal
	rows := float32(1)
	if editor.algoMode == algoCustom {
		rows++
	}
	const removeWidth, scanWidth, pasteWidth = 84, 132, 96
	var algo, custom, otp, scan, paste, remove, x11, agentFwd, noHistory, initScript, agentAccess, agentHelp flex.Box
	f := state.partLayout.Begin(canvas, state.Theme(), ui.Rect{X: x, Y: y, Width: width, Height: rows*rowHeight + otpFieldHeight + otpButtonsRow + sshSwitchRowsHeight})
	heading := func(title string) {
		f.Column().Height(groupHeadingHeight).Children(func() {
			f.Column().Height(1).MarginSides(12, 0, 0, 0).Background(withAlpha(pal.line, 0.7))
			f.Text(title).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).MarginSides(8, 0, 0, 2)
		})
	}
	f.Column().Fill().Children(func() {
		algo = f.Slot().Height(rowHeight)
		if editor.algoMode == algoCustom {
			custom = f.Slot().Height(rowHeight)
		}
		f.Row().Height(otpFieldHeight).Gap(8).Children(func() {
			otp = f.Slot().Grow(1)
		})
		f.Row().Height(otpButtonsRow).Gap(8).Children(func() {
			scan = f.Slot().Size(scanWidth, 30).Self(flex.Start)
			paste = f.Slot().Size(pasteWidth, 30).Self(flex.Start)
			remove = f.Slot().Size(removeWidth, 30).Self(flex.Start)
		})
		heading(m.AdvancedSession)
		x11 = state.declareDialogCheckbox(f, m.X11Forward, 4, 0)
		agentFwd = state.declareDialogCheckbox(f, m.AgentForward, 12, 0)
		noHistory = state.declareDialogCheckbox(f, state.messages.History.HostNoRecord, 12, 0)
		initScript = f.Slot().Height(initScriptFieldHeight)
		heading(m.AdvancedAgents)
		agentAccess, agentHelp = state.declareDialogCheckboxHelp(f, m.AgentAccess, 4, 0)
	})
	f.End()

	content := components.BeginField(canvas, state.Theme(), algo.Rect()).Paint(m.AlgoField, "", "")
	options := [...]components.SegmentOption{
		{ID: hitDialogAlgoDefault, Label: m.AlgoDefault},
		{ID: hitDialogAlgoCompatible, Label: m.AlgoCompatible},
		{ID: hitDialogAlgoCustom, Label: m.AlgoCustom},
	}
	components.BeginSegmentedControl(canvas, state.Theme(), content, options[:], options[editor.algoMode].ID).Paint()
	state.rememberHostControl(options[editor.algoMode].ID, content)
	if editor.algoMode == algoCustom {
		state.paintDialogField(canvas, hitDialogAlgorithms, fieldAlgorithms, m.AlgoCustomField, m.AlgoCustomPlaceholder, custom.Rect())
	}
	var removeRect ui.Rect
	if editor.otpSaved {
		removeRect = remove.Rect()
	}
	state.paintOTPRow(canvas, otp.Rect(), scan.Rect(), paste.Rect(), removeRect)
	state.paintDialogCheckbox(canvas, hitDialogForwardX11, m.X11Forward, editor.forwardX11, x11)
	state.paintDialogCheckbox(canvas, hitDialogAgentForward, m.AgentForward, editor.agentForward, agentFwd)
	state.paintDialogCheckbox(canvas, hitDialogNoHistory, state.messages.History.HostNoRecord, editor.noHistory, noHistory)
	state.paintDialogField(canvas, hitDialogInitScript, fieldInitScript, m.InitScriptField, m.InitScriptPlaceholder, initScript.Rect())
	state.paintDialogCheckbox(canvas, hitDialogAgentAccess, m.AgentAccess, editor.agentAccess, agentAccess)
	// After the checkbox, whose words reach to the end of the row, so that this is
	// the hit on top where the two meet.
	state.paintHelpIcon(canvas, hitDialogAgentAccessHelp, m.AgentAccessNote, agentHelp.Rect())
}

// paintOTPRow draws the one-time code secret: a field for a new one, a button to
// read it from a QR code image, and for a
// saved one a button, in its own rectangle, to take it away. The secret itself
// is never drawn.
func (state *State) paintOTPRow(canvas *ui.Canvas, field, scan, paste, remove ui.Rect) {
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

// vncRowsHeight is the room of the two switches every remote desktop host has.
const vncRowsHeight = 4 + 20 + (12 + 20) + 16

// rdpRowsHeight adds the console session's switch, which only an RDP host has.
const rdpRowsHeight = vncRowsHeight + (12 + 20)

// desktopRowsHeight is the room paintVNCRows takes for the protocol on screen.
func (state *State) desktopRowsHeight() float32 {
	if state.editor.protocol == protocolRDP {
		return rdpRowsHeight
	}
	return vncRowsHeight
}

// paintVNCRows declares the switches of a VNC or RDP host and paints them in
// bounds' place: connecting through SSH, sharing the clipboard and, for RDP,
// asking Windows for its console (administrative) session.
func (state *State) paintVNCRows(canvas *ui.Canvas, bounds ui.Rect) {
	editor, m := &state.editor, state.messages.Desktop
	var tunnel, clipboard, console flex.Box
	f := state.partLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Children(func() {
		tunnel = state.declareDialogCheckbox(f, m.ViaSSH, 4, 0)
		clipboard = state.declareDialogCheckbox(f, m.ShareClipboard, 12, 0)
		if editor.protocol == protocolRDP {
			console = state.declareDialogCheckbox(f, m.ConsoleSession, 12, 0)
		}
	})
	f.End()
	state.paintDialogCheckbox(canvas, hitDialogViaSSH, m.ViaSSH, editor.viaSSH, tunnel)
	state.paintDialogCheckbox(canvas, hitDialogClipboard, m.ShareClipboard, editor.clipboard, clipboard)
	if editor.protocol == protocolRDP {
		state.paintDialogCheckbox(canvas, hitDialogConsole, m.ConsoleSession, editor.console, console)
	}
}
