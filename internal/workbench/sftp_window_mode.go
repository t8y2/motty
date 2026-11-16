package workbench

// sftpWindowOrder controls only which pane is painted first. The local and
// remote state, transfer direction and hit identities stay attached to their
// logical sides.
type sftpWindowOrder uint8

const (
	sftpLocalLeft sftpWindowOrder = iota
	sftpRemoteLeft
)

func sftpWindowOrderFromString(value string) sftpWindowOrder {
	if value == "remote-left" {
		return sftpRemoteLeft
	}
	return sftpLocalLeft
}

func (order sftpWindowOrder) String() string {
	if order == sftpRemoteLeft {
		return "remote-left"
	}
	return "local-left"
}

// sftpOpenMode is read only when a manager is opened. Changing it therefore
// leaves managers that are already open in their current host.
type sftpOpenMode uint8

const (
	sftpOpenWindow sftpOpenMode = iota
	sftpOpenTab
)

func sftpOpenModeFromString(value string) sftpOpenMode {
	if value == "tab" {
		return sftpOpenTab
	}
	return sftpOpenWindow
}

func (mode sftpOpenMode) String() string {
	if mode == sftpOpenTab {
		return "tab"
	}
	return "window"
}
