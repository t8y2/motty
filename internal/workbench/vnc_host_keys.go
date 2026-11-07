package workbench

import (
	"cmp"
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

// An unpinned third entry must not let two conflicting identities be folded
// together. Check the whole duplicate group before choosing a surviving host.
func conflictingVNCHostKeys(hosts []Host) map[mergeKey]bool {
	pins := make(map[mergeKey]map[[2]string]string)
	conflicts := make(map[mergeKey]bool)
	for _, host := range hosts {
		if host.Protocol != protocolVNC {
			continue
		}
		group := mergeKeyOf(host)
		for _, key := range host.VNCKeys {
			if key.Fingerprint == "" {
				continue
			}
			if pins[group] == nil {
				pins[group] = make(map[[2]string]string)
			}
			identity := [2]string{key.Address, key.Kind}
			if previous := pins[group][identity]; previous != "" && previous != key.Fingerprint {
				conflicts[group] = true
			}
			pins[group][identity] = key.Fingerprint
		}
	}
	return conflicts
}

func (state *State) mergeVNCHostKeys(merges []hostMerge) {
	for _, merge := range merges {
		if merge.dropped.Protocol != protocolVNC {
			continue
		}
		host, ok := state.hostByCatalogID(state.resolveMerged(merge.dropped.CatalogID))
		if !ok {
			continue
		}
		host.VNCKeys = slices.Clone(host.VNCKeys)
		for _, key := range merge.dropped.VNCKeys {
			if !slices.Contains(host.VNCKeys, key) {
				host.VNCKeys = append(host.VNCKeys, key)
			}
		}
		slices.SortFunc(host.VNCKeys, func(a, b vncHostKey) int {
			return cmp.Or(cmp.Compare(a.Address, b.Address), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Fingerprint, b.Fingerprint))
		})
		state.hosts[state.hostIndex(host.ID)] = host
	}
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
	if session.phase != sshconn.PhaseHostKeyPrompt || session.host.Protocol != protocolVNC || session.hostKey.Type != vncServerKeyType {
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
	fingerprint := host.vncFingerprint(key.Address, key.Kind)
	if fingerprint != "" && fingerprint != key.Fingerprint {
		return fmt.Errorf("%w for %s", sshconn.ErrHostKeyChanged, key.Address)
	}
	previous := host.VNCKeys
	if fingerprint == "" {
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
