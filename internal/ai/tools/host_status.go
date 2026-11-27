package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// The host status tools are offered only by the independent MCP service: they
// read resource use from the saved hosts it connects to, and the activity log
// it writes itself. The in-app assistant has the monitor panel and the settings
// page for both.
const (
	NameHostResources = "host_resources"
	NameHostActivity  = "host_activity"

	// DefaultActivityLimit and MaxActivityLimit bound one host_activity page.
	DefaultActivityLimit = 20
	MaxActivityLimit     = 100
)

var hostStatusSpecs = []Spec{
	{
		Name: NameHostResources, Kind: Read,
		Description: "Read CPU, memory and per-mount disk use once from one or more authorized hosts, in parallel. Each host returns collected_at, or its own error when it cannot be reached; the others still return. Uses the same sampling commands as the Motty monitor and changes nothing. CPU is measured over a brief interval; virtual filesystems are left out.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"sessions":{"type":"array","minItems":1,"maxItems":8,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":256},"description":"Authorized host ids from list_sessions, one to eight. May be left out when only one host is available."}` +
			`},"additionalProperties":false}`),
	},
	{
		Name: NameHostActivity, Kind: Read,
		Description: "List the latest calls agents made through this service on the hosts open to you, newest first: time, host, tool, a short summary (secrets masked, never output) and the recorded outcome ok, failed or refused. A queued transfer and its finish are separate entries; only a finished entry says how it ended.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"session":{"type":"string","maxLength":256,"description":"Authorized host id from list_sessions. Without it, every host open to you."},` +
			fmt.Sprintf(`"limit":{"type":"integer","minimum":1,"maximum":%d,"default":%d,"description":"Number of entries to return."}`, MaxActivityLimit, DefaultActivityLimit) +
			`},"additionalProperties":false}`),
	},
}

// HostResourcesArgs names the hosts to sample. Empty means the only host there
// is.
type HostResourcesArgs struct {
	Sessions []string `json:"sessions,omitempty"`
}

// HostActivityArgs filters the activity log to one host, or lists every
// authorized host's.
type HostActivityArgs struct {
	Session string `json:"session,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

func DecodeHostResources(raw []byte) (HostResourcesArgs, error) {
	var args HostResourcesArgs
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if len(args.Sessions) > MaxBatchSessions {
		return args, fmt.Errorf("at most %d hosts per call", MaxBatchSessions)
	}
	for index := range args.Sessions {
		if args.Sessions[index] == "" {
			return args, errors.New("a host id must not be empty")
		}
		if _, err := session(&args.Sessions[index]); err != nil {
			return args, err
		}
	}
	for index, id := range args.Sessions {
		if slices.Contains(args.Sessions[:index], id) {
			return args, fmt.Errorf("host %q is listed twice", id)
		}
	}
	return args, nil
}

func DecodeHostActivity(raw []byte) (HostActivityArgs, error) {
	args := HostActivityArgs{Limit: DefaultActivityLimit}
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if _, err := session(&args.Session); err != nil {
		return args, err
	}
	if args.Limit < 1 || args.Limit > MaxActivityLimit {
		return args, fmt.Errorf("limit must be 1..%d", MaxActivityLimit)
	}
	return args, nil
}
