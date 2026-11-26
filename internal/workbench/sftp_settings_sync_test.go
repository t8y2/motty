package workbench

import "testing"

// The file panel's display choices are shared like the other settings: a
// computer that changes how the transfer panes lay out, open and list files
// changes it everywhere. Paths stay with the computer they name.
func TestFilePanelDisplayChoicesSyncToAnotherComputer(t *testing.T) {
	server, _ := newFakeDAV(t)
	source, _, _ := backupTestState(t)
	source.sftpWindowOrder = sftpRemoteLeft
	source.sftpOpenMode = sftpOpenTab
	source.sftpDirectoryTree = true
	source.sftpColumns.setEnabled(sftpColumnModified, false)
	source.sftpDownloadDirectory = "/source/downloads"
	source.save()
	configureSync(t, source, server)
	syncAndWait(t, source, hitSyncNow, source.messages.Tools.SyncDone)

	target, _, _ := backupTestState(t)
	target.sftpDownloadDirectory = "/target/downloads"
	target.save()
	configureSync(t, target, server)
	syncAndWait(t, target, hitSyncNow, target.messages.Tools.SyncDone)

	if target.sftpWindowOrder != sftpRemoteLeft || target.sftpOpenMode != sftpOpenTab || !target.sftpDirectoryTree {
		t.Fatalf("file panel choices did not reach the other computer: order %v, open %v, tree %v",
			target.sftpWindowOrder, target.sftpOpenMode, target.sftpDirectoryTree)
	}
	if target.sftpColumns.enabled(sftpColumnModified) {
		t.Fatal("the file list's column choice did not reach the other computer")
	}
	if target.sftpDownloadDirectory != "/target/downloads" {
		t.Fatalf("a download folder named by another computer replaced this one: %q", target.sftpDownloadDirectory)
	}
}
