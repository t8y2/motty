package workbench

import (
	"errors"
	"fmt"
	"motty/internal/desktop"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/charset"
	"motty/internal/i18n"
	"motty/internal/otp"
	"motty/internal/serial"
	"motty/internal/sshconfig"
	"motty/internal/sshconn"
)

// hostEditor holds the New Host dialog state: one text editor per field.
// Keyboard focus belongs to the runtime; the dialog never tracks it.
type hostEditor struct {
	label   text.Editor
	address text.Editor
	user    text.Editor
	// password is the value shown in this dialog: what was typed, or the saved one
	// read back from the vault. It is never copied into Host; a successful submit
	// stores it separately.
	password text.Editor
	// passphrase unlocks the private key the host is pinned to; it is kept in
	// the vault under the key, never in the host.
	passphrase text.Editor
	// passphraseFor is the key whose saved passphrase the form last looked up.
	passphraseFor string
	// passwordSaved says the password field was filled from the vault when the
	// dialog opened, so that emptying it means taking the password away.
	passwordSaved bool
	// keyLostError is the dialog error a failed save put up because the vault's key
	// is gone. The recovery button shows only while that error is still the one
	// on screen, so a later, unrelated error does not keep offering it.
	keyLostError string
	group        text.Editor
	groupPicker  hostGroupPicker
	tags         text.Editor
	tagPicker    hostTagPicker
	// encoding is the remote charset the host's shells use: empty is UTF-8.
	encoding string
	icon     hostIcon
	color    text.Editor
	form     hostFormScroll
	// system is the system the host was last detected to run, whose badge the
	// automatic icon shows.
	system sshconn.RemoteSystem
	// iconChosen says the user picked an icon, or the host being edited already
	// has one, so a protocol switch must not replace it with that protocol's
	// default.
	iconChosen bool
	// port is the port of an SSH or Telnet host and the line settings of a serial
	// one ("115200 8N1").
	port text.Editor

	// algoMode is the SSH algorithm choice; algorithms holds the custom list.
	algoMode     algoMode
	auth         hostAuth
	identity     string
	sshOptions   *sshconfig.ConnectionOptions
	authOpen     bool
	identityOpen bool
	// identitiesOnly is the automatic-authentication switch that offers the
	// ssh-agent only keys matching the configured identities.
	identitiesOnly bool
	// credential is the saved password the host takes its password from, and
	// credentialOpen its list being open.
	credential     string
	credentialOpen bool
	// credentialAnchor is where the password field was painted, which the list
	// of shared passwords opens under.
	credentialAnchor gfx.Rect
	transfer         transferMode
	algorithms       text.Editor
	// loginCommand is the line typed into the host's shells after login.
	loginCommand text.Editor
	// idleKeepAlive is the Advanced section's inactivity probe: the chosen
	// interval, its custom value and unit, and what the probe sends.
	idleKeepAlive   idleKeepAlivePreset
	idleInterval    text.Editor
	idleUnit        idleIntervalUnit
	idleInput       idleInputMode
	idleInputChosen bool
	idleInputCustom text.Editor
	// The three lists the probe's selects open, and the interval field.
	idleKeepAliveOpen, idleUnitOpen, idleInputOpen bool
	remoteLogPaths                                 []remoteLogPath
	// forwardX11 is the host's "Forward X11" switch.
	forwardX11 bool
	// agentForward is the host's "Forward ssh-agent" switch.
	agentForward bool
	// interactiveOnly is the host's "Interactive session only" switch: it keeps
	// the connection to the pty and shell a gateway permits.
	interactiveOnly bool
	// agentAccess is the "Allow MCP agents to use this host" switch. It is not
	// part of the host: it is kept for this computer (see savedState.AgentHosts).
	agentAccess        bool
	agentAccessChanged bool
	// noHistory is the host's "Don't record commands" switch.
	noHistory          bool
	loginFailurePolicy loginFailurePolicy
	// proxy is nil to inherit the global proxy, ProxyNone to force a direct
	// connection, or a dedicated proxy. proxyProfile names a saved proxy profile
	// instead, with proxyBypass as this host's own direct list. proxyPassword is
	// staged until the outer host form is saved.
	proxy         *outboundProxy
	proxyProfile  string
	proxyBypass   string
	proxyPassword string
	// otp is a new one-time code secret to save with the host. A saved one is
	// never shown again; otpSaved only says there is one.
	otp                 text.Editor
	otpSaved, otpRemove bool
	// advancedOpen is the SSH form's Advanced section being open: the algorithm
	// choice, the one-time code secret and the two switches. It opens by itself
	// for a host that sets any of them and for a value that fails validation.
	advancedOpen bool

	wslDistro                         string
	wslUseUser, wslNoGUI              bool
	wslUser, wslDirectory, wslCommand text.Editor

	protocol hostProtocol
	// terminal is an SSH host's terminal transport: SSH itself or mosh.
	terminal hostTerminal
	// connection is the form's connection-type choice (SSH, Mosh, Telnet, ...).
	// connectionBefore is the type the frame was laid out for: the choice writes
	// the new one, and applyConnection needs what the form showed before it.
	connection       hostConnectionType
	connectionBefore hostConnectionType
	flow             serial.Flow
	// backspace is the byte the Backspace key of a host that asks for it sends.
	backspace backspaceKey
	// viaSSH, clipboard and chromeless are the switches of a VNC or RDP host;
	// console, noAudio, standardSecurity and legacyTLS are RDP's own: the
	// server's console (administrative) session, muting remote sound, Standard
	// RDP Security without TLS or NLA, and the RSA key exchange TLS suites an
	// old server needs.
	viaSSH, clipboard, console, noAudio, standardSecurity, legacyTLS, chromeless bool
	// colorDepth, desktopEncoding and effects are a desktop host's quality
	// choices: the bits per pixel (zero is 32), the VNC encoding listed first,
	// and the visual effects an RDP server may render.
	colorDepth      int
	desktopEncoding desktop.Encoding
	effects         desktop.Effects
	// resolution is the remote resolution choice of an RDP host, and
	// resolutionCustom the size typed when the choice is a custom one.
	resolution       desktopSize
	resolutionCustom text.Editor
	// ports are the serial ports found when the picker was last opened.
	ports []serial.Port
}

type hostField uint8

const (
	fieldLabel hostField = iota
	fieldAddress
	fieldUser
	fieldPassword
	fieldGroup
	fieldPort
	fieldTags
	fieldColor
	fieldAlgorithms
	fieldOTP
	fieldPassphrase
	fieldLoginCommand
	fieldIdleInterval
	fieldIdleInputCustom
	fieldResolution
	fieldCount
)

// field returns the editor backing one form field.
func (editor *hostEditor) field(field hostField) *text.Editor {
	switch field {
	case fieldLabel:
		return &editor.label
	case fieldAddress:
		return &editor.address
	case fieldUser:
		return &editor.user
	case fieldPassword:
		return &editor.password
	case fieldGroup:
		return &editor.group
	case fieldTags:
		return &editor.tags
	case fieldColor:
		return &editor.color
	case fieldAlgorithms:
		return &editor.algorithms
	case fieldOTP:
		return &editor.otp
	case fieldPassphrase:
		return &editor.passphrase
	case fieldLoginCommand:
		return &editor.loginCommand
	case fieldIdleInterval:
		return &editor.idleInterval
	case fieldResolution:
		return &editor.resolutionCustom
	case fieldIdleInputCustom:
		return &editor.idleInputCustom
	default:
		return &editor.port
	}
}

// value is a field's text without surrounding blanks; pasted addresses often
// carry them.
func (editor *hostEditor) value(field hostField) string {
	raw := editor.field(field).String()
	if field == fieldLoginCommand {
		return normalizeLoginCommand(raw)
	}
	return strings.TrimSpace(raw)
}

// moshTerminal reports whether the form describes a mosh session, whose
// settings are narrower than an SSH session's.
func (editor *hostEditor) moshTerminal() bool {
	return editor.protocol == protocolSSH && editor.terminal == terminalMosh
}

// portNumber returns the port the form describes and whether it is valid. An empty
// field means the default.
func (editor *hostEditor) portNumber() (uint16, bool) {
	value := editor.value(fieldPort)
	if value == "" {
		return Host{Protocol: editor.protocol}.defaultPort(), true
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return uint16(port), true
}

// validate returns the first user-visible error, or empty when the form is
// submittable. A host is a duplicate only when it is the same destination under
// the same name in the same group (see sameHost and sameHostGroup): the same
// machine under another account, or reached through a bastion under another name,
// is a different host, and so is the same server filed in a second group.
func (editor *hostEditor) validate(hosts []Host, ignoreID uint32, messages i18n.Messages) string {
	if editor.value(fieldLabel) == "" {
		return messages.ErrLabelRequired
	}
	if editor.protocol != protocolWSL && editor.value(fieldAddress) == "" {
		if editor.protocol == protocolSerial {
			return messages.ErrDeviceRequired
		}
		return messages.ErrAddressRequired
	}
	if editor.protocol == protocolWSL {
		if !validEndpoint(protocolWSL, editor.wslDistro) {
			return messages.WSL.ErrDistribution
		}
		user := strings.TrimSpace(editor.wslUser.String())
		if editor.wslUseUser && (user == "" || !validWSLUser(user)) {
			return messages.WSL.ErrUser
		}
		if !validWSLDirectory(editor.wslDirectory.String()) {
			editor.advancedOpen = true
			return messages.WSL.ErrDirectory
		}
		if !validSingleLineCommand(editor.wslCommand.String()) {
			editor.advancedOpen = true
			return messages.SettingsLocalStartupInvalid
		}
	} else if editor.protocol == protocolSerial {
		if !validEndpoint(protocolSerial, editor.value(fieldAddress)) {
			return messages.Tools.ErrSerialDevice
		}
		if _, err := serial.ParseLine(editor.value(fieldPort), editor.flow); err != nil {
			return messages.Tools.ErrSerialLine
		}
	} else if _, ok := editor.portNumber(); !ok {
		return messages.ErrPortInvalid
	}
	if editor.protocol == protocolRDP && editor.value(fieldUser) == "" {
		return messages.Desktop.ErrRDPUser
	}
	if editor.protocol == protocolRDP {
		if _, ok := editor.resolutionSize(); !ok {
			return messages.Desktop.ErrResolution
		}
	}
	if editor.protocol == protocolSSH && editor.value(fieldOTP) != "" {
		if _, err := otp.Parse(editor.value(fieldOTP)); err != nil {
			editor.advancedOpen = true
			return messages.Tools.ErrOTPInvalid
		}
	}
	if editor.protocol == protocolSSH && editor.algoMode == algoCustom {
		list := editor.value(fieldAlgorithms)
		if list == "" {
			editor.advancedOpen = true
			return messages.Tools.ErrAlgoRequired
		}
		if _, err := sshconn.ParseAlgorithms(list); err != nil {
			editor.advancedOpen = true
			var unknown *sshconn.UnknownAlgorithmError
			if errors.As(err, &unknown) {
				return fmt.Sprintf(messages.Tools.ErrAlgoUnknownFmt, unknown.Name)
			}
			return messages.Tools.ErrAlgoRequired
		}
	}
	if editor.protocol == protocolSSH && !validLoginCommand(editor.value(fieldLoginCommand)) {
		editor.advancedOpen = true
		return messages.Tools.ErrLoginCommand
	}
	if editor.moshTerminal() && editor.encoding != "" && editor.encoding != charset.UTF8 {
		return messages.Tools.ErrMoshEncoding
	}
	if editor.moshTerminal() && (editor.forwardX11 || editor.agentForward) {
		editor.advancedOpen = true
		return messages.Tools.ErrMoshForwarding
	}
	if editor.moshTerminal() && editor.interactiveOnly {
		editor.advancedOpen = true
		return messages.Tools.ErrMoshInteractive
	}
	if idleKeepAliveProtocol(editor.protocol) {
		if _, ok := editor.idleKeepAliveDuration(); !ok {
			editor.advancedOpen = true
			return messages.Tools.ErrIdleKeepAliveInterval
		} else if editor.idleInputValue() == idleInputCustom {
			// The payload is saved whether or not the probe is on, so it is
			// validated the same way: text the next load would drop must not be
			// saved just because the interval is off.
			if _, err := parseIdleEscapes(editor.value(fieldIdleInputCustom)); err != nil {
				editor.advancedOpen = true
				return messages.Tools.ErrIdleKeepAliveInput
			}
		}
	}
	if _, ok := parseHostTags(editor.value(fieldTags)); !ok {
		return messages.ErrHostTagsInvalid
	}
	if _, ok := normalizeHostColor(editor.value(fieldColor)); !ok {
		return messages.ErrHostColorInvalid
	}
	candidate := editor.host(0)
	for _, host := range hosts {
		if host.ID != ignoreID && sameHost(host, candidate) && sameHostGroup(host, candidate) {
			return messages.ErrAddressTaken
		}
	}
	return ""
}

// serialPorts lists the machine's serial ports, for the serial host form and the
// picker that reloads them when it opens. A test replaces it.
var serialPorts = serial.Ports

func (editor *hostEditor) load(host Host) {
	editor.agentAccessChanged = false
	editor.form = hostFormScroll{}
	editor.color = text.Editor{}
	editor.color.Insert(host.Color)
	editor.label = text.Editor{}
	editor.address = text.Editor{}
	editor.user = text.Editor{}
	editor.password, editor.passphrase, editor.passphraseFor, editor.passwordSaved = text.Editor{}, text.Editor{}, "", false
	editor.keyLostError = ""
	editor.group = text.Editor{}
	editor.tags = text.Editor{}
	editor.tagPicker = hostTagPicker{}
	editor.groupPicker = hostGroupPicker{}
	editor.encoding = host.Encoding
	editor.icon = hostIconAuto
	editor.port = text.Editor{}
	editor.algorithms = text.Editor{}
	editor.loginCommand = text.Editor{}
	editor.loginCommand.Insert(host.LoginCommand)
	editor.loginFailurePolicy = host.LoginCommandPolicy
	editor.loadIdleKeepAlive(host)
	editor.remoteLogPaths = host.RemoteLogPaths
	editor.otp, editor.otpSaved, editor.otpRemove = text.Editor{}, false, false
	editor.algoMode = host.algoMode()
	editor.auth, editor.identity, editor.credential = host.Auth, host.Identity, host.Credential
	editor.sshOptions = host.SSHOptions.Clone()
	editor.identitiesOnly = host.IdentitiesOnly
	editor.authOpen, editor.identityOpen, editor.credentialOpen = false, false, false
	editor.forwardX11 = host.ForwardX11
	editor.agentForward = host.AgentForward
	editor.interactiveOnly = host.InteractiveOnly
	editor.noHistory = host.NoHistory
	editor.proxy, editor.proxyProfile, editor.proxyBypass = cloneOutboundProxy(host.Proxy), host.ProxyProfile, host.ProxyBypass
	editor.proxyPassword = ""
	editor.transfer = host.Transfer
	editor.advancedOpen = host.isSSH() && (host.Algo != nil || host.ForwardX11 || host.AgentForward || host.InteractiveOnly || host.NoHistory || host.IdentitiesOnly || host.LoginCommand != "" || host.LoginCommandPolicy != loginFailureStop)
	if idleKeepAliveProtocol(host.Protocol) && (host.IdleKeepAlive > 0 || host.IdleInput != idleInputProtocolDefault) {
		editor.advancedOpen = true
	}
	if host.Algo != nil {
		editor.algorithms.Insert(host.Algo.list)
	}
	editor.label.Insert(host.Label)
	editor.loadWSL(host)
	if !host.isWSL() {
		editor.address.Insert(host.Address)
		editor.user.Insert(host.User)
	}
	editor.group.Insert(host.Group)
	editor.tags.Insert(formatHostTags(host))
	editor.icon, editor.iconChosen, editor.system = host.Icon, true, host.System
	editor.protocol, editor.flow = host.Protocol, host.Line.Flow
	editor.backspace = backspaceDEL
	if hostAsksBackspace(host.Protocol, host.Terminal) {
		editor.backspace = host.Backspace
	}
	editor.terminal = host.Terminal
	editor.viaSSH, editor.clipboard, editor.chromeless = host.ViaSSH, host.Clipboard, host.Chromeless
	editor.console = host.Protocol == protocolRDP && host.Console
	editor.noAudio = host.Protocol == protocolRDP && host.NoAudio
	editor.standardSecurity = host.Protocol == protocolRDP && host.StandardSecurity
	editor.legacyTLS = host.Protocol == protocolRDP && host.LegacyTLS
	editor.colorDepth, editor.desktopEncoding, editor.effects = 0, desktop.EncodingAuto, desktop.DefaultEffects()
	editor.resolution, editor.resolutionCustom = desktopSize{}, text.Editor{}
	switch host.Protocol {
	case protocolVNC:
		editor.colorDepth, editor.desktopEncoding = host.ColorDepth, host.PreferredEncoding
	case protocolRDP:
		editor.colorDepth, editor.effects = host.ColorDepth, host.desktopEffects()
		if size, fixed := host.fixedDesktopSize(); fixed {
			editor.resolution = desktopSizeChoice(size)
			if editor.resolution == customDesktopSize {
				editor.resolutionCustom.Insert(size.String())
			}
		}
	}
	if host.Protocol == protocolSerial {
		editor.port.Insert(host.Line.String())
		editor.ports = serialPorts()
		return
	}
	editor.port.Insert(strconv.Itoa(int(host.Port)))
}

func (editor *hostEditor) loadWSL(host Host) {
	editor.wslDistro, editor.wslUseUser, editor.wslNoGUI = "", false, false
	editor.wslUser, editor.wslDirectory, editor.wslCommand = text.Editor{}, text.Editor{}, text.Editor{}
	if !host.isWSL() {
		return
	}
	editor.wslDistro, editor.wslUseUser, editor.wslNoGUI = host.Address, host.User != "", host.WSLNoGUI
	editor.wslUser.Insert(host.User)
	editor.wslDirectory.Insert(host.WSLDirectory)
	editor.wslCommand.Insert(host.LoginCommand)
	editor.advancedOpen = host.WSLDirectory != "" || host.LoginCommand != "" || host.WSLNoGUI || host.NoHistory
}

// host builds the host the form currently describes.
func (editor *hostEditor) host(id uint32) Host {
	tags, _ := parseHostTags(editor.value(fieldTags))
	color, _ := normalizeHostColor(editor.value(fieldColor))
	host := Host{
		Color:    color,
		ID:       id,
		Label:    editor.value(fieldLabel),
		Group:    editor.value(fieldGroup),
		Icon:     editor.icon,
		Address:  editor.value(fieldAddress),
		User:     editor.value(fieldUser),
		Protocol: editor.protocol,
		Encoding: editor.encoding,
	}
	if host.isWSL() {
		host.Address, host.User = editor.wslDistro, ""
		if editor.wslUseUser {
			host.User = strings.TrimSpace(editor.wslUser.String())
		}
		host.WSLDirectory, host.WSLNoGUI = editor.wslDirectory.String(), editor.wslNoGUI
		host.LoginCommand, host.NoHistory = strings.TrimSpace(editor.wslCommand.String()), editor.noHistory
	} else if editor.protocol == protocolSerial {
		host.Line, _ = serial.ParseLine(editor.value(fieldPort), editor.flow)
	} else {
		host.Port, _ = editor.portNumber()
	}
	if hostAsksBackspace(editor.protocol, editor.terminal) {
		host.Backspace = editor.backspace
	}
	if editor.protocol == protocolVNC || editor.protocol == protocolRDP {
		host.ViaSSH, host.Clipboard, host.Chromeless = editor.viaSSH, editor.clipboard, editor.chromeless
	}
	if editor.protocol == protocolRDP {
		host.Console = editor.console
		host.NoAudio = editor.noAudio
		host.StandardSecurity = editor.standardSecurity
		host.LegacyTLS = editor.legacyTLS
		host.ColorDepth = rdpColorDepth(editor.colorDepth)
		if size, ok := editor.resolutionSize(); ok {
			host.DesktopWidth, host.DesktopHeight = savedDesktopSize(size.width, size.height)
		}
		if editor.effects != desktop.DefaultEffects() {
			effects := editor.effects
			host.Effects = &effects
		}
	}
	if editor.protocol == protocolVNC {
		host.ColorDepth, host.PreferredEncoding = vncColorDepth(editor.colorDepth), editor.desktopEncoding
	}
	if editor.protocol == protocolSSH {
		host.SSHOptions = editor.sshOptions.Clone()
		host.Auth, host.Identity = editor.auth, editor.identity
		host.IdentitiesOnly = editor.identitiesOnly
		host.Terminal = editor.terminal
		host.ForwardX11 = editor.forwardX11
		host.AgentForward = editor.agentForward
		host.InteractiveOnly = editor.interactiveOnly
		host.NoHistory = editor.noHistory
		host.LoginCommand = editor.value(fieldLoginCommand)
		host.LoginCommandPolicy = editor.loginFailurePolicy
		host.RemoteLogPaths = editor.remoteLogPaths
		host.Transfer = editor.transfer
		switch editor.algoMode {
		case algoCompatible:
			host.Algo = &hostAlgo{mode: algoCompatible}
		case algoCustom:
			host.Algo = &hostAlgo{mode: algoCustom, list: editor.value(fieldAlgorithms)}
		}
	}
	if editor.protocol != protocolSerial && !host.isWSL() {
		host.Proxy = cloneOutboundProxy(editor.proxy)
		host.ProxyProfile, host.ProxyBypass = editor.proxyProfile, editor.proxyBypass
	}
	if idleKeepAliveProtocol(editor.protocol) {
		host.IdleKeepAlive, _ = editor.idleKeepAliveDuration()
		host.IdleInput = editor.idleInputModeForSave()
		if host.IdleInput == idleInputCustom {
			host.IdleInputCustom = editor.value(fieldIdleInputCustom)
		}
	}
	// Only a host that signs in with a password has one to take from a saved one.
	if editor.protocol == protocolVNC || editor.protocol == protocolRDP ||
		editor.protocol == protocolSSH && editor.auth.usesPassword() {
		host.Credential = editor.credential
	}
	host.normalizeEndpoint()
	setHostTags(&host, tags)
	host.finish()
	return host
}

// reset clears every field back to defaults for a fresh dialog session.
func (editor *hostEditor) reset() {
	editor.loadWSL(Host{})
	editor.form = hostFormScroll{}
	editor.color = text.Editor{}
	editor.label = text.Editor{}
	editor.address = text.Editor{}
	editor.user = text.Editor{}
	editor.password, editor.passphrase, editor.passphraseFor, editor.passwordSaved = text.Editor{}, text.Editor{}, "", false
	editor.keyLostError = ""
	editor.group = text.Editor{}
	editor.tags = text.Editor{}
	editor.tagPicker = hostTagPicker{}
	editor.groupPicker = hostGroupPicker{}
	editor.encoding = ""
	editor.icon, editor.iconChosen, editor.system = hostIconAuto, false, sshconn.SystemUnknown
	editor.port = text.Editor{}
	editor.algorithms = text.Editor{}
	editor.loginCommand = text.Editor{}
	editor.loginFailurePolicy = loginFailureStop
	editor.resetIdleKeepAlive()
	editor.remoteLogPaths = nil
	editor.otp, editor.otpSaved, editor.otpRemove = text.Editor{}, false, false
	editor.algoMode, editor.auth, editor.identity, editor.credential = algoDefault, authAutomatic, "", ""
	editor.sshOptions = nil
	editor.identitiesOnly = false
	editor.authOpen, editor.identityOpen, editor.credentialOpen = false, false, false
	editor.forwardX11, editor.agentForward, editor.noHistory, editor.transfer = false, false, false, transferAuto
	editor.agentAccess = false
	editor.agentAccessChanged = false
	editor.proxy, editor.proxyPassword = nil, ""
	editor.advancedOpen = false
	editor.port.Insert(strconv.Itoa(defaultPort))
	editor.protocol, editor.terminal, editor.flow = protocolSSH, terminalSSH, serial.FlowNone
	editor.backspace = backspaceDEL
	editor.viaSSH, editor.clipboard, editor.console, editor.noAudio, editor.standardSecurity, editor.legacyTLS, editor.chromeless = false, false, false, false, false, false, false
	editor.colorDepth, editor.desktopEncoding, editor.effects = 0, desktop.EncodingAuto, desktop.DefaultEffects()
}

// defaultPortText is what the port field holds for a protocol until the user
// types something: the standard port, or the line most serial devices start with.
func defaultPortText(protocol hostProtocol) string {
	if protocol == protocolSerial {
		return serial.Default.String()
	}
	return strconv.Itoa(int(Host{Protocol: protocol}.defaultPort()))
}

// chooseBaud writes a rate the common-rate select chose into the line field,
// keeping the frame the line names. A line that does not parse keeps what
// follows its first word, so mistyping the rate does not cost the frame.
func (editor *hostEditor) chooseBaud(baud string) {
	rate, err := strconv.Atoi(baud)
	if err != nil {
		return
	}
	line := editor.value(fieldPort)
	if settings, err := serial.ParseLine(line, editor.flow); err == nil {
		settings.Baud = int32(rate)
		line = settings.String()
	} else if fields := strings.FieldsFunc(line, serialLineSeparator); len(fields) > 1 {
		line = baud + " " + strings.Join(fields[1:], " ")
	} else {
		line = baud
	}
	editor.port = text.Editor{}
	editor.port.Insert(line)
}

// setProtocol switches the form to another protocol. A port or line the user has
// not changed follows the protocol; one they typed stays. A serial form with no
// device yet offers the first port the machine has.
func (editor *hostEditor) setProtocol(protocol hostProtocol) {
	if protocol == editor.protocol {
		return
	}
	previous := editor.protocol
	editor.protocol = protocol
	editor.applyProtocol(previous)
}

// applyConnection maps the form's connection-type choice to the protocol and
// terminal transport the rest of the form reads. SSH and Mosh share the same
// protocol, so switching between them keeps the password and port the user
// typed; a real protocol change runs applyProtocol.
func (editor *hostEditor) applyConnection(previous hostConnectionType) {
	protocol := editor.connection.protocol()
	terminal := editor.connection.terminal()
	editor.protocol, editor.terminal = protocol, terminal
	if protocol != previous.protocol() {
		editor.applyProtocol(previous.protocol())
		editor.terminal = terminal // applyProtocol resets it for a non-SSH protocol
	}
	if terminal == terminalMosh {
		// mosh-server needs a UTF-8 locale, and a mosh session carries no X11
		// or agent forwarding.
		editor.forwardX11, editor.agentForward = false, false
		editor.encoding = ""
	}
}

// applyProtocol applies the protocol the page's choice already wrote. previous is
// the protocol the form was showing: what the port and device fields were filled
// from before the choice replaced it.
func (editor *hostEditor) applyProtocol(previous hostProtocol) {
	protocol := editor.protocol
	// A password typed for one protocol's account is not another's.
	editor.password, editor.passphrase = text.Editor{}, text.Editor{}
	if value := editor.value(fieldPort); value == "" || value == defaultPortText(previous) {
		editor.port = text.Editor{}
		editor.port.Insert(defaultPortText(protocol))
	}
	if previous == protocolSerial {
		// The device the form offered means nothing to another protocol.
		for _, port := range editor.ports {
			if editor.value(fieldAddress) == port.Device {
				editor.address = text.Editor{}
				break
			}
		}
	}
	// The Backspace key belongs to the console protocols that ask for it, and
	// serial and Telnet keep the choice between them. An SSH host starts on
	// DEL, so a choice made for a console is not carried into one, nor back.
	if protocol == protocolSSH || previous == protocolSSH {
		editor.backspace = backspaceDEL
	}
	editor.protocol = protocol
	if protocol != protocolSSH {
		editor.terminal = terminalSSH
	}
	if !editor.iconChosen {
		if protocol == protocolSerial {
			editor.icon = hostIconSerial
		} else if editor.icon == hostIconSerial {
			editor.icon = hostIconAuto
		}
	}
	editor.authOpen, editor.identityOpen, editor.credentialOpen = false, false, false
	editor.idleKeepAliveOpen, editor.idleUnitOpen, editor.idleInputOpen = false, false, false
	if protocol == protocolSerial {
		editor.ports = serialPorts()
		if editor.value(fieldAddress) == "" && len(editor.ports) > 0 {
			editor.address.Insert(editor.ports[0].Device)
		}
	}
}
