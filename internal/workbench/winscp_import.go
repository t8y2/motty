package workbench

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/app"
	"motty/internal/sshconfig"
	"motty/internal/winscpini"
)

func (state *State) pickWinSCPFile() string {
	title := state.messages.WinSCPImport.PickFile
	paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: title})
	switch {
	case errors.Is(err, app.ErrCanceled):
	case err != nil:
		state.showActionFailure(title, err, state.messages.ActionFeedback.FilePickerFailed)
	case len(paths) != 0:
		return paths[0]
	}
	return ""
}

func (state *State) importWinSCP() {
	path := state.pickWinSCPFile()
	if path == "" {
		return
	}
	m := state.messages.WinSCPImport
	cfg, err := winscpini.Load(path)
	if err != nil {
		state.showFileReadFailure(m.PickFile, err, m.Title)
		return
	}
	sites, skipped := cfg.Sites()
	builder := newHostImportBuilder(state, sshconfig.Result{})
	for _, site := range sites {
		if hasWinSCPHost(state.hosts, site) || hasWinSCPHost(builder.plan.hosts, site) {
			builder.plan.duplicateHosts++
			continue
		}
		builder.addWinSCPHost(site)
	}
	if skipped > 0 {
		builder.warn(fmt.Sprintf(m.SkippedFmt, skipped))
	}
	plan := builder.plan
	summary := fmt.Sprintf(m.SummaryFmt, len(plan.hosts), len(plan.identities), plan.duplicateHosts)
	_ = state.applyHostImportSummary(plan, summary)
	state.hostImportResult.title = m.Title
}

func hasWinSCPHost(hosts []Host, site winscpini.Site) bool {
	return slices.ContainsFunc(hosts, func(host Host) bool {
		return host.isSSH() && strings.EqualFold(winSCPSiteName(host), site.Name) &&
			connectionKey(host.Address, host.User, host.Port) == connectionKey(site.Host, site.User, site.Port)
	})
}

func (builder *hostImportBuilder) addWinSCPHost(site winscpini.Site) Host {
	host := Host{ID: builder.plan.nextHostID, CatalogID: freshCatalogID(builder.usedIDs)}
	updateWinSCPHost(&host, site)
	if site.KeyPath != "" {
		host.SSHOptions = importedSSHOptions(&sshconfig.ConnectionOptions{IdentityFiles: []string{site.KeyPath}})
		builder.addIdentity(site.KeyPath)
	}
	builder.plan.nextHostID++
	builder.plan.hosts = append(builder.plan.hosts, host)
	return host
}

func updateWinSCPHost(host *Host, site winscpini.Site) {
	group, label := "", site.Name
	if last := strings.LastIndexByte(site.Name, '/'); last >= 0 {
		group, label = site.Name[:last], site.Name[last+1:]
	}
	host.Group, host.Label = group, label
	host.Address, host.User, host.Port = site.Host, site.User, port22(site.Port)
	host.finish()
}

func winSCPSiteName(host Host) string {
	if host.Group == "" {
		return host.Label
	}
	return host.Group + "/" + host.Label
}
