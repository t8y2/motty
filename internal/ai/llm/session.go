package llm

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// openCodeSessionHeader names the conversation a request belongs to. OpenCode
// Zen and Go route on it for prompt cache affinity, and the Go plan rejects a
// request without it (400 MissingSessionID) before looking at the key.
const openCodeSessionHeader = "x-opencode-session"

// NewSessionID returns a fresh opaque conversation identifier in the ses_
// form OpenCode itself uses.
func NewSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "ses_" + hex.EncodeToString(b[:])
}

// isOpenCodeHost reports whether host is opencode.ai or one of its subdomains.
func isOpenCodeHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "opencode.ai" || strings.HasSuffix(host, ".opencode.ai")
}

// sessionHeader adds the conversation identifier OpenCode needs to a request
// bound for it. Other endpoints never see the header, so a conversation
// identifier does not leak to unrelated providers. A request without a
// SessionID (a one-off probe, say) gets a fresh one so it is still accepted.
func sessionHeader(r *http.Request, id string) {
	if r.URL == nil || !isOpenCodeHost(r.URL.Hostname()) {
		return
	}
	if id = strings.TrimSpace(id); id == "" {
		id = NewSessionID()
	}
	r.Header.Set(openCodeSessionHeader, id)
}
