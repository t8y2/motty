package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"motty/internal/ai/store"
)

type AuthState string

const (
	AuthUnknown    AuthState = "unknown"
	AuthSignedOut  AuthState = "signed_out"
	AuthConfigured AuthState = "configured"
	AuthChecking   AuthState = "checking"
	AuthSigningIn  AuthState = "signing_in"
	AuthFailed     AuthState = "failed"
	AuthCanceled   AuthState = "canceled"
)

// AuthCommand is an explicit user-initiated local operation, not an AI tool.
type AuthCommand struct {
	Program string
	Args    []string
}

var loginReservations = struct {
	sync.Mutex
	active map[string]bool
}{active: make(map[string]bool)}

// ReserveLogin serializes official auth operations across windows sharing a
// CLI home. The returned release is idempotent, including stale completions.
func ReserveLogin(kind, path string) (func(), error) {
	c, err := resolve(kind, path)
	if err != nil {
		return nil, err
	}
	program := c.program
	if canonical, err := filepath.EvalSymlinks(program); err == nil {
		program = canonical
	}
	key := jsonString([]string{kind, program, jsonString(c.args), os.Getenv("HOME"), os.Getenv("CODEX_HOME"), os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv("XDG_CONFIG_HOME"), os.Getenv("XDG_DATA_HOME")})
	loginReservations.Lock()
	defer loginReservations.Unlock()
	if loginReservations.active[key] {
		return nil, errors.New("cli: login_in_progress")
	}
	loginReservations.active[key] = true
	var once sync.Once
	return func() {
		once.Do(func() { loginReservations.Lock(); delete(loginReservations.active, key); loginReservations.Unlock() })
	}, nil
}

func LoginCommand(kind, path string, device bool) (AuthCommand, error) {
	c, err := resolve(kind, path)
	if err != nil {
		return AuthCommand{}, err
	}
	args := []string{"auth", "login"}
	if kind == store.KindCodexCLI {
		args = []string{"login"}
	}
	if device {
		if kind != store.KindCodexCLI {
			return AuthCommand{}, errors.New("cli: unsupported_auth")
		}
		args = append(args, "--device-auth")
	}
	if kind == store.KindPiCLI {
		// Pi's `auth login` requires a provider on the command line, and its own
		// interface is where a provider is chosen. Starting Pi opens that UI, so
		// the embedded terminal can run /login.
		args = nil
	}
	return AuthCommand{Program: c.program, Args: append(c.args, args...)}, nil
}

func CheckAuth(parent context.Context, kind, path string) (AuthState, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	c, err := resolve(kind, path)
	if err != nil {
		return AuthUnknown, err
	}
	switch kind {
	case store.KindClaudeCLI:
		c.args = append(c.args, "auth", "status", "--json")
	case store.KindCodexCLI:
		c.args = append(c.args, "login", "status")
	case store.KindOpenCodeCLI:
		// An auth-list entry is not proof that the selected provider is usable.
		return AuthUnknown, nil
	case store.KindPiCLI:
		// Pi checks credentials for a named provider, and this provider entry
		// selects only "default", so there is no account state to report.
		return AuthUnknown, nil
	default:
		return AuthUnknown, errors.New("cli: unsupported_provider")
	}
	out := &boundedWriter{limit: 1 << 20, strict: true, cancel: cancel}
	stderr, err := runCommand(ctx, c, out)
	if ctx.Err() != nil {
		return AuthUnknown, ctx.Err()
	}
	if err != nil && err.Error() == "cli: login_required" {
		return AuthSignedOut, nil
	}
	state := classifyAuth(kind, out.data, stderr, err != nil)
	return state, nil
}

func classifyAuth(kind string, stdout, stderr []byte, failed bool) AuthState {
	if kind == store.KindClaudeCLI {
		var status struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if json.Unmarshal(stdout, &status) == nil && status.LoggedIn != nil {
			if *status.LoggedIn && !failed {
				return AuthConfigured
			}
			if !*status.LoggedIn {
				return AuthSignedOut
			}
		}
	}
	if kind == store.KindCodexCLI {
		text := strings.ToLower(string(stdout) + string(stderr))
		if strings.Contains(text, "not logged in") {
			return AuthSignedOut
		}
		if !failed && strings.Contains(text, "logged in using") {
			return AuthConfigured
		}
	}
	return AuthUnknown
}
