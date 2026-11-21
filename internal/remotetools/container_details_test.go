package remotetools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const inspectFixture = `[{
	"Id": "0f1e2d3c4b5a0f1e2d3c4b5a0f1e2d3c4b5a0f1e2d3c4b5a0f1e2d3c4b5a0f1e",
	"Created": "2026-11-19T08:14:02.123456789Z",
	"Image": "sha256:0f1e2d3c4b5a0000000000000000000000000000000000000000000000000000",
	"Config": {"Entrypoint": ["/docker-entrypoint.sh"], "Cmd": ["nginx", "-g", "daemon off;"]},
	"HostConfig": {"RestartPolicy": {"Name": "on-failure", "MaximumRetryCount": 3}},
	"Mounts": [
		{"Type": "volume", "Name": "nginx-data", "Source": "/var/lib/docker/volumes/nginx-data/_data", "Destination": "/var/cache/nginx", "RW": true},
		{"Type": "bind", "Source": "/srv/www", "Destination": "/usr/share/nginx/html", "RW": false}
	],
	"NetworkSettings": {
		"Ports": {
			"443/tcp": null,
			"80/tcp": [{"HostIp": "::", "HostPort": "8080"}, {"HostIp": "0.0.0.0", "HostPort": "8080"}]
		},
		"Networks": {"bridge": {"IPAddress": "172.17.0.2"}}
	}
}]`

func TestParseContainerDetailsReadsTheOverviewFields(t *testing.T) {
	details, err := ParseContainerDetails([]byte(inspectFixture))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 19, 8, 14, 2, 123456789, time.UTC); !details.Created.Equal(want) {
		t.Errorf("Created = %v, want %v", details.Created, want)
	}
	if details.Command != `/docker-entrypoint.sh nginx -g 'daemon off;'` {
		t.Errorf("Command = %q", details.Command)
	}
	if !strings.HasPrefix(details.ImageID, "sha256:0f1e2d3c4b5a") {
		t.Errorf("ImageID = %q", details.ImageID)
	}
	wantPorts := []string{"0.0.0.0:8080->80/tcp", ":::8080->80/tcp", "443/tcp"}
	if len(details.Ports) != len(wantPorts) {
		t.Fatalf("Ports = %q, want %q", details.Ports, wantPorts)
	}
	for index, want := range wantPorts {
		if details.Ports[index] != want {
			t.Fatalf("Ports = %q, want %q", details.Ports, wantPorts)
		}
	}
	if len(details.Mounts) != 2 {
		t.Fatalf("Mounts = %+v", details.Mounts)
	}
	bind := details.Mounts[0]
	if bind.Type != "bind" || bind.Source != "/srv/www" || bind.Destination != "/usr/share/nginx/html" || !bind.ReadOnly {
		t.Errorf("bind mount = %+v", bind)
	}
	volume := details.Mounts[1]
	if volume.Type != "volume" || volume.Name != "nginx-data" || volume.ReadOnly {
		t.Errorf("volume mount = %+v", volume)
	}
	if len(details.Networks) != 1 || details.Networks[0].Name != "bridge" || details.Networks[0].Address != "172.17.0.2" {
		t.Errorf("Networks = %+v", details.Networks)
	}
	if details.RestartPolicy != "on-failure:3" {
		t.Errorf("RestartPolicy = %q", details.RestartPolicy)
	}
}

func TestParseContainerDetailsKeepsAnEmptyCommandAndDefaultPolicy(t *testing.T) {
	document := `[{"Created": "2026-11-19T08:14:02Z", "Image": "sha256:abc", "HostConfig": {"RestartPolicy": {}}}]`
	details, err := ParseContainerDetails([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if details.Command != "" || details.RestartPolicy != "no" || details.Ports != nil || details.Mounts != nil {
		t.Fatalf("details = %+v", details)
	}
}

func TestParseContainerDetailsRejectsUnreadableDocuments(t *testing.T) {
	for _, document := range []string{
		"",
		"not json",
		"[]",
		`[{"Created": "yesterday", "Image": "sha256:abc"}]`,
	} {
		if _, err := ParseContainerDetails([]byte(document)); !errors.Is(err, ErrMalformed) {
			t.Errorf("document %q: err = %v, want ErrMalformed", document, err)
		}
	}
	oversized := `[{"Created": "` + strings.Repeat("x", maxContainerDetailsOutput) + `"}]`
	if _, err := ParseContainerDetails([]byte(oversized)); !errors.Is(err, ErrContainerDetailsTooLarge) {
		t.Errorf("oversized document: err = %v, want ErrContainerDetailsTooLarge", err)
	}
}

func TestReadContainerDetailsBoundsTheInspectPayload(t *testing.T) {
	documents := &scriptedRunner{results: []scriptedResult{{
		output: strings.Repeat("x", maxContainerDetailsOutput),
		err:    errors.New("output too large"),
	}}}
	if _, err := ReadContainerDetails(context.Background(), documents, "0f1e2d3c4b5a"); !errors.Is(err, ErrContainerDetailsTooLarge) {
		t.Fatalf("err = %v, want ErrContainerDetailsTooLarge", err)
	}
	if !strings.HasPrefix(documents.commands[0], "docker inspect 0f1e2d3c4b5a") {
		t.Fatalf("asked %q", documents.commands[0])
	}
}

func TestReadContainerDetailsReportsAMissingDockerCLI(t *testing.T) {
	runner := &fixedRunner{output: "sh: 1: docker: not found", err: errors.New("exit status 127")}
	if _, err := ReadContainerDetails(context.Background(), runner, "0f1e2d3c4b5a"); !errors.Is(err, ErrNoDocker) {
		t.Fatalf("err = %v, want ErrNoDocker", err)
	}
}
