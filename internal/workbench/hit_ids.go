package workbench

import "github.com/vivaui/viva/gfx"

// tree names every place the workbench can be pointed at. A page or overlay is a
// feature of it, a list on a page is a scope of that feature, and a control is a
// name in one of them. Nothing is numbered: two features cannot answer to the
// same click, a list has as many rows as it has data, and a handler asks which
// place an identity is in rather than comparing it against a range.
//
//	var proxiesHits = tree.Feature("proxies")
//	var hitProxyAdd = tree.ID(proxiesHits, "add")
//	var hitProxyRows = tree.Sub(proxiesHits, tree.Name("rows"))   // hitProxyRows.At(i)
//
// An embedded session page is mounted in the pane that paints it with
// tree.Mount, so the same controls are different identities in different panes.
var tree gfx.Tree

// Tree is the identity tree of the workbench's controls, for the window that paints
// them: identities print as paths in logs and in the headless driver.
func Tree() *gfx.Tree { return &tree }

// The pages and overlays whose controls workbench.go declares.
var (
	shellHits      = tree.Feature("shell")
	keysHits       = tree.Feature("keys")
	forwardsHits   = tree.Feature("forwards")
	settingsHits   = tree.Feature("settings")
	proxiesHits    = tree.Feature("proxies")
	knownHostsHits = tree.Feature("known-hosts")
	sftpHits       = tree.Feature("sftp")
	vaultHits      = tree.Feature("vault")

	// connHits holds the controls of the connection page of a tab: the log, the
	// progress, retrying and closing.
	connHits = tree.Sub(shellHits, tree.Name("connection"))
)

// Where an embedded session page is painted. Every pane paints the same page with
// the same controls; mounting it in the pane's place makes them that pane's own
// identities (see Tree.Mount), and the context panel is a mount of its own.
var (
	panesHits     = tree.Feature("panes")
	contextMount  = tree.Feature("context")
	fileTabsHits  = tree.Feature("file-tabs")
	filePagesHits = tree.Feature("file-pages")

	fileTabList      = tree.Sub(fileTabsHits, tree.Name("tab"))
	fileTabCloseList = tree.Sub(fileTabsHits, tree.Name("tab-close"))
)

func sftpFileTabID(sessionID uint32) gfx.ID {
	return fileTabList.At(int(sessionID))
}

func sftpFileTabCloseID(sessionID uint32) gfx.ID {
	return fileTabCloseList.At(int(sessionID))
}

func sftpWindowMount(window *sftpWindow) gfx.Scope {
	return tree.Sub(filePagesHits, gfx.Index(int(window.session.ID)))
}

// paneMount is the place the page of a session is mounted at when it is painted
// in one of several panes.
func paneMount(session *Session) gfx.Scope {
	return tree.Sub(panesHits, gfx.Index(int(session.paneSlot)))
}

// contextID is the identity a control of the files panel has in the window.
func contextID(id gfx.ID) gfx.ID { return tree.MountID(contextMount, id) }

// item is the number of the list item an identity names, or zero for a control.
func item(id gfx.ID) int {
	n, _ := id.Name().Index()
	return n
}

// dialogHits is the place of a modal dialog's focus scope. Only one dialog is
// open at a time, so they share it; the scope is what Tab stays inside.
var (
	dialogHits  = tree.Feature("dialog")
	dialogScope = tree.ID(dialogHits, "scope")
)
