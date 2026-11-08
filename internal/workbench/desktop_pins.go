package workbench

import (
	"fmt"
	"net"
	"strconv"

	"github.com/vivaui/viva/components"
	"motty/internal/desktop/pins"
	"motty/internal/sshconn"
)

func canForgetServerIdentity(session *Session) bool {
	if session == nil || session.role == sshconn.EndpointProxyJump || len(session.hostKey.Known) == 0 {
		return false
	}
	if session.hostKey.Address != net.JoinHostPort(session.host.Address, strconv.Itoa(int(session.host.Port))) {
		return false
	}
	return session.host.Protocol == protocolVNC && session.hostKey.Type == vncServerKeyType || session.host.Protocol == protocolRDP && session.hostKey.Type == rdpCertificateType
}

// Forgetting affects the typed server endpoint, independently of host records.
// It grants no trust; the next connection asks about the presented key again.
func (state *State) forgetServerIdentity(session *Session) bool {
	if !canForgetServerIdentity(session) {
		return false
	}
	conn, ok := session.conn.(*desktopConn)
	if !ok || conn.certs == nil {
		return false
	}
	protocol := pins.RDP
	if session.hostKey.Type == vncServerKeyType {
		protocol = pins.VNC
	}
	if err := conn.certs.Forget(pins.Endpoint{Protocol: protocol, Address: session.hostKey.Address}); err != nil {
		state.status = fmt.Sprintf(state.messages.StatusSaveFailedFmt, err)
		state.fileNotices.show(state.messages.HostKeyForget, state.status, components.ToastError)
		return false
	}
	session.hostKey.Known = nil
	return true
}
