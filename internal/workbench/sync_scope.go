package workbench

import (
	"encoding/json"
	"slices"
	"strings"

	"motty/internal/configstore"
	"motty/internal/vault"
)

// A syncCategory is one kind of content cloud sync exchanges. The values are
// written to this device's own state, so they are stable words rather than
// numbers: reordering the settings page must not change what a saved exclusion
// means.
type syncCategory string

const (
	syncCategoryHosts       syncCategory = "hosts"
	syncCategorySettings    syncCategory = "settings"
	syncCategoryNotes       syncCategory = "notes"
	syncCategorySnippets    syncCategory = "snippets"
	syncCategoryRoutes      syncCategory = "routes"
	syncCategoryForwards    syncCategory = "forwards"
	syncCategoryWorkspaces  syncCategory = "workspaces"
	syncCategoryCredentials syncCategory = "credentials"
	syncCategoryKeys        syncCategory = "keys"
	syncCategoryAI          syncCategory = "ai"
	syncCategoryRules       syncCategory = "rules"
	syncCategoryKnownHosts  syncCategory = "known-hosts"
)

// syncCategories is the selection in the order the settings page offers it.
var syncCategories = [...]syncCategory{
	syncCategoryHosts,
	syncCategorySettings,
	syncCategoryNotes,
	syncCategorySnippets,
	syncCategoryRoutes,
	syncCategoryForwards,
	syncCategoryWorkspaces,
	syncCategoryCredentials,
	syncCategoryKeys,
	syncCategoryAI,
	syncCategoryRules,
	syncCategoryKnownHosts,
}

// recordSyncCategory names the category a shared record belongs to. A kind this
// build does not know is not named: such a record is always exchanged, because
// only a build that knows it can decide that it is turned off.
func recordSyncCategory(kind string) (syncCategory, bool) {
	switch kind {
	case "host", "group":
		return syncCategoryHosts, true
	case kindSetting, "outbound-proxy":
		return syncCategorySettings, true
	case "note", "note-folder":
		return syncCategoryNotes, true
	case "snippet":
		return syncCategorySnippets, true
	case "forward":
		return syncCategoryForwards, true
	case "proxy":
		return syncCategoryRoutes, true
	case kindWorkspaceTemplate:
		return syncCategoryWorkspaces, true
	case "credential":
		return syncCategoryCredentials, true
	case kindAIProvider, kindAICLIProvider:
		return syncCategoryAI, true
	case "bookmark", "highlight", "autofill":
		return syncCategoryRules, true
	}
	return "", false
}

// secretSyncCategory names the category of a saved password. A name this build
// does not know is not named, so it stays out of the selection, like an unknown
// record kind. The logins of the sync service itself never reach here; they are
// excluded before any category is looked at.
func secretSyncCategory(name string) (syncCategory, bool) {
	switch {
	case strings.HasPrefix(name, "host:"), strings.HasPrefix(name, "host-proxy:"):
		return syncCategoryHosts, true
	case name == proxyPasswordSecret:
		return syncCategorySettings, true
	case strings.HasPrefix(name, "credential:"):
		return syncCategoryCredentials, true
	case strings.HasPrefix(name, "sshkey:"), strings.HasPrefix(name, "passphrase:"):
		return syncCategoryKeys, true
	case strings.HasPrefix(name, "aikey:"):
		return syncCategoryAI, true
	case strings.HasPrefix(name, "autofill:"):
		return syncCategoryRules, true
	}
	return "", false
}

// syncScope is this computer's choice of what cloud sync exchanges. It is a
// device setting, not a shared record: closing a category here changes nothing
// on the other computers. The zero value allows everything.
type syncScope struct {
	excluded []syncCategory // sorted; unknown words kept as they came
	hosts    syncHostSelection
	groups   map[string]bool // resolved for one exchange; never persisted
}

// newSyncScope builds a scope from saved exclusions. Unknown words are kept so
// that a choice made by a newer build survives a save here.
func newSyncScope(excluded []syncCategory) syncScope {
	kept := make([]syncCategory, 0, len(excluded))
	for _, category := range excluded {
		if category != "" {
			kept = append(kept, category)
		}
	}
	slices.Sort(kept)
	return syncScope{excluded: slices.Compact(kept)}
}

// allows says whether a category is exchanged. The empty category stands for
// something this build has no category for, and is always allowed.
func (scope syncScope) allows(category syncCategory) bool {
	return category == "" || !slices.Contains(scope.excluded, category)
}

// allowsRecord says whether a record of this kind is exchanged.
func (scope syncScope) allowsRecord(kind string) bool {
	category, known := recordSyncCategory(kind)
	return !known || scope.allows(category)
}

// allowsSecret says whether a saved password is exchanged.
func (scope syncScope) allowsSecret(name string) bool {
	category, known := secretSyncCategory(name)
	if known && !scope.allows(category) {
		return false
	}
	if category == syncCategoryHosts && scope.hosts.OnlySelected {
		_, rest, _ := strings.Cut(name, ":")
		id, _, ok := strings.Cut(rest, ":")
		return ok && scope.hosts.hasHost(id)
	}
	return true
}

// allowsKnownHosts says whether the trusted host keys are exchanged.
func (scope syncScope) allowsKnownHosts() bool {
	return scope.allows(syncCategoryKnownHosts)
}

// empty reports that every category is off: there is nothing to sync, and the
// engine must not contact the service at all.
func (scope syncScope) empty() bool {
	for _, category := range syncCategories {
		if scope.allows(category) && (category != syncCategoryHosts || !scope.hosts.OnlySelected || len(scope.hosts.IDs)+len(scope.hosts.Groups) > 0) {
			return false
		}
	}
	return true
}

// toggled returns the scope with one category changed.
func (scope syncScope) toggled(category syncCategory) syncScope {
	next := append([]syncCategory(nil), scope.excluded...)
	if index := slices.Index(next, category); index >= 0 {
		next = slices.Delete(next, index, index+1)
	} else {
		next = append(next, category)
	}
	scope.excluded = newSyncScope(next).excluded
	return scope
}

// excludedCopy is what this scope saves: the closed categories, in a stable
// order, or nil when everything is open.
func (scope syncScope) excludedCopy() []syncCategory {
	if len(scope.excluded) == 0 {
		return nil
	}
	return append([]syncCategory(nil), scope.excluded...)
}

// filterRecords keeps the records a scope exchanges. It returns the input slice
// itself when nothing is closed, so an all-open scope costs no allocation.
func (scope syncScope) filterRecords(records []configstore.Record) []configstore.Record {
	if len(scope.excluded) == 0 && !scope.hosts.OnlySelected {
		return records
	}
	kept := make([]configstore.Record, 0, len(records))
	for _, record := range records {
		if scope.allowsRecordKey(record.Kind, record.ID) {
			kept = append(kept, record)
		}
	}
	return kept
}

// filterSecrets keeps the saved passwords a scope exchanges.
func (scope syncScope) filterSecrets(secrets []vault.Secret) []vault.Secret {
	if len(scope.excluded) == 0 && !scope.hosts.OnlySelected {
		return secrets
	}
	kept := make([]vault.Secret, 0, len(secrets))
	for _, secret := range secrets {
		if scope.allowsSecret(secret.Name) {
			kept = append(kept, secret)
		}
	}
	return kept
}

// syncHostSelection is local to this device. A group is a bulk selection of its
// current hosts, not a rule that silently shares hosts added there later. IDs
// survive moves, renames and deletions, so selected tombstones still travel.
type syncHostSelection struct {
	OnlySelected bool     `json:"only_selected,omitempty"`
	IDs          []string `json:"ids,omitempty"`
	Groups       []string `json:"groups,omitempty"`
}

func (selection syncHostSelection) hasHost(id string) bool {
	return id != "" && slices.Contains(selection.IDs, id)
}

func (selection syncHostSelection) clone() syncHostSelection {
	selection.IDs = slices.Clone(selection.IDs)
	selection.Groups = slices.Clone(selection.Groups)
	return selection
}

// allowsRecordKey refines the category gate with the stable record identity.
// A tombstone has no payload, so membership must never depend on its payload.
func (scope syncScope) allowsRecordKey(kind, id string) bool {
	if !scope.allowsRecord(kind) {
		return false
	}
	if !scope.hosts.OnlySelected {
		return true
	}
	switch kind {
	case "host":
		return scope.hosts.hasHost(id)
	case "group":
		return scope.groups[id]
	default:
		return true
	}
}

// withHostGroups resolves just the group metadata needed by selected hosts.
// Both sides contribute ancestors: a host moving or a group being renamed must
// carry the old group's tombstone as well as the new group's metadata.
func (scope syncScope) withHostGroups(collections ...[]configstore.Record) syncScope {
	if !scope.hosts.OnlySelected {
		return scope
	}
	scope.groups = make(map[string]bool)
	add := func(path string) {
		for path != "" {
			scope.groups[path] = true
			path = hostGroupParent(path)
		}
	}
	for _, group := range scope.hosts.Groups {
		add(group)
	}
	for _, records := range collections {
		for _, record := range records {
			if record.Kind != "host" || record.Deleted || !scope.hosts.hasHost(record.ID) {
				continue
			}
			var payload recordPayload
			var host savedHost
			if json.Unmarshal(record.Payload, &payload) == nil && json.Unmarshal(payload.Data, &host) == nil {
				add(host.Group)
			}
		}
	}
	return scope
}
