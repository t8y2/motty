package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"motty/internal/configstore"
	"motty/internal/vault"
)

func sample() Document {
	return Document{
		Created: time.Unix(1_700_000_000, 0).UTC(),
		Device:  "0a1b2c3d",
		Records: []configstore.Record{
			{Kind: "host", ID: "h1", Stamp: "00018bcfe56800.0000.0a1b2c3d", Payload: json.RawMessage(`{"d":{"label":"Production","address":"prod.example.com"}}`)},
			{Kind: "host", ID: "h2", Stamp: "00018bcfe56801.0000.0a1b2c3d", Deleted: true},
		},
		KnownHosts: "prod.example.com ssh-ed25519 AAAA\n",
		Secrets: []vault.Secret{
			{Name: "password:deploy@prod.example.com:22", Value: "hunter2", Stamp: "00018bcfe56800.0001.0a1b2c3d"},
			{Name: "otp:deploy@prod.example.com:22", Value: "otpauth://totp/x?secret=ABC", Stamp: "00018bcfe56800.0002.0a1b2c3d"},
			{Name: "password:gone", Stamp: "00018bcfe56802.0000.0a1b2c3d", Deleted: true},
		},
	}
}

func TestASealedBackupOpensWithItsPasswordAndHidesEverythingElse(t *testing.T) {
	sealed, err := Seal(sample(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2", "prod.example.com", "Production", "otpauth", "ssh-ed25519"} {
		if bytes.Contains(sealed, []byte(leak)) {
			t.Fatalf("the sealed file shows %q", leak)
		}
	}
	opened, err := Open(sealed, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	want := sample()
	if !opened.Created.Equal(want.Created) || opened.Device != want.Device || opened.KnownHosts != want.KnownHosts ||
		!reflect.DeepEqual(opened.Records, want.Records) || !reflect.DeepEqual(opened.Secrets, want.Secrets) {
		t.Fatalf("opened %+v", opened)
	}
}

func TestTwoSealsOfTheSameBackupDiffer(t *testing.T) {
	a, _ := Seal(sample(), "correct horse")
	b, _ := Seal(sample(), "correct horse")
	if bytes.Equal(a, b) {
		t.Fatal("the salt and nonce are not fresh")
	}
}

func TestAWrongPasswordAndAChangedFileAreBothRefused(t *testing.T) {
	sealed, _ := Seal(sample(), "correct horse")
	if _, err := Open(sealed, "battery staple"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	var file envelope
	json.Unmarshal(sealed, &file)
	file.Data[len(file.Data)/2] ^= 1
	tampered, _ := json.Marshal(file)
	if _, err := Open(tampered, "correct horse"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("changed data: %v", err)
	}
	// The readable costs are bound too: weakening them does not open anything.
	json.Unmarshal(sealed, &file)
	file.KDF.Time = 1
	cheaper, _ := json.Marshal(file)
	if _, err := Open(cheaper, "correct horse"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("changed costs: %v", err)
	}
}

func TestOnlyBackupsOfThisProgramAreRecognised(t *testing.T) {
	for name, data := range map[string]string{
		"empty": "", "text": "hello", "other json": `{"format":"something-else"}`,
		"absurd cost": `{"format":"motty-backup","version":2,"kdf":{"name":"argon2id","time":999999,"memory_kib":65536,"threads":4,"salt":"AAAAAAAAAAAAAAAAAAAAAA=="},"nonce":"AAAA","data":"AAAA"}`,
	} {
		if _, err := Open([]byte(data), "correct horse"); !errors.Is(err, ErrNotBackup) {
			t.Errorf("%s: %v", name, err)
		}
	}
	newer, _ := json.Marshal(envelope{Format: format, Version: version + 1})
	if _, err := Open(newer, "correct horse"); !errors.Is(err, ErrTooNew) {
		t.Errorf("newer: %v", err)
	}
}

func TestAShortPasswordIsRefusedAtSealing(t *testing.T) {
	if _, err := Seal(sample(), strings.Repeat("a", MinPasswordLength-1)); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("err = %v", err)
	}
}

func TestPrivateKeysRideInTheBackupWhenItCarriesThem(t *testing.T) {
	document := sample()
	document.Keys = []Key{{
		Fingerprint: "SHA256:abc123", Name: "id_ed25519", Path: "/home/me/.ssh/id_ed25519",
		Data: []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n"),
	}}
	sealed, err := Seal(document, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("OPENSSH PRIVATE KEY")) {
		t.Fatal("the sealed file shows the key")
	}
	opened, err := Open(sealed, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opened.Keys, document.Keys) {
		t.Fatalf("keys after opening: %+v", opened.Keys)
	}
}

func TestAKeyThatNamesAPathOrRepeatsAnotherIsNotABackup(t *testing.T) {
	good := Key{Fingerprint: "SHA256:abc123", Name: "id_ed25519", Data: []byte("key")}
	for name, key := range map[string]Key{
		"a path":   {Fingerprint: "SHA256:abc123", Name: "../id_ed25519", Data: []byte("key")},
		"no name":  {Fingerprint: "SHA256:abc123", Data: []byte("key")},
		"no print": {Name: "id_ed25519", Data: []byte("key")},
		"no data":  {Fingerprint: "SHA256:abc123", Name: "id_ed25519"},
		"too big":  {Fingerprint: "SHA256:abc123", Name: "id_ed25519", Data: make([]byte, MaxKeySize+1)},
		"twice":    good,
	} {
		document := sample()
		if name == "twice" {
			document.Keys = []Key{good, good}
		} else {
			document.Keys = []Key{key}
		}
		sealed, err := Seal(document, "correct horse")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Open(sealed, "correct horse"); !errors.Is(err, ErrNotBackup) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
