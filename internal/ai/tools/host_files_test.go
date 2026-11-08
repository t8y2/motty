package tools

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHostFileArgumentBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		raw    string
		decode func([]byte) error
		valid  bool
	}{
		{"page", `{"path":"/tmp","offset":500,"limit":1000}`, func(raw []byte) error { _, err := DecodeFileList(raw); return err }, true},
		{"negative page", `{"path":"/tmp","offset":-1}`, func(raw []byte) error { _, err := DecodeFileList(raw); return err }, false},
		{"empty page", `{"path":"/tmp","limit":0}`, func(raw []byte) error { _, err := DecodeFileList(raw); return err }, false},
		{"tail", `{"path":"/log","tail":true,"max_bytes":262144}`, func(raw []byte) error { _, err := DecodeFileRead(raw); return err }, true},
		{"ambiguous range", `{"path":"/log","tail":true,"offset":1}`, func(raw []byte) error { _, err := DecodeFileRead(raw); return err }, false},
		{"large range", `{"path":"/log","max_bytes":262145}`, func(raw []byte) error { _, err := DecodeFileRead(raw); return err }, false},
		{"binary", `{"path":"/bin","encoding":"base64","offset":1}`, func(raw []byte) error { _, err := DecodeFileRead(raw); return err }, true},
		{"bad encoding", `{"path":"/bin","encoding":"raw"}`, func(raw []byte) error { _, err := DecodeFileRead(raw); return err }, false},
		{"hash", `{"path":"/bin","algorithm":"sha512"}`, func(raw []byte) error { _, err := DecodeFileChecksum(raw); return err }, true},
		{"bad hash", `{"path":"/bin","algorithm":"md5"}`, func(raw []byte) error { _, err := DecodeFileChecksum(raw); return err }, false},
		{"cwd", `{"command":"pwd","cwd":"/tmp/a'b"}`, func(raw []byte) error { _, err := DecodeHostExec(raw); return err }, true},
		{"relative cwd", `{"command":"pwd","cwd":"tmp"}`, func(raw []byte) error { _, err := DecodeHostExec(raw); return err }, false},
		{"unknown argument", `{"command":"pwd","sudo":true}`, func(raw []byte) error { _, err := DecodeHostExec(raw); return err }, false},
		{"missing task", `{}`, func(raw []byte) error { _, err := DecodeTransferQuery(raw, true); return err }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode([]byte(test.raw)); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
	args := TransferArgs{LocalPath: filepath.Join(t.TempDir(), "file.bin"), RemotePath: "/tmp/file.bin"}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTransfer(raw)
	if err != nil || decoded.Existing != "fail" {
		t.Fatalf("default transfer: %+v %v", decoded, err)
	}
}

func TestHeadlessSchemasMatchTheirExtendedArguments(t *testing.T) {
	arguments := map[string]any{
		NameListSessions: ListSessionsArgs{}, NameListHosts: ListHostsArgs{}, NameTerminalExec: HostExecArgs{}, NameTerminalExecMany: TerminalExecManyArgs{},
		NameSFTPList: FileListArgs{}, NameSFTPRead: FileReadArgs{}, NameSFTPWrite: SFTPWriteArgs{}, NameSFTPRemove: SFTPRemoveArgs{},
		NameSFTPStat: SFTPListArgs{}, NameSFTPChecksum: FileChecksumArgs{}, NameSFTPUpload: TransferArgs{}, NameSFTPDownload: TransferArgs{},
		NameTransferStatus: TransferQuery{}, NameTransferCancel: TransferQuery{},
	}
	for _, spec := range Headless(true, true, true) {
		var schema struct {
			Properties           map[string]json.RawMessage
			AdditionalProperties bool
		}
		if err := json.Unmarshal(spec.Schema, &schema); err != nil {
			t.Fatal(err)
		}
		properties := make([]string, 0, len(schema.Properties))
		for property := range schema.Properties {
			properties = append(properties, property)
		}
		slices.Sort(properties)
		if schema.AdditionalProperties || !slices.Equal(properties, jsonNames(t, arguments[spec.Name])) {
			t.Fatalf("schema/arguments mismatch: %s %v", spec.Name, properties)
		}
	}
}

func TestTransfersRequireBothFullAccessAndLocalFileAuthorization(t *testing.T) {
	for _, test := range []struct{ full, local bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		names := make(map[string]bool)
		for _, spec := range Headless(true, test.full, test.local) {
			names[spec.Name] = true
			if !json.Valid(spec.Schema) {
				t.Fatalf("invalid schema %s", spec.Name)
			}
			if _, found := Lookup(spec.Name); !found {
				t.Fatalf("missing lookup %s", spec.Name)
			}
			if spec.Name == NameSFTPRead && strings.Contains(spec.Description, NameSFTPDownload) != (test.full && test.local) {
				t.Fatalf("sftp_read description must mention sftp_download only when it is offered (%+v): %s", test, spec.Description)
			}
		}
		if names[NameSFTPUpload] != (test.full && test.local) || names[NameSFTPDownload] != (test.full && test.local) {
			t.Fatalf("permissions %+v: %v", test, names)
		}
	}
	for _, spec := range All() {
		if spec.Name == NameSFTPUpload || spec.Name == NameSFTPDownload {
			t.Fatal("local file access leaked into the HTTP/in-app catalog")
		}
	}
}
