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
	hitDialogProtocolSSH    = tree.ID(hostProtocolUiHits, "dialog-protocol-ssh")
	hitDialogProtocolTelnet = tree.ID(hostProtocolUiHits, "dialog-protocol-telnet")
	hitDialogProtocolSerial = tree.ID(hostProtocolUiHits, "dialog-protocol-serial")
	hitDialogProtocolVNC    = tree.ID(hostProtocolUiHits, "dialog-protocol-vnc")
	hitDialogProtocolRDP    = tree.ID(hostProtocolUiHits, "dialog-protocol-rdp")
	hitDialogViaSSH         = tree.ID(hostProtocolUiHits, "dialog-via-ssh")
	hitDialogClipboard      = tree.ID(hostProtocolUiHits, "dialog-clipboard")
	hitDialogFlowNone       = tree.ID(hostProtocolUiHits, "dialog-flow-none")
	hitDialogFlowHardware   = tree.ID(hostProtocolUiHits, "dialog-flow-hardware")
	hitDialogFlowSoftware   = tree.ID(hostProtocolUiHits, "dialog-flow-software")
	hitDialogPortsTrigger   = tree.ID(hostProtocolUiHits, "dialog-ports-trigger")
	hitDialogPortsDismiss   = tree.ID(hostProtocolUiHits, "dialog-ports-dismiss")
	hitDialogAlgoDefault    = tree.ID(hostProtocolUiHits, "dialog-algo-default")
	hitDialogAlgoCompatible = tree.ID(hostProtocolUiHits, "dialog-algo-compatible")
	hitDialogAlgoCustom     = tree.ID(hostProtocolUiHits, "dialog-algo-custom")
	hitDialogAlgorithms     = tree.ID(hostProtocolUiHits, "dialog-algorithms")
	hitDialogAdvanced       = tree.ID(hostProtocolUiHits, "dialog-advanced")
	hitDialogForwardX11     = tree.ID(hostProtocolUiHits, "dialog-forward-x11")
	hitDialogAgentForward   = tree.ID(hostProtocolUiHits, "dialog-agent-forward")
	hitDialogNoHistory      = tree.ID(hostProtocolUiHits, "dialog-no-history")
	hitDialogOTP            = tree.ID(hostProtocolUiHits, "dialog-otp")
	hitDialogOTPRemove      = tree.ID(hostProtocolUiHits, "dialog-otp-remove")
	hitDialogOTPScan        = tree.ID(hostProtocolUiHits, "dialog-otp-scan")
	hitDialogOTPPaste       = tree.ID(hostProtocolUiHits, "dialog-otp-paste")
	hitDialogPortsItemList  = tree.Sub(hostProtocolUiHits, tree.Name("dialog-ports-item")) // + index of a detected port
	hitDialogIconDismiss    = tree.ID(hostProtocolUiHits, "dialog-icon-dismiss")
	hitDialogIconItemList   = tree.Sub(hostProtocolUiHits, tree.Name("dialog-icon-item")) // + a hostIcon
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
	components.BeginSegmentedControl(canvas, state.Theme(), bounds, options[:], selected).Paint()
}

// paintHostEndpointRows draws where the host is: its address and, beside it, the
// port it is reached on, and on the row below the credentials the protocol takes.
// A serial port takes the device, its line and, on its own row, the flow
// control; a Telnet host has no credentials in the form, so it takes one row.
func (state *State) paintHostEndpointRows(canvas *ui.Canvas, x, y, width, rowHeight float32) {
	m, tools := state.messages, state.messages.Tools
	editor := &state.editor
	var address, picker, second, password, third flex.Box
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
	case state.clickTransferChoice(id):
	case state.clickHostColor(id):
	case id == hitDialogOTPRemove:
		if !editor.otpChecked {
			state.checkEditedHostOTP()
			return true
		}
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
const hostFormRowHeight = 58

// hostPortWidth is the width of the port field: the label and five digits fit,
// and the address keeps the rest of the row.
const hostPortWidth = 84

// hostDialogBaseHeight is the content height of the host dialog for its base
// form, whose endpoint block takes two rows, before the protocol's own rows.
const hostDialogBaseHeight = 472

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
	}
	return 2
}

// hostDialogHeight is the content height of the host dialog: the base form, less
// the endpoint rows the protocol does not need, plus the rows it adds.
func (state *State) hostDialogHeight() float32 {
	return hostDialogBaseHeight - float32(2-state.hostEndpointRows())*hostFormRowHeight + state.hostDialogExtra()
}

// switchRowsHeight is the room of the three switches under the algorithm
// rows: "Forward X11", "Forward ssh-agent" and "Don't record commands". A
// switch's hit region is 32px high, so consecutive rows keep that distance
// for their hit regions not to overlap.
const switchRowsHeight = 96

// advancedHeaderHeight is the row that opens the Advanced section of an SSH
// host, with the gap above it.
const advancedHeaderHeight = 40

// advancedSectionHeight is the room of an SSH host's Advanced section: its row
// alone while closed, and open the algorithm choice (a third row for a custom
// list), the one-time code secret with its buttons, and the switches.
func (state *State) advancedSectionHeight() float32 {
	if !state.editor.advancedOpen {
		return advancedHeaderHeight
	}
	rows := float32(2)
	if state.editor.algoMode == algoCustom {
		rows++
	}
	return advancedHeaderHeight + rows*hostFormRowHeight + otpButtonsRow + switchRowsHeight
}

// hostDialogExtra is the height the protocol-specific rows add to the host dialog.
func (state *State) hostDialogExtra() float32 {
	if state.editor.protocol == protocolVNC || state.editor.protocol == protocolRDP {
		return vncRowsHeight
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
	header := ui.Rect{X: bounds.X, Y: bounds.Y + advancedHeaderHeight - 32, Width: bounds.Width, Height: 32}
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
	m, editor := state.messages.Tools, &state.editor
	rows := float32(2)
	if editor.algoMode == algoCustom {
		rows++
	}
	const removeWidth, scanWidth, pasteWidth = 84, 132, 96
	var algo, custom, otp, scan, paste, remove, x11, agentFwd, noHistory flex.Box
	f := state.partLayout.Begin(canvas, state.Theme(), ui.Rect{X: x, Y: y, Width: width, Height: rows*rowHeight + switchRowsHeight + otpButtonsRow})
	f.Column().Fill().Children(func() {
		algo = f.Slot().Height(rowHeight)
		if editor.algoMode == algoCustom {
			custom = f.Slot().Height(rowHeight)
		}
		f.Row().Height(rowHeight).Gap(8).Children(func() {
			otp = f.Slot().Grow(1)
		})
		f.Row().Height(otpButtonsRow).Gap(8).Children(func() {
			scan = f.Slot().Size(scanWidth, 30).Self(flex.Start)
			paste = f.Slot().Size(pasteWidth, 30).Self(flex.Start)
			remove = f.Slot().Size(removeWidth, 30).Self(flex.Start)
		})
		x11 = state.declareDialogCheckbox(f, m.X11Forward, 4, 0)
		agentFwd = state.declareDialogCheckbox(f, m.AgentForward, 4, 0)
		noHistory = state.declareDialogCheckbox(f, state.messages.History.HostNoRecord, 12, 0)
	})
	f.End()

	content := components.BeginField(canvas, state.Theme(), algo.Rect()).Paint(m.AlgoField, "", "")
	options := [...]components.SegmentOption{
		{ID: hitDialogAlgoDefault, Label: m.AlgoDefault},
		{ID: hitDialogAlgoCompatible, Label: m.AlgoCompatible},
		{ID: hitDialogAlgoCustom, Label: m.AlgoCustom},
	}
	components.BeginSegmentedControl(canvas, state.Theme(), content, options[:], options[editor.algoMode].ID).Paint()
	if editor.algoMode == algoCustom {
		state.paintDialogField(canvas, hitDialogAlgorithms, fieldAlgorithms, m.AlgoCustomField, m.AlgoCustomPlaceholder, custom.Rect())
	}
	var removeRect ui.Rect
	if editor.otpSaved || !editor.otpChecked {
		removeRect = remove.Rect()
	}
	state.paintOTPRow(canvas, otp.Rect(), scan.Rect(), paste.Rect(), removeRect)
	state.paintDialogCheckbox(canvas, hitDialogForwardX11, m.X11Forward, editor.forwardX11, x11)
	state.paintDialogCheckbox(canvas, hitDialogAgentForward, m.AgentForward, editor.agentForward, agentFwd)
	state.paintDialogCheckbox(canvas, hitDialogNoHistory, state.messages.History.HostNoRecord, editor.noHistory, noHistory)
}

// paintOTPRow draws the one-time code secret: a field for a new one, a button to
// read it from a QR code image, and for a
// saved one a button, in its own rectangle, to take it away. The secret itself
// is never drawn.
func (state *State) paintOTPRow(canvas *ui.Canvas, field, scan, paste, remove ui.Rect) {
	m, editor := state.messages.Tools, &state.editor
	placeholder := m.OTPPlaceholder
	if !editor.otpChecked {
		placeholder = m.OTPUncheckedPlaceholder
	} else if editor.otpSaved {
		placeholder = m.OTPSavedPlaceholder
		if editor.otpRemove {
			placeholder = m.OTPRemovingPlaceholder
		}
	}
	state.paintDialogField(canvas, hitDialogOTP, fieldOTP, m.OTPField, placeholder, field)
	components.BeginButton(canvas, state.Theme(), hitDialogOTPScan, scan).Small().Outline(m.OTPScan)
	components.BeginButton(canvas, state.Theme(), hitDialogOTPPaste, paste).Small().Outline(m.OTPPaste)
	if !editor.otpChecked || editor.otpSaved {
		label := m.OTPCheck
		if editor.otpChecked {
			label = m.OTPRemove
		}
		if editor.otpRemove {
			label = m.OTPKeep
		}
		components.BeginButton(canvas, state.Theme(), hitDialogOTPRemove, remove).Small().Outline(label)
	}
}

// vncRowsHeight is the room of the two switches of a remote desktop host.
const vncRowsHeight = switchRowsHeight

// paintVNCRows declares the two switches of a VNC or RDP host and paints them in
// bounds' place: connecting through SSH and sharing the clipboard.
func (state *State) paintVNCRows(canvas *ui.Canvas, bounds ui.Rect) {
	editor, m := &state.editor, state.messages.Desktop
	var tunnel, clipboard flex.Box
	f := state.partLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Children(func() {
		tunnel = state.declareDialogCheckbox(f, m.ViaSSH, 4, 0)
		clipboard = state.declareDialogCheckbox(f, m.ShareClipboard, 12, 0)
	})
	f.End()
	state.paintDialogCheckbox(canvas, hitDialogViaSSH, m.ViaSSH, editor.viaSSH, tunnel)
	state.paintDialogCheckbox(canvas, hitDialogClipboard, m.ShareClipboard, editor.clipboard, clipboard)
}
