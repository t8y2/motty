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
	address, port, err := net.SplitHostPort(session.hostKey.Address)
	var portBuffer [5]byte
	if err != nil || address != session.host.Address || port != string(strconv.AppendUint(portBuffer[:0], uint64(session.host.Port), 10)) {
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
	conn.mu.Lock()
	if conn.status.HostKey.Address == session.hostKey.Address && conn.status.HostKey.Type == session.hostKey.Type {
		conn.status.HostKey.Known = nil
	}
	conn.mu.Unlock()
	session.hostKey.Known = nil
	return true
}
