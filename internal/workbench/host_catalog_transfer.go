package workbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/vivaui/viva/app"
	"motty/internal/sshconn"
)

type hostCatalogDocument struct {
	Format  string           `json:"format"`
	Version int              `json:"version"`
	Hosts   []savedHost      `json:"hosts"`
	Groups  []savedHostGroup `json:"groups,omitempty"`
}

func (state *State) exportHostCatalog() {
	hosts := state.exportedHosts()
	if len(hosts) == 0 {
		return
	}
	path, err := state.options.SaveFile(app.SaveFileOptions{
		Title:            state.messages.ExportHosts,
		SuggestedName:    "motty-hosts.json",
		DefaultExtension: "json",
		Filters:          []app.FileFilter{{Name: "Motty host catalog", Pattern: "*.json"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err != nil {
		state.status = fmt.Sprintf(state.messages.HostCatalogExportFailedFmt, err)
		return
	}
	document := hostCatalogDocument{
		Format: hostCatalogFormat, Version: hostCatalogSchemaVersion,
		Hosts: make([]savedHost, 0, len(hosts)), Groups: make([]savedHostGroup, 0, len(state.hostGroups)),
	}
	for _, host := range hosts {
		document.Hosts = append(document.Hosts, savedHostFromHost(host))
	}
	for _, group := range state.hostGroups {
		if catalogContainsGroup(hosts, group.Name) {
			document.Groups = append(document.Groups, savedHostGroup{Name: group.Name, Collapsed: !group.Expanded})
		}
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err == nil {
		data = append(data, '\n')
		err = writePrivateAtomic(path, data)
	}
	if err != nil {
		state.status = fmt.Sprintf(state.messages.HostCatalogExportFailedFmt, err)
		return
	}
	state.status = fmt.Sprintf(state.messages.HostCatalogExportedFmt, len(document.Hosts), path)
}

func (state *State) exportedHosts() []Host {
	if len(state.hostSelections) == 0 {
		return state.hosts
	}
	hosts := make([]Host, 0, len(state.hostSelections))
	for _, host := range state.hosts {
		if state.hostSelected(host.ID) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func savedHostFromHost(host Host) savedHost {
	return savedHost{
		ID: host.CatalogID, Label: host.Label, Group: host.Group,
		Tags: splitHostTags(host), Icon: host.Icon.String(),
		Address: host.Address, User: host.User, Port: host.Port, System: host.System.String(),
		savedTransport: host.transport(),
	}
}

func catalogContainsGroup(hosts []Host, name string) bool {
	for _, host := range hosts {
		if strings.TrimSpace(host.Group) == name {
			return true
		}
	}
	return false
}

func (state *State) importHostCatalog() {
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title:   state.messages.HostCatalogPickerTitle,
		Filters: []app.FileFilter{{Name: "Motty host catalog", Pattern: "*.json"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		state.status = fmt.Sprintf(state.messages.HostCatalogImportFailedFmt, err)
		return
	}
	document, err := readHostCatalog(paths[0])
	if err != nil {
		state.status = fmt.Sprintf(state.messages.HostCatalogImportFailedFmt, err)
		return
	}
	added, duplicates := state.applyHostCatalog(document)
	state.status = fmt.Sprintf(state.messages.HostCatalogImportedFmt, added, duplicates)
	if added > 0 {
		state.save()
	}
}

func readHostCatalog(path string) (hostCatalogDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hostCatalogDocument{}, err
	}
	var document hostCatalogDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return hostCatalogDocument{}, err
	}
	if document.Format != hostCatalogFormat {
		return hostCatalogDocument{}, fmt.Errorf("unsupported format %q", document.Format)
	}
	if document.Version != hostCatalogSchemaVersion {
		return hostCatalogDocument{}, fmt.Errorf("unsupported catalog version %d", document.Version)
	}
	return document, nil
}

func (state *State) applyHostCatalog(document hostCatalogDocument) (added, duplicates int) {
	usedIDs := make(map[string]struct{}, len(state.hosts)+len(document.Hosts))
	for _, host := range state.hosts {
		if host.CatalogID != "" {
			usedIDs[host.CatalogID] = struct{}{}
		}
	}
	for _, group := range document.Groups {
		name := strings.TrimSpace(group.Name)
		if state.hostGroupIndex(name) < 0 {
			state.hostGroups = append(state.hostGroups, hostGroup{Name: name, Expanded: !group.Collapsed})
		}
	}
	for _, entry := range document.Hosts {
		host, ok := state.hostFromCatalogEntry(entry)
		if !ok {
			continue
		}
		if state.hostIndexByEndpoint(host) >= 0 {
			duplicates++
			continue
		}
		host.ID = state.nextHostID
		state.nextHostID++
		host.CatalogID = uniqueCatalogID(host.CatalogID, host, usedIDs)
		state.hosts = append(state.hosts, host)
		state.selectedHostID = host.ID
		added++
	}
	if added > 0 {
		state.hostDataChanged()
	}
	return added, duplicates
}

func (state *State) hostFromCatalogEntry(entry savedHost) (Host, bool) {
	entry.Label = strings.TrimSpace(entry.Label)
	entry.Address = strings.TrimSpace(entry.Address)
	entry.User = strings.TrimSpace(entry.User)
	var host Host
	if !host.applyTransport(entry.savedTransport) {
		return Host{}, false
	}
	if entry.Label == "" || !validEndpoint(host.Protocol, entry.Address) || !validCatalogID(entry.ID) {
		return Host{}, false
	}
	icon, iconOK := parseHostIcon(entry.Icon)
	tags, tagsOK := normalizeHostTags(entry.Tags)
	if !iconOK || !tagsOK {
		return Host{}, false
	}
	host.CatalogID, host.Label, host.Group, host.Icon = entry.ID, entry.Label, strings.TrimSpace(entry.Group), icon
	host.Address, host.User, host.Port, host.System = entry.Address, entry.User, entry.Port, sshconn.ParseRemoteSystem(entry.System)
	host.normalizeEndpoint()
	setHostTags(&host, tags)
	host.finish()
	return host, true
}

func validCatalogID(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 128 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}
