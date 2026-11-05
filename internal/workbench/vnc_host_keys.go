package workbench

import (
	"errors"
	"fmt"
	"slices"

	"motty/internal/sshconn"
)

// vncHostKey is a public SHA-256 identity pin, saved with the host record so
// backups, sync, copies and deletion use the catalog's existing lifecycle.
type vncHostKey struct {
	Address     string `json:"address"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
}

func vncIdentityKind(kind string) bool {
	return kind == vncServerKeyType
}

func (host Host) vncFingerprint(address, kind string) string {
	for _, key := range host.VNCKeys {
		if key.Address == address && key.Kind == kind {
			return key.Fingerprint
		}
	}
	return ""
}

// rememberVNCServerKey runs on the UI thread before approving the waiting
// handshake. A failed save or a concurrent host/key change never grants trust.
func (state *State) rememberVNCServerKey(session *Session) error {
	if session.phase != sshconn.PhaseHostKeyPrompt || session.host.Protocol != protocolVNC || !vncIdentityKind(session.hostKey.Type) {
		return errors.New("VNC identity is not awaiting approval")
	}
	if state.config == nil {
		return errors.New("configuration store is unavailable")
	}
	if err := state.refreshShared(); err != nil {
		return err
	}
	host, ok := state.hostByCatalogID(session.host.CatalogID)
	if !ok || host.Protocol != protocolVNC {
		return errors.New("the VNC host is no longer saved")
	}
	key := vncHostKey{Address: session.hostKey.Address, Kind: session.hostKey.Type, Fingerprint: session.hostKey.Fingerprint}
	if previous := host.vncFingerprint(key.Address, key.Kind); previous != "" && previous != key.Fingerprint {
		return fmt.Errorf("%w for %s", sshconn.ErrHostKeyChanged, key.Address)
	}
	previous := slices.Clone(host.VNCKeys)
	if host.vncFingerprint(key.Address, key.Kind) == "" {
		host.VNCKeys = append(slices.Clone(previous), key)
		state.hosts[state.hostIndex(host.ID)] = host
	}
	if err := state.saveWithResult(); err != nil {
		// A save conflict may have refreshed the catalog; preserve that winner.
		if current, ok := state.hostByCatalogID(host.CatalogID); ok && slices.Equal(current.VNCKeys, host.VNCKeys) {
			current.VNCKeys = previous
			state.hosts[state.hostIndex(current.ID)] = current
		}
		return err
	}
	saved, ok := state.hostByCatalogID(host.CatalogID)
	if !ok || saved.Protocol != protocolVNC || saved.vncFingerprint(key.Address, key.Kind) != key.Fingerprint {
		return errors.New("the VNC host changed while saving its identity")
	}
	for index := range state.sessions {
		if state.sessions[index].host.CatalogID == saved.CatalogID {
			state.sessions[index].host.VNCKeys = slices.Clone(saved.VNCKeys)
		}
	}
	return nil
}
