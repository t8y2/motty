package rdp

import (
	"testing"

	"motty/internal/desktop"
)

// A session can give the remote the picture on this computer's clipboard; a host
// that shares no clipboard never sends one.
func TestSessionIsAnImageClipboard(t *testing.T) {
	var _ desktop.ImageClipboard = (*Session)(nil)
	session := &Session{}
	if err := session.SetClipboardImage([]byte{1, 2, 3}); err != nil {
		t.Fatalf("an image was refused for a host that shares nothing: %v", err)
	}
}
