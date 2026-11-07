package workbench

import (
	"path"
	"path/filepath"
)

// splitDirectory is the directory a shell split off this session starts in: where
// this shell last reported being, or the directory a restored shell will use
// when it reconnects. Empty means the new shell starts where a new shell does.
func (session *Session) splitDirectory() string {
	if session.isLocal() {
		return session.localDirectory
	}
	if session.containerCommand != "" {
		return ""
	}
	if session.term == nil {
		if directory := path.Clean(session.startDirectory); validSyncDirectory(directory) {
			return directory
		}
		return ""
	}
	if directory := path.Clean(session.term.reportedDirectory.Path); validSyncDirectory(directory) {
		return directory
	}
	if directory := path.Clean(session.startDirectory); validSyncDirectory(directory) {
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
		session.startDirectory = path.Clean(directory)
	}
}
