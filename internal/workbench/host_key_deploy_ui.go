package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// The keychain's Deploy button opens this dialog. It is the one place a key's
// public half leaves Motty for a host: the dialog picks hosts, shows the exact
// key and fingerprint, and reports every host separately. Only supported stored
// keys are offered; a key that is a file path or a security-key handle has no
// private material in the vault to verify with afterwards.

var (
	hitHostKeyDeployClose  = tree.ID(dialogHits, "host-key-deploy-close")
	hitHostKeyDeploySubmit = tree.ID(dialogHits, "host-key-deploy-submit")
	hitHostKeyDeploySwitch = tree.ID(dialogHits, "host-key-deploy-switch")
	hitHostKeyDeployStop   = tree.ID(dialogHits, "host-key-deploy-stop")
	hitHostKeyDeployHosts  = tree.Sub(dialogHits, tree.Name("host-key-deploy-host"))
)

type hostKeyDeployView struct {
	keyRef      string
	fingerprint string
	selected    map[uint32]bool
	switchAuth  bool
	started     bool
	list        ui.ListState
}

func usableDeployIdentity(key identityRecord) bool {
	return key.Stored && key.Path != "" && strings.TrimSpace(key.PublicKey) != "" && key.Error == "" &&
		!key.SecurityKey && !key.Certificate
}

func (state *State) openHostKeyDeployment() {
	if state.activeSection != sectionKeys || state.modal != modalNone {
		return
	}
	key, ok := state.identityByID(state.selectedIdentityID)
	if !ok || !usableDeployIdentity(key) {
		return
	}
	state.hostKeyDeployView = hostKeyDeployView{
		keyRef:      key.Path,
		fingerprint: key.Fingerprint,
		selected:    make(map[uint32]bool),
	}
	state.modal = modalHostKeyDeploy
}

func (state *State) deployTargets() []Host {
	hosts := make([]Host, 0, len(state.hosts))
	for _, host := range state.hosts {
		if host.isSSH() {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func deployHostSupported(host Host) bool {
	return host.isSSH() && !host.mosh() && !host.tunnel && !host.InteractiveOnly &&
		host.CatalogID != "" && host.Address != "" && host.System != sshconn.SystemWindows
}

func (state *State) clickHostKeyDeployment(id gfx.ID) bool {
	if state.modal != modalHostKeyDeploy {
		return false
	}
	v := &state.hostKeyDeployView
	switch {
	case id == hitHostKeyDeployClose:
		if !state.hostKeyDeploy.running {
			state.closeModal()
			state.hostKeyDeployView = hostKeyDeployView{}
		}
	case id == hitHostKeyDeployStop:
		if state.hostKeyDeploy.running {
			state.cancelHostKeyDeployment()
		}
	case id == hitHostKeyDeploySwitch:
		if !v.started {
			v.switchAuth = !v.switchAuth
		}
	case id.Scope() == hitHostKeyDeployHosts:
		if !v.started {
			hostID := uint32(item(id))
			if host, ok := state.hostByID(hostID); ok && deployHostSupported(host) {
				v.selected[hostID] = !v.selected[hostID]
			}
		}
	case id == hitHostKeyDeploySubmit:
		if v.started || state.hostKeyDeploy.running {
			return true
		}
		ids := make([]uint32, 0, len(v.selected))
		for _, host := range state.deployTargets() {
			if v.selected[host.ID] && deployHostSupported(host) {
				ids = append(ids, host.ID)
			}
		}
		if len(ids) == 0 || v.keyRef == "" {
			return true
		}
		v.started = true
		state.startHostKeyDeployment(ids, v.keyRef, v.switchAuth)
	}
	return true
}

func (state *State) hostKeyDeployKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalHostKeyDeploy || event.Release || event.Key != gfx.KeyEscape {
		return gfx.EventResult{}
	}
	if state.hostKeyDeploy.running {
		return gfx.EventResult{Handled: true}
	}
	state.closeModal()
	state.hostKeyDeployView = hostKeyDeployView{}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitKeysDeploy}
}

func (state *State) declareHostKeyDeployment(c ui.C) {
	if state.modal != modalHostKeyDeploy {
		return
	}
	v, m := &state.hostKeyDeployView, state.messages
	key, hasKey := state.identityByID(state.selectedIdentityID)
	if !hasKey || key.Path != v.keyRef {
		hasKey = false
	}
	title := m.KeyDeployTitle
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 720, Height: min(590, max(360, dialogViewport(c).Height-80)),
		Layer: dialogLayer, Scope: dialogScope, NoClose: true, CloseID: hitHostKeyDeployClose,
	})
	dialog.Body(func() {
		body := ui.Column(c).Fill().Gap(8)
		body.Children(func() {
			description := m.KeyDeployDescription
			if hasKey {
				description = fmt.Sprintf(m.KeyDeployDescriptionFmt, key.Name, key.Fingerprint)
			}
			ui.Paragraph(c, description, 4).Color(state.pal.muted).FillWidth().Shrink(0)
			if !hasKey {
				ui.Paragraph(c, m.KeyDeployKeyUnavailable, 2).Color(state.pal.danger).FillWidth().Shrink(0)
				return
			}
			if v.started && state.hostKeyDeploy.running {
				ui.Paragraph(c, m.KeyDeployRunning, 2).Color(state.pal.muted).FillWidth().Shrink(0)
			} else if v.started {
				ui.Paragraph(c, m.KeyDeployFinished, 2).Color(state.pal.muted).FillWidth().Shrink(0)
			}
			if !v.started {
				ui.Checkbox(c, state.pageFlag(v.switchAuth), m.KeyDeploySwitch).Hit(hitHostKeyDeploySwitch, m.KeyDeploySwitch).Shrink(0)
				if v.switchAuth {
					ui.Paragraph(c, m.KeyDeploySwitchDescription, 2).Color(state.pal.muted).FillWidth().Shrink(0)
				}
			}
			hosts := state.deployTargets()
			if len(hosts) == 0 {
				ui.Paragraph(c, m.KeyDeployNoHosts, 2).Color(state.pal.muted).FillWidth().Shrink(0)
				return
			}
			results := make(map[uint32]hostKeyDeployResult, len(state.hostKeyDeploy.results))
			for _, result := range state.hostKeyDeploy.results {
				results[result.HostID] = result
			}
			ui.List(c, &v.list, len(hosts), func(row ui.ListRow) {
				if row.Index < 0 || row.Index >= len(hosts) {
					return
				}
				host := hosts[row.Index]
				result, done := results[host.ID]
				row.Element.Height(58).PadX(3).Items(ui.Center).Gap(8).Children(func() {
					if !v.started {
						ui.Checkbox(row.C, state.pageFlag(v.selected[host.ID]), "").Hit(hitHostKeyDeployHosts.At(int(host.ID)), host.Label).Disabled(!deployHostSupported(host)).Shrink(0)
					}
					ui.Column(row.C).Grow(1).Shrink(1).Children(func() {
						ui.Text(row.C, host.Label).Color(state.pal.text).Ellipsis()
						detail := host.User + "@" + net.JoinHostPort(host.Address, strconv.Itoa(int(host.Port)))
						if host.Port == 0 {
							detail = host.User + "@" + net.JoinHostPort(host.Address, "22")
						}
						ui.Paragraph(row.C, detail, 2).FontSize(metrics.FontSizeS).Color(state.pal.muted).FillWidth()
					})
					status, color := "", state.pal.muted
					switch {
					case !deployHostSupported(host):
						status = m.KeyDeployHostUnsupported
					case done && result.Error != "":
						status, color = state.hostKeyDeployError(result), state.pal.danger
					case done && result.Switched:
						status, color = m.KeyDeploySwitched, state.pal.text
					case done && result.Imported:
						status, color = m.KeyDeployInstalled, state.pal.text
					case v.started && state.hostKeyDeploy.running:
						status = m.KeyDeployWaiting
					}
					if status != "" {
						ui.Text(row.C, status).FontSize(metrics.FontSizeS).Color(color).Width(210).Ellipsis()
					}
				})
			}).Grow(1).Basis(0).MinHeight(0).Scrollbars().ScrollbarSize(catalogBarGutter).Label(title)
		})
	})
	dialog.Footer(func() {
		actions := []components.ButtonAction{{ID: hitHostKeyDeployClose, Label: m.KeyDeployClose, Disabled: state.hostKeyDeploy.running, Variant: components.ButtonSecondary}}
		if state.hostKeyDeploy.running {
			actions = append(actions, components.ButtonAction{ID: hitHostKeyDeployStop, Label: m.KeyDeployStop, Disabled: state.hostKeyDeploy.canceled, Variant: components.ButtonSecondary})
		} else if !v.started {
			actions = append(actions, components.ButtonAction{ID: hitHostKeyDeploySubmit, Label: m.KeyDeploySubmit, Disabled: len(v.selected) == 0 || !hasKey, Variant: components.ButtonDefault})
		}
		state.dialogActionFooter(c, actions...)
	})
}

func (state *State) hostKeyDeployError(result hostKeyDeployResult) string {
	m := state.messages
	switch result.Error {
	case hostKeyDeployErrorKeyUnavailable:
		return m.KeyDeployKeyUnavailable
	case hostKeyDeployErrorUnsupportedHost:
		return m.KeyDeployHostUnsupported
	case hostKeyDeployErrorRoute:
		return m.KeyDeployRouteUnsupported
	case hostKeyDeployErrorNotConfirmed:
		return m.KeyDeployNotConfirmed
	case hostKeyDeployErrorCertificate:
		return m.KeyDeployCertUnsupported
	case hostKeyDeployErrorNotVerified:
		return m.KeyDeployNotVerified
	case hostKeyDeployErrorCanceled:
		return m.KeyDeploySwitchedNotSaved
	case hostKeyDeployErrorHostChanged, hostKeyDeployErrorRouteChanged:
		return m.KeyDeployChanged
	case hostKeyDeployErrorSave:
		return m.KeyDeploySwitchedNotSaved
	case hostKeyDeployErrorHostKey:
		return m.KeyDeployHostKey
	case hostKeyDeployErrorAuth:
		return m.KeyDeployAuthFailed
	case hostKeyDeployErrorNoPassword:
		return m.KeyDeployNoPassword
	case hostKeyDeployErrorTimeout:
		return m.KeyDeployTimeout
	case hostKeyDeployErrorConnection, hostKeyDeployErrorOther:
		return m.KeyDeployFailed
	}
	return m.KeyDeployFailed
}
