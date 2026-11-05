package i18n

// DesktopMessages are the texts of remote-desktop tabs (VNC and RDP): the host
// editor's choices, the bar and status line over the picture, and the page shown
// when the connection ended.
type DesktopMessages struct {
	ProtocolVNC               string
	ProtocolRDP               string
	FieldSSHUser              string
	FieldVNCUser              string
	VNCUserHint               string
	ErrVNCUser                string
	ErrVNCPassword            string
	VNCAccountPasswordChanged string
	VNCSystemUser             string
	VNCKeyNewFmt              string
	VNCKeyChangedFmt          string
	StripVNCEncrypted         string
	FieldRDPUser              string
	RDPUserHint               string
	ErrRDPUser                string
	CertNewFmt                string
	CertChangedFmt            string
	StripTLS                  string
	ViaSSH                    string
	ShareClipboard            string
	HideBars                  string
	ConsoleSession            string
	VNCPlaintext              string
	TipCapture                string
	TipRelease                string
	Fit                       string
	Actual                    string
	Fullscreen                string
	ExitFullscreen            string
	FullscreenUnavailable     string
	FullscreenFailedFmt       string
	ShowLatency               string
	LatencyUnavailable        string
	LatencyHintFmt            string
	CtrlAltDel                string
	StripCaptured             string
	StripReleased             string
	StripSizeFmt              string
	StripEncrypted            string
	StripPlain                string
	StripClipboard            string
	Disconnected              string
	DisconnectedFmt           string
	Reconnect                 string
	ReconnectHint             string
	AccessibleFmt             string
	StatusReleased            string
	StatusCaptured            string
	ErrNoDesktop              string
}
