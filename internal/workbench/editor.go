package workbench

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
	"motty/internal/otp"
	"motty/internal/serial"
	"motty/internal/sshconn"
)

// hostEditor holds the New Host dialog state: one text editor per field.
// Keyboard focus belongs to the runtime; the dialog never tracks it.
type hostEditor struct {
	label   text.Editor
	address text.Editor
	user    text.Editor
	// password is only the value typed in this dialog. It is never loaded from
	// the vault or copied into Host; a successful submit stores it separately.
	password text.Editor
	group    text.Editor
	tags     text.Editor
	icon     hostIcon
	color    text.Editor
	// system is the system the host was last detected to run, whose badge the
	// automatic icon shows.
	system sshconn.RemoteSystem
	// iconOpen is the icon choice's list being open.
	iconOpen bool
	// iconReveal says the list has just opened and has to scroll to the chosen icon.
	iconReveal bool
	// port is the port of an SSH or Telnet host and the line settings of a serial
	// one ("115200 8N1").
	port text.Editor

	// algoMode is the SSH algorithm choice; algorithms holds the custom list.
	algoMode   algoMode
	transfer   transferMode
	algorithms text.Editor
	// forwardX11 is the host's "Forward X11" switch.
	forwardX11 bool
	// agentForward is the host's "Forward ssh-agent" switch.
	agentForward bool
	// noHistory is the host's "Don't record commands" switch.
	noHistory bool
	// proxy is nil to inherit the global proxy, ProxyNone to force a direct
	// connection, or a dedicated proxy. proxyPassword is staged until the outer
	// host form is saved.
	proxy         *outboundProxy
	proxyPassword string
	// otp is a new one-time code secret to save with the host. A saved one is
	// never shown again. Existing hosts stay unchecked while the vault is closed,
	// so merely opening their editor does not ask the system secret store for its key.
	otp                             text.Editor
	otpChecked, otpSaved, otpRemove bool
	// advancedOpen is the SSH form's Advanced section being open: the algorithm
	// choice, the one-time code secret and the two switches. It opens by itself
	// for a host that sets any of them and for a value that fails validation.
	advancedOpen bool

	protocol hostProtocol
	flow     serial.Flow
	// viaSSH and clipboard are the switches of a VNC or RDP host.
	viaSSH, clipboard bool
	// ports are the serial ports found when the picker was last opened.
	ports     []serial.Port
	portsOpen bool
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
	default:
		return &editor.port
	}
}

// value is a field's text without surrounding blanks; pasted addresses often
// carry them.
func (editor *hostEditor) value(field hostField) string {
	return strings.TrimSpace(editor.field(field).String())
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
// submittable. A host is a duplicate only when address, user and port all match
// an existing one: the same machine under another account is a different host.
func (editor *hostEditor) validate(hosts []Host, ignoreID uint32, messages i18n.Messages) string {
	if editor.value(fieldLabel) == "" || editor.value(fieldAddress) == "" {
		return messages.ErrLabelRequired
	}
	if editor.protocol == protocolSerial {
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
	if _, ok := parseHostTags(editor.value(fieldTags)); !ok {
		return messages.ErrHostTagsInvalid
	}
	if _, ok := normalizeHostColor(editor.value(fieldColor)); !ok {
		return messages.ErrHostColorInvalid
	}
	candidate := editor.host(0)
	for _, host := range hosts {
		if host.ID != ignoreID && sameEndpoint(host, candidate) {
			return messages.ErrAddressTaken
		}
	}
	return ""
}

func (editor *hostEditor) load(host Host) {
	editor.color = text.Editor{}
	editor.color.Insert(host.Color)
	editor.label = text.Editor{}
	editor.address = text.Editor{}
	editor.user = text.Editor{}
	editor.password = text.Editor{}
	editor.group = text.Editor{}
	editor.tags = text.Editor{}
	editor.icon, editor.iconOpen = hostIconAuto, false
	editor.port = text.Editor{}
	editor.algorithms = text.Editor{}
	editor.otp, editor.otpChecked, editor.otpSaved, editor.otpRemove = text.Editor{}, true, false, false
	editor.algoMode = host.algoMode()
	editor.forwardX11 = host.ForwardX11
	editor.agentForward = host.AgentForward
	editor.noHistory = host.NoHistory
	editor.proxy, editor.proxyPassword = cloneOutboundProxy(host.Proxy), ""
	editor.transfer = host.Transfer
	editor.advancedOpen = host.isSSH() && (host.Algo != nil || host.ForwardX11 || host.AgentForward || host.NoHistory)
	if host.Algo != nil {
		editor.algorithms.Insert(host.Algo.list)
	}
	editor.label.Insert(host.Label)
	editor.address.Insert(host.Address)
	editor.user.Insert(host.User)
	editor.group.Insert(host.Group)
	editor.tags.Insert(formatHostTags(host))
	editor.icon, editor.system = host.Icon, host.System
	editor.protocol, editor.flow = host.Protocol, host.Line.Flow
	editor.viaSSH, editor.clipboard = host.ViaSSH, host.Clipboard
	editor.portsOpen = false
	if host.Protocol == protocolSerial {
		editor.port.Insert(host.Line.String())
		return
	}
	editor.port.Insert(strconv.Itoa(int(host.Port)))
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
	}
	if editor.protocol == protocolSerial {
		host.Line, _ = serial.ParseLine(editor.value(fieldPort), editor.flow)
	} else {
		host.Port, _ = editor.portNumber()
	}
	if editor.protocol == protocolVNC || editor.protocol == protocolRDP {
		host.ViaSSH, host.Clipboard = editor.viaSSH, editor.clipboard
	}
	if editor.protocol == protocolSSH {
		host.ForwardX11 = editor.forwardX11
		host.AgentForward = editor.agentForward
		host.NoHistory = editor.noHistory
		host.Transfer = editor.transfer
		switch editor.algoMode {
		case algoCompatible:
			host.Algo = &hostAlgo{mode: algoCompatible}
		case algoCustom:
			host.Algo = &hostAlgo{mode: algoCustom, list: editor.value(fieldAlgorithms)}
		}
	}
	if editor.protocol != protocolSerial {
		host.Proxy = cloneOutboundProxy(editor.proxy)
	}
	host.normalizeEndpoint()
	setHostTags(&host, tags)
	host.finish()
	return host
}

// reset clears every field back to defaults for a fresh dialog session.
func (editor *hostEditor) reset() {
	editor.color = text.Editor{}
	editor.label = text.Editor{}
	editor.address = text.Editor{}
	editor.user = text.Editor{}
	editor.password = text.Editor{}
	editor.group = text.Editor{}
	editor.tags = text.Editor{}
	editor.icon, editor.system, editor.iconOpen = hostIconAuto, sshconn.SystemUnknown, false
	editor.port = text.Editor{}
	editor.algorithms = text.Editor{}
	editor.otp, editor.otpChecked, editor.otpSaved, editor.otpRemove = text.Editor{}, true, false, false
	editor.algoMode, editor.forwardX11, editor.agentForward, editor.noHistory, editor.transfer = algoDefault, false, false, false, transferAuto
	editor.proxy, editor.proxyPassword = nil, ""
	editor.advancedOpen = false
	editor.port.Insert(strconv.Itoa(defaultPort))
	editor.protocol, editor.flow, editor.portsOpen = protocolSSH, serial.FlowNone, false
	editor.viaSSH, editor.clipboard = false, false
}

// defaultPortText is what the port field holds for a protocol until the user
// types something: the standard port, or the line most serial devices start with.
func defaultPortText(protocol hostProtocol) string {
	if protocol == protocolSerial {
		return serial.Default.String()
	}
	return strconv.Itoa(int(Host{Protocol: protocol}.defaultPort()))
}

// setProtocol switches the form to another protocol. A port or line the user has
// not changed follows the protocol; one they typed stays. A serial form with no
// device yet offers the first port the machine has.
func (editor *hostEditor) setProtocol(protocol hostProtocol) {
	if protocol == editor.protocol {
		return
	}
	// A password typed for one protocol's account is not another's.
	editor.password = text.Editor{}
	previous := defaultPortText(editor.protocol)
	if value := editor.value(fieldPort); value == "" || value == previous {
		editor.port = text.Editor{}
		editor.port.Insert(defaultPortText(protocol))
	}
	if editor.protocol == protocolSerial {
		// The device the form offered means nothing to another protocol.
		for _, port := range editor.ports {
			if editor.value(fieldAddress) == port.Device {
				editor.address = text.Editor{}
				break
			}
		}
	}
	editor.protocol, editor.portsOpen, editor.iconOpen = protocol, false, false
	if protocol == protocolSerial {
		editor.ports = serial.Ports()
		if editor.value(fieldAddress) == "" && len(editor.ports) > 0 {
			editor.address.Insert(editor.ports[0].Device)
		}
	}
}
