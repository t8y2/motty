package workbench

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"motty/internal/keyidentity"
	"motty/internal/sshconn"
)

// The operation is an explicit user action, never exposed to external agents.
// Only the canonical public line enters the remote shell's stdin.
const hostKeyInstallScript = `set -eu
case "$(uname -s)" in Linux|Darwin|FreeBSD|OpenBSD|NetBSD|DragonFly|SunOS|AIX) ;; *) exit 21 ;; esac
case "${HOME:-}" in /*) ;; *) exit 22 ;; esac
IFS=" " read -r algorithm blob
[ -n "$algorithm" ] && [ -n "$blob" ] || exit 23
umask 077
dir="$HOME/.ssh"
file="$dir/authorized_keys"
lock="$dir/.motty-authorized-keys.lock"
[ ! -L "$dir" ] || exit 24
mkdir -p "$dir"
mkdir "$lock"
trap 'rmdir "$lock"' EXIT
[ ! -L "$file" ] || exit 24
if [ -e "$file" ]; then
    [ -f "$file" ] || exit 24
    # Match key material anywhere on the line to retain restrictions on an
    # existing key. A possible false match is safer than adding unrestricted
    # access. The independent login check still applies before switching.
    if awk -v a="$algorithm" -v b="$blob" '
        /^[[:space:]]*#/ { next }
        { for (i=1;i<NF;i++) if ($i==a && ($(i+1)==b || $(i+1)==b "\r")) found=1 }
        END { exit !found }
    ' "$file"; then
        printf '%s\n' motty-key-already-present
        exit 0
    else
        status=$?
        # awk's ordinary no-match code is 1; establish readability separately.
        [ "$status" -eq 1 ] && [ -r "$file" ] || exit 25
    fi
fi
if [ -s "$file" ]; then
    # Separate the new key even if the existing file has no final newline.
    printf '\n%s %s\n' "$algorithm" "$blob" >> "$file"
else
    printf '%s %s\n' "$algorithm" "$blob" >> "$file"
fi
printf '%s\n' motty-key-installed
`

var hostKeyInstallCommand = "sh -c '" + strings.ReplaceAll(hostKeyInstallScript, "'", "'\"'\"'") + "'"

type hostKeyDeployResult struct {
	HostID   uint32
	Imported bool
	Switched bool
	Error    string
	host     Host
	login    string
	route    connectionRoute
	verified bool
}

type hostKeyDeployment struct {
	running     bool
	results     []hostKeyDeployResult
	done        chan hostKeyDeployResult
	ref         string
	fingerprint string
	cancel      context.CancelFunc
	canceled    bool
}

type hostKeyDeployTask struct {
	host      Host
	config    sshconn.Config
	route     connectionRoute
	passwords map[sshconn.Endpoint]string
	secrets   passwordReader
	certified bool
}

func (state *State) startHostKeyDeployment(hostIDs []uint32, keyRef string, switchAuth bool) {
	if state.hostKeyDeploy.running || len(hostIDs) == 0 {
		return
	}
	deploy := &state.hostKeyDeploy
	*deploy = hostKeyDeployment{}
	var identity identityRecord
	for _, item := range state.identities {
		if item.Path == keyRef && item.Stored {
			identity = item
			break
		}
	}
	// Re-read the selected record in case sync has replaced or removed it
	// since the catalog originally rendered its public metadata.
	var publicLine string
	if state.secrets.vault != nil && sshconn.IsStoredKey(keyRef) && identity.Path != "" && !identity.SecurityKey {
		if record, err := readStoredKeyRecord(state.secrets.vault, keyRef); err == nil {
			if info, err := keyidentity.InspectData(keyRef, []byte(record.Key)); err == nil && !info.SecurityKey && info.Fingerprint == identity.Fingerprint {
				publicLine = info.PublicKey
				identity.Certificate = record.Certificate != ""
			}
		}
	}
	public, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(publicLine))
	if err == nil && public != nil {
		_, cert := public.(*ssh.Certificate)
		if cert || len(options) != 0 || len(strings.TrimSpace(string(rest))) != 0 || ssh.FingerprintSHA256(public) != identity.Fingerprint {
			err = errors.New("invalid stored public key")
		}
	}
	if err != nil || public == nil || !sshconn.IsStoredKey(keyRef) {
		for _, id := range hostIDs {
			deploy.results = append(deploy.results, hostKeyDeployResult{HostID: id, Error: "The selected SSH key is unavailable."})
		}
		return
	}
	line := ssh.MarshalAuthorizedKey(public)
	notify := state.options.Invalidate
	env := state.connectionEnv(nil)
	// Batch import must not execute a local program configured as a proxy.
	env.ProxyCommandRefused = errors.New("batch import does not run ProxyCommand")
	var tasks []hostKeyDeployTask
	seen := make(map[uint32]bool)
	for _, id := range hostIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		host, ok := state.hostByID(id)
		if !ok || !host.isSSH() || host.InteractiveOnly || host.mosh() || host.tunnel || host.CatalogID == "" || host.System == sshconn.SystemWindows {
			deploy.results = append(deploy.results, hostKeyDeployResult{HostID: id, Error: "Requires an SSH host that supports non-interactive Unix commands."})
			continue
		}
		config, route, valid := state.hostCatalog.sshConfigFor(host, host, env)
		config.NoProbes = true
		if !valid || len(config.Relays) != 0 || (config.Proxy != nil && config.Proxy.Err != nil) {
			deploy.results = append(deploy.results, hostKeyDeployResult{HostID: id, Error: "The connection route is unavailable or not supported for batch import."})
			continue
		}
		passwords := make(map[sshconn.Endpoint]string)
		target := sshconn.Endpoint{Host: config.Host, Port: config.Port, User: config.User}
		if target.Port == 0 {
			config.Authentication[deploymentEndpoint(target)] = config.Authentication[target]
		}
		passwords[deploymentEndpoint(target)] = state.hostCatalog.passwordSecret(host, target, sshconn.EndpointTarget)
		for _, jump := range config.ProxyJumps {
			if jump.Port == 0 {
				config.Authentication[deploymentEndpoint(jump)] = config.Authentication[jump]
			}
			passwords[deploymentEndpoint(jump)] = state.hostCatalog.passwordSecret(host, jump, sshconn.EndpointProxyJump)
		}
		tasks = append(tasks, hostKeyDeployTask{host: host, config: config, route: route, passwords: passwords, secrets: env.Passwords, certified: identity.Certificate})
	}
	if len(tasks) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan hostKeyDeployResult, len(tasks))
	deploy.running, deploy.done, deploy.ref, deploy.fingerprint, deploy.cancel = true, done, keyRef, identity.Fingerprint, cancel
	go func() {
		for _, task := range tasks {
			done <- task.deploy(ctx, line, keyRef, identity.Fingerprint, switchAuth)
			if notify != nil {
				notify()
			}
		}
		close(done)
		if notify != nil {
			notify()
		}
	}()
}

func (task hostKeyDeployTask) deploy(ctx context.Context, public []byte, keyRef, fingerprint string, switchAuth bool) hostKeyDeployResult {
	out := hostKeyDeployResult{HostID: task.host.ID, host: task.host, login: task.config.User, route: task.route}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	first, err := task.connect(ctx, task.config, false)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	result, err := first.RunCommandInput(ctx, hostKeyInstallCommand, public, 4096)
	first.Cancel()
	if err != nil {
		out.Error = "The server did not confirm installation in ~/.ssh/authorized_keys."
		return out
	}
	marker := strings.TrimSpace(string(result))
	if marker != "motty-key-installed" && marker != "motty-key-already-present" {
		out.Error = "The server did not confirm installation in ~/.ssh/authorized_keys."
		return out
	}
	out.Imported = true
	if !switchAuth {
		return out
	}

	if task.certified {
		out.Error = "Public key installed. This identity has a certificate attached; automatic switching is not supported."
		return out
	}

	// Open a separate transport. For the target, offer this exact raw key
	// only: no agent, default key, certificate, or password fallback.
	config := task.config
	target := sshconn.Endpoint{Host: config.Host, Port: config.Port, User: config.User}
	config.Authentication[deploymentEndpoint(target)] = sshconn.Authentication{
		Methods: sshconn.AuthPublicKey, IdentityFiles: []string{keyRef}, RequirePublicKey: true,
	}
	readKey := config.StoredKey
	config.StoredKey = func(ref string) ([]byte, error) {
		if readKey == nil {
			return nil, errors.New("stored key is unavailable")
		}
		private, err := readKey(ref)
		if err != nil || ref != keyRef {
			return private, err
		}
		info, err := keyidentity.InspectData(ref, private)
		if err != nil || info.Fingerprint != fingerprint || info.SecurityKey {
			clear(private)
			return nil, errors.New("stored key changed during verification")
		}
		return private, nil
	}
	readCertificate := config.StoredCertificate
	config.StoredCertificate = func(ref string) ([]byte, error) {
		if ref == keyRef || readCertificate == nil {
			return nil, nil
		}
		return readCertificate(ref)
	}
	verified, err := task.connect(ctx, config, true)
	if err != nil {
		out.Error = "Public key installed, but key-only login was not verified: " + err.Error()
		return out
	}
	verified.Cancel()
	out.verified = true
	return out
}

func (task hostKeyDeployTask) connect(ctx context.Context, config sshconn.Config, verify bool) (*sshconn.Conn, error) {
	if ctx.Err() != nil {
		return nil, errors.New("Canceled before connecting.")
	}
	ticks := make(chan struct{}, 1)
	config.Notify = func() {
		select {
		case ticks <- struct{}{}:
		default:
		}
	}
	conn := sshconn.Dial(config)
	fail := func(message string) (*sshconn.Conn, error) { conn.Cancel(); return nil, errors.New(message) }
	var version uint64
	for {
		if ctx.Err() != nil {
			return fail("Connection timed out or was canceled.")
		}
		status := conn.Status()
		if status.Version != version || status.Phase == sshconn.PhaseConnected {
			version = status.Version
			switch status.Phase {
			case sshconn.PhaseConnected:
				if verify && status.Method != "publickey" {
					return fail("The server did not authenticate with the selected public key.")
				}
				return conn, nil
			case sshconn.PhaseHostKeyPrompt:
				return fail("Host key is unknown or changed. Verify the fingerprint in a normal connection first.")
			case sshconn.PhasePasswordPrompt:
				if (verify && status.EndpointRole == sshconn.EndpointTarget) || status.Attempt > 1 || !isPasswordQuestion(status.Questions) {
					return fail("Login requires manual verification or the saved password was refused.")
				}
				secret := task.passwords[status.Endpoint]
				if secret == "" || task.secrets == nil {
					return fail("No saved login password is available.")
				}
				password, ok, err := task.secrets.Get(secret)
				if err != nil || !ok || password == "" {
					return fail("No saved login password is available.")
				}
				conn.SubmitPassword(password)
			case sshconn.PhaseKeyPassphrasePrompt:
				if task.secrets != nil && status.Attempt <= 1 {
					passphrase, ok, err := task.secrets.Get(passphraseSecretName(status.IdentityPath))
					if err == nil && ok && passphrase != "" {
						conn.SubmitKeyPassphrase(passphrase)
						break
					}
				}
				conn.SkipIdentity()
			case sshconn.PhaseFailed, sshconn.PhaseClosed:
				switch {
				case errors.Is(status.Err, sshconn.ErrHostKeyChanged):
					return fail("Host key changed. Verify the fingerprint in a normal connection first.")
				case errors.Is(status.Err, sshconn.ErrAuthFailed):
					return fail("SSH authentication failed.")
				default:
					return fail("SSH connection could not be established.")
				}
			}
		}
		select {
		case <-ctx.Done():
			return fail("Connection timed out or was canceled.")
		case <-ticks:
		}
	}
}

func (state *State) pollHostKeyDeployment() {
	deploy := &state.hostKeyDeploy
	for deploy.running {
		select {
		case result, more := <-deploy.done:
			if !more {
				deploy.cancel()
				deploy.running, deploy.done, deploy.cancel = false, nil, nil
				return
			}
			if result.verified && !deploy.canceled {
				result.Switched, result.Error = state.switchHostAfterKeyVerification(result, deploy.ref, deploy.fingerprint)
			} else if result.verified {
				result.Error = "Public key installed, but login settings were kept because the job was canceled."
			}
			deploy.results = append(deploy.results, result)
		default:
			return
		}
	}
}

func (state *State) switchHostAfterKeyVerification(result hostKeyDeployResult, ref, fingerprint string) (bool, string) {
	index := state.hostIndex(result.HostID)
	identity, ok := state.identityForFingerprint(fingerprint)
	if index < 0 || !ok || identity.Path != ref ||
		!reflect.DeepEqual(state.hosts[index], result.host) || state.hostCatalog.login(state.hosts[index]) != result.login {
		return false, "Public key installed, but the saved host or key changed during verification. Login settings were kept."
	}
	before := state.hosts[index]
	env := state.connectionEnv(nil)
	env.ProxyCommandRefused = errors.New("batch import does not run ProxyCommand")
	_, route, valid := state.hostCatalog.sshConfigFor(before, before, env)
	if !valid || !reflect.DeepEqual(route, result.route) {
		return false, "Public key installed, but the connection route changed during verification. Login settings were kept."
	}
	after := before
	after.Auth, after.Identity = authPrivateKey, fingerprint
	if before.Auth == after.Auth && before.Identity == after.Identity {
		return true, ""
	}
	state.replaceHost(index, before, after)
	if err := state.saveWithResult(); err != nil {
		if current := state.hostIndex(result.HostID); current >= 0 && reflect.DeepEqual(state.hosts[current], after) {
			state.replaceHost(current, after, before)
		}
		return false, "Public key installed and verified, but Motty could not save the login settings."
	}
	state.hostDataChanged()
	return true, ""
}

func deploymentEndpoint(e sshconn.Endpoint) sshconn.Endpoint {
	if e.Port == 0 {
		e.Port = 22
	}
	return e
}

func (state *State) cancelHostKeyDeployment() {
	deploy := &state.hostKeyDeploy
	if deploy.cancel != nil {
		deploy.canceled = true
		deploy.cancel()
	}
}
