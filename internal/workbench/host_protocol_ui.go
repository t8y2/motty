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
	hitDialogViaSSH          = tree.ID(hostProtocolUiHits, "dialog-via-ssh")
	hitDialogClipboard       = tree.ID(hostProtocolUiHits, "dialog-clipboard")
	hitDialogConsole         = tree.ID(hostProtocolUiHits, "dialog-console")
	hitDialogChromeless      = tree.ID(hostProtocolUiHits, "dialog-chromeless")
	hitDialogPortsTrigger    = tree.ID(hostProtocolUiHits, "dialog-ports-trigger")
	hitDialogPortsDismiss    = tree.ID(hostProtocolUiHits, "dialog-ports-dismiss")
	hitDialogAlgorithms      = tree.ID(hostProtocolUiHits, "dialog-algorithms")
	hitDialogLoginCommand    = tree.ID(hostProtocolUiHits, "dialog-login-command")
	hitDialogAdvanced        = tree.ID(hostProtocolUiHits, "dialog-advanced")
	hitDialogForwardX11      = tree.ID(hostProtocolUiHits, "dialog-forward-x11")
	hitDialogAgentForward    = tree.ID(hostProtocolUiHits, "dialog-agent-forward")
	hitDialogInteractiveOnly = tree.ID(hostProtocolUiHits, "dialog-interactive-only")
	hitDialogNoHistory       = tree.ID(hostProtocolUiHits, "dialog-no-history")
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

// The segmented choices of the host form. Each is a page of its own: the form's
// layout has already placed every control at a rectangle, so a page declares one
// Choice there and keeps the roving focus the dialog's keys ask it for.
var (
	hostProtocolChoices = [...]hostProtocol{protocolSSH, protocolTelnet, protocolSerial, protocolVNC, protocolRDP}
	hostFlowChoices     = [...]serial.Flow{serial.FlowNone, serial.FlowHardware, serial.FlowSoftware}
	hostAlgoChoices     = [...]algoMode{algoDefault, algoCompatible, algoCustom}
)

// dialogChoice is one segmented choice of a dialog, declared as a page. chosen is
// the identity of the option the page painted last, kept so the form's tab ring
// can reach the choice even while its page lies outside the clipped viewport.
type dialogChoice struct {
	page   *ui.Page
	view   func(ui.C)
	chosen gfx.ID
}

// dialogChoiceKind names the declaration a dialog choice's page is drawn with.
type dialogChoiceKind uint8

const (
	hostProtocolChoice dialogChoiceKind = iota
	hostFlowChoice
	hostAlgoChoice
	hostTransferChoice
	xyModeChoice
	xyDirectionChoice
)

// dialogChoiceView is the page declaration of one choice. It is asked for once,
// when the page is made: a method value made in every paint allocates.
func (state *State) dialogChoiceView(kind dialogChoiceKind) func(ui.C) {
	switch kind {
	case hostProtocolChoice:
		return state.declareHostProtocolChoice
	case hostFlowChoice:
		return state.declareHostFlowChoice
	case hostAlgoChoice:
		return state.declareHostAlgoChoice
	case hostTransferChoice:
		return state.declareHostTransferChoice
	case xyModeChoice:
		return state.declareXYModeChoice
	case xyDirectionChoice:
		return state.declareXYDirectionChoice
	}
	return nil
}

// drawDialogChoice draws a dialog choice's page in bounds, making the page the
// first time the control paints.
func (state *State) drawDialogChoice(canvas *gfx.Canvas, bounds gfx.Rect, choice *dialogChoice, root string, kind dialogChoiceKind) {
	if choice.page == nil {
		choice.page = ui.NewPage(ui.PageConfig{Root: root, Theme: &state.theme})
		choice.view = state.dialogChoiceView(kind)
	}
	choice.page.Draw(canvas, bounds, choice.view)
}

// hostDialogChoiceKey gives the form's page choices their arrows before the
// form's own Tab order and scrolling take the key.
func (state *State) hostDialogChoiceKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	editor := &state.editor
	for _, choice := range [...]*ui.Page{editor.protocolChoice.page, editor.flowChoice.page, editor.algoChoice.page, editor.transferChoice.page} {
		if choice == nil {
			continue
		}
		if result, handled := choice.Key(event); handled {
			return result, true
		}
	}
	return gfx.EventResult{}, false
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

// declareHostProtocolChoice is the page of the protocol choice. The choice writes
// editor.protocol, so applyProtocol is what follows the switch.
func (state *State) declareHostProtocolChoice(c ui.C) {
	editor := &state.editor
	choice := ui.Choice(c, &editor.protocol, hostProtocolChoices[:], state.hostProtocolChoiceLabel).Fill()
	if choice.Changed() {
		editor.applyProtocol(editor.protocolBefore)
		state.dialogError = ""
	}
}

// declareHostFlowChoice is the page of the serial flow-control choice. The click
// that already wrote editor.flow only has the form's error to clear.
func (state *State) declareHostFlowChoice(c ui.C) {
	choice := ui.Choice(c, &state.editor.flow, hostFlowChoices[:], state.hostFlowChoiceLabel).Fill()
	if choice.Changed() {
		state.dialogError = ""
	}
}

// declareHostAlgoChoice is the page of the SSH algorithm choice.
func (state *State) declareHostAlgoChoice(c ui.C) {
	choice := ui.Choice(c, &state.editor.algoMode, hostAlgoChoices[:], state.hostAlgoChoiceLabel).Fill()
	if choice.Changed() {
		state.dialogError = ""
	}
}

func (state *State) paintHostProtocolSwitch(canvas *gfx.Canvas, bounds gfx.Rect) {
	editor := &state.editor
	editor.protocolBefore = editor.protocol
	state.drawDialogChoice(canvas, bounds, &editor.protocolChoice, "host-dialog-protocol", hostProtocolChoice)
	state.rememberHostChoice(canvas, bounds, &editor.protocolChoice, state.hostProtocolChoiceLabel(editor.protocol))
}

// paintHostEndpointRows draws where the host is: its address and, beside it, the
// port it is reached on, and on the row below the credentials the protocol takes.
// A serial port takes the device, its line and, on its own row, the flow
// control; a Telnet host has no credentials in the form, so it takes one row.
func (state *State) paintHostEndpointRows(canvas *gfx.Canvas, x, y, width, rowHeight float32) {
	m, tools := state.messages, state.messages.Tools
	editor := &state.editor
	var address, picker, second, password, third, authentication, identity, passphrase ui.Box
	f := state.partLayout.Begin(canvas, state.Theme(), gfx.Rect{X: x, Y: y, Width: width, Height: float32(state.hostEndpointRows()) * rowHeight})
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
				second = f.Slot().Grow(1)
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
		state.drawDialogChoice(canvas, content, &editor.flowChoice, "host-dialog-flow", hostFlowChoice)
		state.rememberHostChoice(canvas, content, &editor.flowChoice, state.hostFlowChoiceLabel(editor.flow))
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
	case id == hitDialogIcon:
		editor.iconOpen = !editor.iconOpen
		editor.iconReveal = editor.iconOpen
	case id == hitDialogIconDismiss:
		editor.iconOpen = false
	case id.Scope() == hitDialogIconItemList:
		if choice := hostIcon(item(id)); choice.valid() {
			editor.icon, editor.iconChosen = choice, true
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

// paintAdvancedSection draws the row that opens or closes an SSH host's
// less-used settings and, while open, the settings under it.
func (state *State) paintAdvancedSection(canvas *gfx.Canvas, bounds gfx.Rect) {
	pal, open := state.pal, state.editor.advancedOpen
	label := state.messages.Tools.AdvancedOptions
	chevron := iconChevronRight
	if open {
		chevron = iconChevronDown
	}
	var icon ui.Box
	header := gfx.Rect{X: bounds.X, Y: bounds.Y + advancedHeaderGap, Width: bounds.Width, Height: 32}
	state.rememberHostControl(hitDialogAdvanced, header)
	f := state.partLayout.Begin(canvas, state.Theme(), header)
	f.Row().Fill().PadSides(0, 0, 0, 2).Gap(8).Items(ui.Center).
		Radius(radius.control).Hover(ui.Look{}.Background(pal.surfaceSoft)).
		HitWith(hitDialogAdvanced, label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: open}).
		Children(func() {
			icon = f.Slot().Size(16, 16)
			f.Text(label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(20)
		})
	f.End()
	paintIcon(canvas, uint8(chevron), icon.Rect(), pal.muted)
	if open {
		state.paintAlgorithmRows(canvas, bounds.X, bounds.Y+advancedHeaderHeight, bounds.Width, hostFormRowHeight)
	}
}

// hostVNCRowsRoot and hostAlgoRowsRoot name the pages the desktop switches and
// the SSH algorithm rows are declared in.
const (
	hostVNCRowsRoot  = "host-vnc-rows"
	hostAlgoRowsRoot = "host-algo-rows"
	// The widths of the one-time code row's buttons.
	algoRemoveWidth, algoScanWidth, algoPasteWidth = 84, 132, 96
)

// hostVNCRowsBody is where the desktop switches' body put each checkbox row.
type hostVNCRowsBody struct {
	tunnel, clipboard, chromeless, console ui.Box
}

// hostAlgoRowsBody is where the SSH algorithm rows put each control.
type hostAlgoRowsBody struct {
	algo, custom, login, otp, scan, paste, remove ui.Box
	x11, agentFwd, noHistory, interactive         ui.Box
	agentAccess, agentHelp                        ui.Box
}

func (state *State) hostVNCRowsPage() *ui.Page {
	form := &state.editor.form
	if form.vncPage == nil {
		form.vncPage = ui.NewPage(ui.PageConfig{Root: hostVNCRowsRoot, Theme: &state.theme})
		form.vncView = state.declareHostVNCRows
	}
	return form.vncPage
}

func (state *State) hostAlgoRowsPage() *ui.Page {
	form := &state.editor.form
	if form.algoPage == nil {
		form.algoPage = ui.NewPage(ui.PageConfig{Root: hostAlgoRowsRoot, Theme: &state.theme})
		form.algoView = state.declareHostAlgoRows
	}
	return form.algoPage
}

// paintAlgorithmRows draws the SSH algorithm choice and, for a custom one, the
// list that is allowed, then the one-time code secret.
func (state *State) paintAlgorithmRows(canvas *gfx.Canvas, x, y, width, rowHeight float32) {
	m, editor := state.messages.Tools, &state.editor
	// The page's choice may write a new mode while it is declared; the rows this
	// page laid out are the ones before it, so draw the custom field only when
	// its row was laid out.
	customMode := editor.algoMode == algoCustom
	rows := float32(1)
	if customMode {
		rows++
	}
	b := &editor.form.algoBody
	state.hostAlgoRowsPage().Draw(canvas, gfx.Rect{X: x, Y: y, Width: width, Height: (rows+1)*rowHeight + otpFieldHeight + otpButtonsRow + sshSwitchRowsHeight}, editor.form.algoView)

	content := components.BeginField(canvas, state.Theme(), b.algo.Rect()).Paint(m.AlgoField, "", "")
	state.drawDialogChoice(canvas, content, &editor.algoChoice, "host-dialog-algorithm", hostAlgoChoice)
	state.rememberHostChoice(canvas, content, &editor.algoChoice, state.hostAlgoChoiceLabel(editor.algoMode))
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
	customMode := editor.algoMode == algoCustom
	rowHeight := float32(hostFormRowHeight)
	ui.Column(c).Fill().Children(func() {
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
// bounds' place: connecting through SSH, sharing the clipboard, showing the
// picture without the bars around it and, for RDP, asking Windows for its
// console (administrative) session.
func (state *State) paintVNCRows(canvas *gfx.Canvas, bounds gfx.Rect) {
	editor, m := &state.editor, state.messages.Desktop
	b := &editor.form.vncBody
	state.hostVNCRowsPage().Draw(canvas, bounds, editor.form.vncView)
	state.paintDialogCheckbox(canvas, hitDialogViaSSH, m.ViaSSH, editor.viaSSH, b.tunnel)
	state.paintDialogCheckbox(canvas, hitDialogClipboard, m.ShareClipboard, editor.clipboard, b.clipboard)
	state.paintDialogCheckbox(canvas, hitDialogChromeless, m.HideBars, editor.chromeless, b.chromeless)
	if editor.protocol == protocolRDP {
		state.paintDialogCheckbox(canvas, hitDialogConsole, m.ConsoleSession, editor.console, b.console)
	}
}

// declareHostVNCRows lays out the switches of a VNC or RDP host: connecting
// through SSH, sharing the clipboard, showing the picture without the bars
// around it and, for RDP, asking Windows for its console (administrative)
// session.
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
		}
	})
}
