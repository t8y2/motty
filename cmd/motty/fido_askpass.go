//go:build darwin || windows || linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"motty/internal/fidoaskpass"
)

// runFidoAskpass is the SSH_ASKPASS helper OpenSSH starts for a security key.
// It carries one prompt to the running window and prints the answer that comes
// back; it never opens a window and never writes the answer anywhere else.
func runFidoAskpass(args []string) int {
	return fidoAskpass(args, os.Stdout)
}

// fidoAskpass is runFidoAskpass with an explicit answer stream, so a test can
// read the answer without replacing the process's stdout.
func fidoAskpass(args []string, out io.Writer) int {
	// OpenSSH sets SSH_ASKPASS_PROMPT=none for passive presence notices
	// ("Confirm user presence..."). Motty already displays touch status via
	// Status.SecurityKey, and opening the PIN/passphrase form here would block
	// or outlive the touch prompt.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SSH_ASKPASS_PROMPT")), "none") {
		return 0
	}
	prompt := strings.TrimSpace(strings.Join(args, " "))
	if prompt == "" {
		prompt = strings.TrimSpace(os.Getenv("SSH_ASKPASS_PROMPT"))
	}
	address := os.Getenv("MOTTY_FIDO_ASKPASS_SOCKET")
	token := os.Getenv("MOTTY_FIDO_ASKPASS_TOKEN")
	if address == "" || token == "" {
		return 1
	}
	connection, err := fidoaskpass.Dial(address)
	if err != nil {
		return 1
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(3 * time.Minute))
	if err := json.NewEncoder(connection).Encode(fidoaskpass.Request{Token: token, Prompt: prompt}); err != nil {
		return 1
	}
	var response fidoaskpass.Response
	if err := json.NewDecoder(connection).Decode(&response); err != nil || !response.OK {
		return 1
	}
	fmt.Fprintln(out, response.Response)
	return 0
}
