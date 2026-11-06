package workbench

import "path/filepath"

// splitDirectory is the directory a shell split off this session starts in: where
// this shell last reported being, which needs its prompt integration to have
// reported anything. Empty means the new shell starts where a new shell does.
func (session *Session) splitDirectory() string {
	if session.isLocal() {
		return session.localDirectory
	}
	if session.term == nil || session.containerCommand != "" {
		return ""
	}
	if directory := session.term.reportedDirectory.Path; validSyncDirectory(directory) {
		return directory
	}
	return ""
}

// setStartDirectory makes directory the place this session's shell starts. A
// local shell takes it as its remembered folder, which is process startup data;
// a remote one asks the server for it when the shell opens. A path that cannot
// be one is ignored.
func (session *Session) setStartDirectory(directory string) {
	switch {
	case directory == "":
	case session.isLocal():
		if validLocalDirectory(directory) {
			session.localDirectory = filepath.Clean(directory)
		}
	case validSyncDirectory(directory):
		session.startDirectory = directory
	}
}
