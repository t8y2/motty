package workbench

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// XDG values are paths, never commands. Expand only HOME; do not evaluate shell
// expressions or other variables from the user's directory configuration.
func unixSFTPLocalPlaces(home, system string, userDirs []byte) []sftpLocalPlace {
	var places []sftpLocalPlace
	if home != "" {
		places = append(places, sftpLocalPlace{kind: localPlaceHome, path: home})
		names := []string{"Desktop", "Downloads", "Documents", "Pictures", "Music", "Videos"}
		kinds := []sftpLocalPlaceKind{localPlaceDesktop, localPlaceDownloads, localPlaceDocuments, localPlacePictures, localPlaceMusic, localPlaceVideos}
		if system == "darwin" {
			// Finder calls this folder Movies (影片), not Videos.
			names[5], kinds[5] = "Movies", localPlaceMovies
		}
		overrides := map[string]string{}
		if system == "linux" {
			for _, line := range strings.Split(string(userDirs), "\n") {
				key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
				if !ok {
					continue
				}
				key = strings.TrimSpace(key)
				value, err := strconv.Unquote(strings.TrimSpace(value))
				if err != nil {
					continue
				}
				if value == "$HOME" || value == "${HOME}" {
					overrides[key] = ""
					continue
				}
				for _, prefix := range []string{"$HOME/", "${HOME}/"} {
					if strings.HasPrefix(value, prefix) {
						value = filepath.Join(home, value[len(prefix):])
						break
					}
				}
				if filepath.IsAbs(value) && !strings.ContainsAny(value, "$\x00") {
					overrides[key] = filepath.Clean(value)
				}
			}
		}
		keys := []string{"DESKTOP", "DOWNLOAD", "DOCUMENTS", "PICTURES", "MUSIC", "VIDEOS"}
		for index, name := range names {
			directory := filepath.Join(home, name)
			if configured, ok := overrides["XDG_"+keys[index]+"_DIR"]; ok {
				directory = configured
			}
			if directory == "" || directory == home {
				continue
			}
			if info, err := os.Stat(directory); err == nil && info.IsDir() {
				places = append(places, sftpLocalPlace{kind: kinds[index], path: directory})
			}
		}
	}
	if system == "darwin" {
		// Finder keeps Applications in its reach; it is not under the home folder.
		if info, err := os.Stat("/Applications"); err == nil && info.IsDir() {
			places = append(places, sftpLocalPlace{kind: localPlaceApplications, path: "/Applications"})
		}
	}
	return append(places, sftpLocalPlace{kind: localPlaceDrive, path: "/"})
}
