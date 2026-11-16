package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/vivaui/viva/app"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// An image on the clipboard cannot travel through a terminal, but programs
// can read it from a path. Paste saves a private local copy or uploads it to an
// SSH server and types the path of the copy.

// pasteImageHits is the place of the controls this file paints.
var pasteImageHits = tree.Feature("paste-image")

var (
	hitSettingsPasteImageToggle = tree.ID(pasteImageHits, "settings-paste-image")
)

const (
	// pasteImageDir is where the copies go, under the server's home directory.
	pasteImageDir = ".cache/motty/paste"
	// pasteImageKeep is how long a copy stays; older ones are removed by the next
	// upload.
	pasteImageKeep = 7 * 24 * time.Hour
)

func (state *State) setPasteImage(on bool) bool {
	if state.pasteImageOff == !on {
		return false
	}
	state.pasteImageOff = !on
	return true
}

// canPasteImage reports whether a paste can give the shell a reachable picture.
// A container's shell sees another file system than the one the upload reaches.
func (state *State) canPasteImage(session *Session) bool {
	return !state.pasteImageOff && (session.isLocal() || session.hasServer() && session.conn != nil &&
		session.phase == sshconn.PhaseConnected && session.containerCommand == "")
}

// pasteClipboardImage saves the image on the clipboard and reports whether it
// took the paste over: false when there is nothing for it to do, so the paste
// stays what it was. The clipboard is read when the paste is asked for, not
// before, as asking another program what it holds can take a while.
func (state *State) pasteClipboardImage(session *Session) bool {
	if !state.canPasteImage(session) {
		return false
	}
	m := state.messages.Tools
	if session.pastingImage {
		state.status = m.PasteImageUploading
		return true
	}
	data, err := state.options.ClipboardImagePNG()
	switch {
	case errors.Is(err, app.ErrNoImage):
		return false
	case errors.Is(err, app.ErrTooLarge):
		state.status = m.PasteImageTooLarge
		return true
	case err != nil:
		state.status = fmt.Sprintf(m.PasteImageFailedFmt, err)
		return true
	}
	session.pastingImage = true
	state.status = m.PasteImageUploading
	id, connection, transfer, local := session.ID, session.conn, session.host.Transfer, session.isLocal()
	go func() {
		var target string
		var err error
		if local {
			target, err = saveLocalPastedImage(data, time.Now())
		} else {
			target, err = uploadPastedImage(connection, transfer, data, time.Now())
		}
		state.aiPost(func(state *State) { state.finishPasteImage(id, target, err) })
	}()
	return true
}

func saveLocalPastedImage(png []byte, now time.Time) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(cache, "motty", "paste")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, "paste-"+now.Format("20060102-150405")+"-*.png")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err = file.Write(png); err == nil {
		err = file.Close()
	} else {
		file.Close()
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	entries, readErr := os.ReadDir(directory)
	if readErr == nil {
		for _, entry := range entries {
			if !isPastedImage(entry.Name()) || !entry.Type().IsRegular() || entry.Name() == filepath.Base(name) {
				continue
			}
			if info, err := entry.Info(); err == nil && info.ModTime().Before(now.Add(-pasteImageKeep)) {
				os.Remove(filepath.Join(directory, entry.Name()))
			}
		}
	}
	return name, nil
}

// uploadPastedImage puts one picture on the server and returns its path there.
// The copy is private to the account and the directory is tidied on the way.
func uploadPastedImage(connection Connection, transfer transferMode, png []byte, now time.Time) (string, error) {
	files, err := openRemoteFiles(connection, transfer)
	if err != nil {
		return "", err
	}
	defer files.Close()
	home, err := files.Getwd()
	if err != nil {
		return "", err
	}
	directory, err := sftpclient.EnsurePrivateDir(files, home, pasteImageDir)
	if err != nil {
		return "", err
	}
	var suffix [3]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	name := "paste-" + now.Format("20060102-150405") + "-" + hex.EncodeToString(suffix[:]) + ".png"
	target := path.Join(directory, name)
	if err := sftpclient.WritePrivateFile(files, target, png); err != nil {
		return "", err
	}
	// Tidying is a courtesy; the upload has worked whether or not it does. The
	// new file is never a candidate, whatever the server's clock says.
	_, _ = sftpclient.PruneFiles(files, directory, func(other string) bool { return other != name && isPastedImage(other) }, now.Add(-pasteImageKeep))
	return target, nil
}

func isPastedImage(name string) bool {
	return strings.HasPrefix(name, "paste-") && strings.HasSuffix(name, ".png")
}

// finishPasteImage types the path of an uploaded picture into the terminal it was
// pasted in, or says why there is none.
func (state *State) finishPasteImage(sessionID uint32, remote string, err error) {
	m := state.messages.Tools
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok {
		return
	}
	session.pastingImage = false
	if err != nil {
		state.status = fmt.Sprintf(m.PasteImageFailedFmt, err)
		return
	}
	if session.isLocal() {
		state.status = ""
	} else {
		state.status = fmt.Sprintf(m.PasteImageDoneFmt, remote)
	}
	if session.term == nil || session.term.shell == nil {
		return
	}
	// A space follows, as it does when a file is dropped on a terminal, so the
	// next word is not part of the path.
	quoted := shellEscapePath(remote)
	if session.isLocal() && (session.wslDistro() != "" || filepath.Separator == '\\') {
		quoted = quotedPaths([]string{remote}, session.wslDistro())
	}
	state.typed(session, encodeTerminalPaste(quoted+" ", session.term))
}

// shellEscapePath writes a path so a shell and the programs that read dropped
// files take it as one word: a backslash before every character that is not a
// plain path character.
func shellEscapePath(name string) string {
	var out strings.Builder
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("/._-+:@%,=~", r) {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}
