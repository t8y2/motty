package workbench

import (
	"errors"
	"slices"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSFTPWindowsPlacesHonorRedirectedFoldersAndEnumerateDriveLetters(t *testing.T) {
	places := windowsSFTPLocalPlaces(func(id *windows.KNOWNFOLDERID, flags uint32) (string, error) {
		if flags != windows.KF_FLAG_DONT_VERIFY {
			t.Fatal("known-folder enumeration could verify or create a folder")
		}
		if id == windows.FOLDERID_Documents {
			return `D:\OneDrive\文档`, nil
		}
		return "", errors.New("not configured")
	}, func() (uint32, error) { return 1<<2 | 1<<3 | 1<<4, nil })
	want := []sftpLocalPlace{{kind: localPlaceDocuments, path: `D:\OneDrive\文档`}, {kind: localPlaceDrive, path: `C:\`}, {kind: localPlaceDrive, path: `D:\`}, {kind: localPlaceDrive, path: `E:\`}}
	if !slices.Equal(places, want) {
		t.Fatalf("places=%+v, want %+v", places, want)
	}
	favorites := []string{`D:\Files`}
	if localFavoriteIndex(favorites, `d:\files\.`) != 0 {
		t.Fatal("Windows path aliases can create duplicate favorites")
	}
}
