package rfb

import (
	"bytes"
	"errors"
	"testing"
)

func TestRA2EmptyAccountRequiresPasswordAndHonorsCancellation(t *testing.T) {
	client := &Client{conn: &eaxMemoryConn{}}
	for _, config := range []Config{
		{AskCredentials: func() (string, string, error) { return "", "password", nil }},
		{Password: "password"},
	} {
		data, err := client.ra2Credentials(config, ra2UserPass)
		if err != nil {
			t.Fatal(err)
		}
		user, password, err := readPeerCredentials(bytes.NewReader(data))
		if err != nil || user != "" || password != "password" {
			t.Fatalf("credentials=%q/%q error=%v", user, password, err)
		}
	}
	if data, err := client.ra2Credentials(Config{}, ra2UserPass); data != nil || !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("missing credentials=%x error=%v", data, err)
	}
	if data, err := client.ra2Credentials(Config{AskPassword: func() (string, error) {
		t.Error("legacy password callback used for a missing UserPass account")
		return "password", nil
	}}, ra2UserPass); data != nil || !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("missing account credentials=%x error=%v", data, err)
	}
	canceled := errors.New("canceled")
	if data, err := client.ra2Credentials(Config{AskCredentials: func() (string, string, error) {
		return "", "password", canceled
	}}, ra2UserPass); data != nil || !errors.Is(err, canceled) {
		t.Fatalf("canceled credentials=%x error=%v", data, err)
	}
	if data, err := client.ra2Credentials(Config{AskCredentials: func() (string, string, error) {
		return "", "", nil
	}}, ra2UserPass); data != nil || !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("empty credentials=%x error=%v", data, err)
	}
}
