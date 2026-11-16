package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSFTPUnixPlacesHonorMacMoviesAndLocalizedXDGDirectories(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"Desktop", "Downloads", "Documents", "Pictures", "Music", "Videos", "Movies", "中文文档", "自定义下载"} {
		if err := os.Mkdir(filepath.Join(home, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	find := func(places []sftpLocalPlace, kind sftpLocalPlaceKind) string {
		for _, place := range places {
			if place.kind == kind {
				return place.path
			}
		}
		return ""
	}
	mac := unixSFTPLocalPlaces(home, "darwin", nil)
	if find(mac, localPlaceMovies) != filepath.Join(home, "Movies") || find(mac, localPlaceVideos) != "" || find(mac, localPlaceDocuments) != filepath.Join(home, "Documents") || find(mac, localPlaceDrive) != "/" {
		t.Fatal("macOS quick access is missing standard directories or names Movies as Videos", mac)
	}
	if runtime.GOOS == "darwin" {
		if info, err := os.Stat("/Applications"); err == nil && info.IsDir() && find(mac, localPlaceApplications) != "/Applications" {
			t.Fatal("macOS quick access is missing Applications", mac)
		}
	}
	linux := unixSFTPLocalPlaces(home, "linux", []byte("# paths only\nXDG_DOCUMENTS_DIR=\"$HOME/中文文档\"\nXDG_DOWNLOAD_DIR=\"${HOME}/自定义下载\"\nXDG_DESKTOP_DIR=\"$HOME\"\nXDG_VIDEOS_DIR=\"relative/path\"\n"))
	if find(linux, localPlaceDocuments) != filepath.Join(home, "中文文档") || find(linux, localPlaceDownloads) != filepath.Join(home, "自定义下载") || find(linux, localPlaceDesktop) != "" || find(linux, localPlaceVideos) != filepath.Join(home, "Videos") {
		t.Fatal("Linux XDG overrides or disabled directories were ignored", linux)
	}
	if find(unixSFTPLocalPlaces(home, "linux", []byte("XDG_DOCUMENTS_DIR=\"$HOME/$(run-command)\"\n")), localPlaceDocuments) != filepath.Join(home, "Documents") {
		t.Fatal("a shell expression was treated as a shortcut")
	}
}
