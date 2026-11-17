//go:build darwin || windows || linux

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"motty/internal/fidoaskpass"
)

func TestFidoAskpassCarriesThePromptAndAnswer(t *testing.T) {
	dir, err := os.MkdirTemp("", "motty-ask-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, address, err := fidoaskpass.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("MOTTY_FIDO_ASKPASS_SOCKET", address)
	t.Setenv("MOTTY_FIDO_ASKPASS_TOKEN", "token")

	request := make(chan fidoaskpass.Request, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		var incoming fidoaskpass.Request
		if err := json.NewDecoder(connection).Decode(&incoming); err != nil {
			return
		}
		request <- incoming
		_ = json.NewEncoder(connection).Encode(fidoaskpass.Response{OK: true, Response: "123456"})
	}()

	var output bytes.Buffer
	if code := fidoAskpass([]string{"Enter", "PIN", "for", "id_sk"}, &output); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if output.String() != "123456\n" {
		t.Fatalf("output = %q", output.String())
	}
	incoming := <-request
	if incoming.Token != "token" || incoming.Prompt != "Enter PIN for id_sk" {
		t.Fatalf("request = %+v", incoming)
	}
}

func TestFidoAskpassFailsWhenTheWindowCancels(t *testing.T) {
	dir, err := os.MkdirTemp("", "motty-ask-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, address, err := fidoaskpass.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("MOTTY_FIDO_ASKPASS_SOCKET", address)
	t.Setenv("MOTTY_FIDO_ASKPASS_TOKEN", "token")
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		var incoming fidoaskpass.Request
		if err := json.NewDecoder(connection).Decode(&incoming); err != nil {
			return
		}
		_ = json.NewEncoder(connection).Encode(fidoaskpass.Response{OK: false})
	}()
	var output bytes.Buffer
	if code := fidoAskpass([]string{"Enter PIN"}, &output); code == 0 {
		t.Fatal("a cancelled prompt succeeded")
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want nothing", output.String())
	}
}
