#!/bin/sh
# Runs the four authentication end-to-end scripts against throwaway e2esshd
# fixtures on loopback: ssh-agent forwarding, OpenSSH user certificates,
# host certificates trusted through @cert-authority, and PuTTY PPK identities.
#
#   tools/e2e/run_auth_e2e.sh
#
# Everything stays on this machine: the servers listen on 127.0.0.1 only, the
# agent is a throwaway process, and every motty run gets empty temporary
# directories. A failed step fails the script.
set -e
cd "$(dirname "$0")/../.."

head=${MOTTY_HEAD:-/tmp/motty-head}
[ -x "$head" ] || go build -tags vivahead -o "$head" ./cmd/motty
server=/tmp/motty-e2esshd
[ -x "$server" ] || (cd tools/e2esshd && go build -o "$server" .)

work=$(mktemp -d)
cleanup() {
	[ -n "${fixture_pid:-}" ] && kill "$fixture_pid" 2>/dev/null
	[ -n "${agent_pid:-}" ] && kill "$agent_pid" 2>/dev/null
	rm -rf "$work"
}
trap cleanup EXIT

start_fixture() { # port, further env as VAR=value pairs
	port=$1
	shift
	env "$@" "$server" "$port" >"$work/fixture-$port.log" 2>&1 &
	fixture_pid=$!
}

fixture_line() { # port, prefix
	grep -m1 "^$2 " "$work/fixture-$1.log" | cut -d" " -f2-
}

wait_listening() { # port
	i=0
	while ! grep -q "^listening on" "$work/fixture-$1.log" 2>/dev/null; do
		i=$((i + 1))
		if [ "$i" -gt 100 ]; then cat "$work/fixture-$1.log"; exit 1; fi
		sleep 0.05
	done
}

run_script() { # script file
MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" "$1"
}

echo "== agent forwarding =="
ssh-keygen -q -t ed25519 -N "" -C agent-forward-key -f "$work/agent-key"
eval "$(ssh-agent -s)" >/dev/null
agent_pid=$SSH_AGENT_PID
ssh-add "$work/agent-key" >/dev/null
start_fixture 2244 E2E_AGENTFWD=1
wait_listening 2244
SSH_AUTH_SOCK="$SSH_AUTH_SOCK" run_script tools/e2e/agent_forward_e2e.txt
kill "$fixture_pid" "$agent_pid" 2>/dev/null || true
unset agent_pid

echo "== user certificate =="
start_fixture 2245 E2E_USER_CERT=1
wait_listening 2245
cp "$(fixture_line 2245 identity)" /tmp/motty-e2e-identity
cp "$(fixture_line 2245 user-cert)" /tmp/motty-e2e-identity-cert.pub
run_script tools/e2e/user_cert_e2e.txt
kill "$fixture_pid" 2>/dev/null || true

echo "== host certificate =="
start_fixture 2246 E2E_HOST_CERT=1
wait_listening 2246
home=$(mktemp -d)
mkdir -p "$home/motty-data"
echo "@cert-authority [127.0.0.1]:2246 $(fixture_line 2246 host-ca)" >"$home/motty-data/known_hosts"
MOTTY_LANG=en python3 tools/headless_drive.py "$home" tools/e2e/host_cert_e2e.txt
rm -rf "$home"
kill "$fixture_pid" 2>/dev/null || true

echo "== ppk identity =="
cat >"$work/ppkwrite.go" <<'GO'
// Writes an unencrypted PPK version 2 file for a fresh ed25519 key, plus its
// authorized_keys public line, following the format ppk.go parses.
package main

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
)

func sshString(b []byte) []byte {
	out := make([]byte, 4+len(b))
	out[0], out[1], out[2], out[3] = byte(len(b)>>24), byte(len(b)>>16), byte(len(b)>>8), byte(len(b))
	copy(out[4:], b)
	return out
}

func main() {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	pubBlob := append(sshString([]byte("ssh-ed25519")), sshString(public)...)
	// chunked splits base64 into 64-column lines and reports their count.
	chunked := func(data []byte) (string, int) {
		encoded := base64.StdEncoding.EncodeToString(data)
		out := ""
		for len(encoded) > 64 {
			out += encoded[:64] + "\n"
			encoded = encoded[64:]
		}
		if encoded != "" {
			out += encoded + "\n"
		}
		return out, (base64.StdEncoding.EncodedLen(len(data)) + 63) / 64
	}
	pubBlock, pubLines := chunked(pubBlob)
	privBlock, privLines := chunked(private[:32])
	body := "PuTTY-User-Key-File-2: ssh-ed25519\nEncryption: none\nComment: e2e-ppk\n" +
		fmt.Sprintf("Public-Lines: %d\n", pubLines) + pubBlock +
		fmt.Sprintf("Private-Lines: %d\n", privLines) + privBlock
	macInput := append(sshString([]byte("ssh-ed25519")), sshString([]byte("none"))...)
	macInput = append(macInput, sshString([]byte("e2e-ppk"))...)
	macInput = append(macInput, sshString(pubBlob)...)
	macInput = append(macInput, sshString(private[:32])...)
	key := sha1.Sum([]byte("putty-private-key-file-mac-key"))
	mac := hmac.New(sha1.New, key[:])
	mac.Write(macInput)
	body += "Private-MAC: " + fmt.Sprintf("%x", mac.Sum(nil)) + "\n"
	if err := os.WriteFile("/tmp/motty-e2e.ppk", []byte(body), 0o600); err != nil {
		panic(err)
	}
	pubLine := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(pubBlob) + " e2e-ppk\n"
	if err := os.WriteFile("/tmp/motty-e2e.pub", []byte(pubLine), 0o644); err != nil {
		panic(err)
	}
}
GO
go run "$work/ppkwrite.go"
start_fixture 2247 E2E_EXTRA_PUBKEY=/tmp/motty-e2e.pub
wait_listening 2247
run_script tools/e2e/ppk_e2e.txt
kill "$fixture_pid" 2>/dev/null || true

echo "all four authentication paths verified"
