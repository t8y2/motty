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
		NameTerminalStart: TerminalStartArgs{}, NameTerminalPoll: TerminalPollArgs{}, NameTerminalStop: TerminalStopArgs{},
		NameSFTPList: FileListArgs{}, NameSFTPRead: FileReadArgs{}, NameSFTPWrite: SFTPWriteArgs{}, NameSFTPRemove: SFTPRemoveArgs{},
		NameSFTPStat: SFTPListArgs{}, NameSFTPChecksum: FileChecksumArgs{}, NameSFTPUpload: TransferArgs{}, NameSFTPDownload: TransferArgs{},
		NameTransferStatus: TransferQuery{}, NameTransferCancel: TransferQuery{},
		NameHostResources: HostResourcesArgs{}, NameHostActivity: HostActivityArgs{},
	}
	for _, spec := range Headless(HeadlessOptions{Commands: true, WriteFiles: true, LocalFiles: true}) {
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
		for _, spec := range Headless(HeadlessOptions{Commands: true, WriteFiles: test.full, LocalFiles: test.local}) {
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

func TestHostStatusToolsAreReadOnlyAndInEveryHeadlessCatalog(t *testing.T) {
	for _, commands := range []bool{false, true} {
		offered := map[string]Spec{}
		for _, spec := range Headless(HeadlessOptions{Commands: commands}) {
			offered[spec.Name] = spec
		}
		for _, name := range []string{NameHostResources, NameHostActivity} {
			spec, ok := offered[name]
			if !ok || spec.Kind != Read {
				t.Fatalf("commands=%v: %s must be offered as a read tool, got %+v", commands, name, spec)
			}
		}
	}
	for _, spec := range All() {
		if spec.Name == NameHostResources || spec.Name == NameHostActivity {
			t.Fatalf("%s leaked into the in-app and HTTP catalog", spec.Name)
		}
	}
}

func TestDecodeHostStatusArguments(t *testing.T) {
	for _, test := range []struct {
		name   string
		raw    string
		decode func([]byte) error
		valid  bool
	}{
		{"one host", `{"sessions":["h1"]}`, func(raw []byte) error { _, err := DecodeHostResources(raw); return err }, true},
		{"default host", `{}`, func(raw []byte) error { _, err := DecodeHostResources(raw); return err }, true},
		{"empty list is the default host", `{"sessions":[]}`, func(raw []byte) error { _, err := DecodeHostResources(raw); return err }, true},
		{"empty id", `{"sessions":[""]}`, func(raw []byte) error { _, err := DecodeHostResources(raw); return err }, false},
		{"duplicate", `{"sessions":["h1","h1"]}`, func(raw []byte) error { _, err := DecodeHostResources(raw); return err }, false},
		{"too many", `{"sessions":["1","2","3","4","5","6","7","8","9"]}`, func(raw []byte) error { _, err := DecodeHostResources(raw); return err }, false},
		{"unknown field", `{"command":"df"}`, func(raw []byte) error { _, err := DecodeHostResources(raw); return err }, false},
		{"activity default", `{}`, func(raw []byte) error { _, err := DecodeHostActivity(raw); return err }, true},
		{"activity limit", `{"session":"h1","limit":100}`, func(raw []byte) error { _, err := DecodeHostActivity(raw); return err }, true},
		{"activity zero limit", `{"limit":0}`, func(raw []byte) error { _, err := DecodeHostActivity(raw); return err }, false},
		{"activity over limit", `{"limit":101}`, func(raw []byte) error { _, err := DecodeHostActivity(raw); return err }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode([]byte(test.raw)); (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
	if args, err := DecodeHostActivity([]byte(`{}`)); err != nil || args.Limit != DefaultActivityLimit {
		t.Fatalf("default activity page = %+v, %v", args, err)
	}
}
