package rdp

import (
	"testing"

	"motty/internal/desktop"
	"motty/internal/rdpcore/protocol/cliprdr"
)

// A session can give the remote files, and the limit the workbench tells the user
// is the one the protocol layer enforces.
func TestSessionIsAFileClipboardWithTheProtocolsLimit(t *testing.T) {
	var _ desktop.FileClipboard = (*Session)(nil)
	if desktop.MaxOfferFiles != cliprdr.MaxOfferEntries {
		t.Fatalf("desktop.MaxOfferFiles = %d, but an offer holds %d entries", desktop.MaxOfferFiles, cliprdr.MaxOfferEntries)
	}
}
