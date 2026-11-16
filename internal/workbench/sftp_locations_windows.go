package workbench

import "golang.org/x/sys/windows"

func nativeSFTPLocalPlaces() []sftpLocalPlace {
	return windowsSFTPLocalPlaces(windows.KnownFolderPath, windows.GetLogicalDrives)
}

func windowsSFTPLocalPlaces(knownFolder func(*windows.KNOWNFOLDERID, uint32) (string, error), logicalDrives func() (uint32, error)) []sftpLocalPlace {
	ids := []*windows.KNOWNFOLDERID{windows.FOLDERID_Profile, windows.FOLDERID_Desktop, windows.FOLDERID_Downloads, windows.FOLDERID_Documents, windows.FOLDERID_Pictures, windows.FOLDERID_Music, windows.FOLDERID_Videos}
	var places []sftpLocalPlace
	for kind, id := range ids {
		// Honor relocated/OneDrive folders without verifying or creating them.
		if directory, err := knownFolder(id, windows.KF_FLAG_DONT_VERIFY); err == nil && directory != "" && validBookmarkLocal(directory) {
			places = append(places, sftpLocalPlace{kind: sftpLocalPlaceKind(kind), path: directory})
		}
	}
	if mask, err := logicalDrives(); err == nil {
		for letter := 0; letter < 26; letter++ {
			if mask&(1<<letter) != 0 {
				places = append(places, sftpLocalPlace{kind: localPlaceDrive, path: string(rune('A'+letter)) + `:\`})
			}
		}
	}
	return places
}
