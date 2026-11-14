package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
)

// navPage names one page's way in on the sidebar. The names are what the saved
// hidden set refers to, so the file keeps its meaning if a section is ever
// renumbered or the rows are reordered.
type navPage uint8

const (
	navPageHosts navPage = iota
	navPageWorkspaces
	navPageMonitor
	navPageSessionLogs
	navPageKeychain
	navPageProxies
	navPageForwards
	navPageKnownHosts
	navPageSnippets
	navPageNotes
	navPageCount
)

// navHidden is the set of pages the sidebar leaves out. Hiding a page takes away
// only its row: the page keeps its section, its shortcuts, its command-palette
// entry and its state, so it is still reachable and nothing moves when it is
// shown again.
type navHidden uint16

func (hidden navHidden) has(page navPage) bool {
	return page < navPageCount && hidden&(navHidden(1)<<page) != 0
}

func (hidden navHidden) set(page navPage, hiddenNow bool) navHidden {
	if page >= navPageCount {
		return hidden
	}
	if hiddenNow {
		return hidden | (navHidden(1) << page)
	}
	return hidden &^ (navHidden(1) << page)
}

// key is the name a hidden page keeps in the state file.
func (page navPage) key() string {
	switch page {
	case navPageHosts:
		return "hosts"
	case navPageWorkspaces:
		return "workspaces"
	case navPageMonitor:
		return "dashboard"
	case navPageSessionLogs:
		return "session-logs"
	case navPageKeychain:
		return "keychain"
	case navPageProxies:
		return "proxies"
	case navPageForwards:
		return "forwards"
	case navPageKnownHosts:
		return "known-hosts"
	case navPageSnippets:
		return "snippets"
	case navPageNotes:
		return "notes"
	}
	return ""
}

func navPageFromKey(key string) (navPage, bool) {
	for page := navPageHosts; page < navPageCount; page++ {
		if page.key() == key {
			return page, true
		}
	}
	return 0, false
}

// savedKeys lists the hidden pages in sidebar order, as the keys the state file
// holds. Hosts is never among them: it is the page the application opens on.
func (hidden navHidden) savedKeys() []string {
	var keys []string
	for page := navPageHosts; page < navPageCount; page++ {
		if page != navPageHosts && hidden.has(page) {
			keys = append(keys, page.key())
		}
	}
	return keys
}

// hiddenNavPages reads a saved hidden set. A key this build does not know is
// dropped, so a state file from a newer build loses nothing the user can see
// here, and hosts is never hidden.
func hiddenNavPages(keys []string) navHidden {
	var hidden navHidden
	for _, key := range keys {
		page, ok := navPageFromKey(strings.TrimSpace(key))
		if !ok || page == navPageHosts {
			continue
		}
		hidden = hidden.set(page, true)
	}
	return hidden
}

// sidebarPage is one page's row on the sidebar: the identity it answers a press
// to, its icon, and the section it shows.
type sidebarPage struct {
	page    navPage
	id      gfx.ID
	icon    uint8
	section uint8
}

// sidebarPages is the sidebar's fixed order. Hosts heads it and cannot be hidden:
// it is the page the application opens on. Settings is not here because it is
// declared beside the local-terminal row at the bottom, and is always shown.
var sidebarPages = [...]sidebarPage{
	{navPageHosts, hitHosts, iconHosts, sectionHosts},
	{navPageWorkspaces, hitWorkspaces, iconColumns2, sectionWorkspaces},
	{navPageMonitor, hitDashboard, iconActivity, sectionDashboard},
	{navPageSessionLogs, hitSessionLogs, iconFileClock, sectionSessionLogs},
	{navPageKeychain, hitKeychain, iconKey, sectionKeys},
	{navPageProxies, hitProxies, iconGlobe, sectionProxies},
	{navPageForwards, hitPortForwarding, iconForward, sectionForwards},
	{navPageKnownHosts, hitKnownHosts, iconKnown, sectionKnownHosts},
	{navPageSnippets, hitSnippets, iconNote, sectionSnippets},
	{navPageNotes, hitNotes, iconNotebook, sectionNotes},
}

// navPageLabel is a page's name as the sidebar and the settings row show it.
func (state *State) navPageLabel(page navPage) string {
	m := state.messages
	switch page {
	case navPageHosts:
		return m.NavHosts
	case navPageWorkspaces:
		return m.NavWorkspaces
	case navPageMonitor:
		return m.Dashboard.Title
	case navPageSessionLogs:
		return m.SessionLogs.Title
	case navPageKeychain:
		return m.NavKeychain
	case navPageProxies:
		return m.NavProxies
	case navPageForwards:
		return m.NavPortForwarding
	case navPageKnownHosts:
		return m.NavKnownHosts
	case navPageSnippets:
		return m.NavSnippets
	case navPageNotes:
		return state.noteText("Notes", "笔记")
	}
	return ""
}
