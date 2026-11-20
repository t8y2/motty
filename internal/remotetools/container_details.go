package remotetools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrContainerDetailsTooLarge means docker inspect wrote more than the details
// view keeps, so its output cannot be parsed.
var ErrContainerDetailsTooLarge = errors.New("remotetools: the container's inspect data is too large")

const maxContainerDetailsOutput = 1 << 20

// ContainerMount is one mount of a container.
type ContainerMount struct {
	Type        string // bind, volume, tmpfs, ...
	Name        string // the volume's name; empty for other types
	Source      string
	Destination string
	ReadOnly    bool
}

// ContainerNetwork is one network a container is attached to.
type ContainerNetwork struct {
	Name    string
	Address string
}

// ContainerDetails is the part of docker inspect the details view shows. The
// data stays typed: the environment has a tab of its own, and a raw document
// would only repeat what these fields already name.
type ContainerDetails struct {
	ImageID       string
	Created       time.Time
	Command       string   // entrypoint and command, with each argument kept whole
	Ports         []string // docker ps' "0.0.0.0:8080->80/tcp" form, by container port
	Mounts        []ContainerMount
	Networks      []ContainerNetwork
	RestartPolicy string // no, always, unless-stopped or on-failure[:max]
}

// ReadContainerDetails reads the configured details of one container. Docker
// keeps them for stopped containers too, so no command runs inside it.
func ReadContainerDetails(ctx context.Context, runner Runner, id string) (ContainerDetails, error) {
	if !containerIDPattern.MatchString(id) {
		return ContainerDetails{}, fmt.Errorf("remotetools: %q is not a container id", id)
	}
	output, err := runner.RunCommand(ctx, "docker inspect "+id+" 2>&1", maxContainerDetailsOutput)
	if err != nil {
		if missingDockerCLI(output) {
			return ContainerDetails{}, ErrNoDocker
		}
		// Never copy a retained over-limit prefix into an error the UI paints.
		if len(output) >= maxContainerDetailsOutput {
			return ContainerDetails{}, ErrContainerDetailsTooLarge
		}
		message := strings.TrimSpace(string(output))
		if line, _, found := strings.Cut(message, "\n"); found {
			message = line
		}
		if message != "" {
			return ContainerDetails{}, &Failure{Message: message, Err: err}
		}
		return ContainerDetails{}, err
	}
	return ParseContainerDetails(output)
}

// docker inspect prints an array of one document; these types read only the
// fields the details view shows.
type inspectDocument struct {
	Created         string                 `json:"Created"`
	Image           string                 `json:"Image"`
	Config          inspectConfig          `json:"Config"`
	HostConfig      inspectHostConfig      `json:"HostConfig"`
	Mounts          []inspectMount         `json:"Mounts"`
	NetworkSettings inspectNetworkSettings `json:"NetworkSettings"`
}

type inspectConfig struct {
	Cmd        []string `json:"Cmd"`
	Entrypoint []string `json:"Entrypoint"`
}

type inspectHostConfig struct {
	RestartPolicy struct {
		Name              string `json:"Name"`
		MaximumRetryCount int    `json:"MaximumRetryCount"`
	} `json:"RestartPolicy"`
}

type inspectMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

type inspectPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type inspectNetworkSettings struct {
	Ports    map[string][]inspectPortBinding `json:"Ports"`
	Networks map[string]inspectNetwork       `json:"Networks"`
}

type inspectNetwork struct {
	IPAddress string `json:"IPAddress"`
}

// ParseContainerDetails reads docker inspect's array for the first container.
func ParseContainerDetails(output []byte) (ContainerDetails, error) {
	if len(output) > maxContainerDetailsOutput {
		return ContainerDetails{}, ErrContainerDetailsTooLarge
	}
	var documents []inspectDocument
	if err := json.Unmarshal(output, &documents); err != nil || len(documents) == 0 {
		return ContainerDetails{}, ErrMalformed
	}
	document := documents[0]
	created, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(document.Created))
	if err != nil {
		return ContainerDetails{}, ErrMalformed
	}
	details := ContainerDetails{
		ImageID:  document.Image,
		Created:  created,
		Command:  joinContainerCommand(document.Config.Entrypoint, document.Config.Cmd),
		Ports:    containerPorts(document.NetworkSettings.Ports),
		Mounts:   containerMounts(document.Mounts),
		Networks: containerNetworks(document.NetworkSettings.Networks),
		RestartPolicy: containerRestartPolicy(
			document.HostConfig.RestartPolicy.Name,
			document.HostConfig.RestartPolicy.MaximumRetryCount,
		),
	}
	return details, nil
}

// joinContainerCommand prints the argv Docker would run, keeping an argument
// with spaces or shell syntax in one piece.
func joinContainerCommand(entrypoint, command []string) string {
	arguments := make([]string, 0, len(entrypoint)+len(command))
	arguments = append(arguments, entrypoint...)
	arguments = append(arguments, command...)
	var builder strings.Builder
	for index, argument := range arguments {
		if index > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(quoteContainerArgument(argument))
	}
	return builder.String()
}

func quoteContainerArgument(argument string) string {
	if argument == "" {
		return "''"
	}
	plain := true
	for _, r := range argument {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("_@%+=:,./-", r) {
			continue
		}
		plain = false
		break
	}
	if plain {
		return argument
	}
	return "'" + strings.ReplaceAll(argument, "'", `'\''`) + "'"
}

// containerPorts lists the published bindings in docker ps' form, ordered by
// container port so a refresh paints the same rows.
func containerPorts(ports map[string][]inspectPortBinding) []string {
	if len(ports) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ports))
	for key := range ports {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(a, b int) bool {
		if order := compareContainerPorts(keys[a], keys[b]); order != 0 {
			return order < 0
		}
		return keys[a] < keys[b]
	})
	list := make([]string, 0, len(keys))
	for _, key := range keys {
		bindings := ports[key]
		if len(bindings) == 0 {
			// Exposed but never published, as EXPOSE leaves it.
			list = append(list, key)
			continue
		}
		sorted := append([]inspectPortBinding(nil), bindings...)
		sort.Slice(sorted, func(a, b int) bool {
			if sorted[a].HostIP != sorted[b].HostIP {
				return sorted[a].HostIP < sorted[b].HostIP
			}
			return sorted[a].HostPort < sorted[b].HostPort
		})
		for _, binding := range sorted {
			list = append(list, binding.HostIP+":"+binding.HostPort+"->"+key)
		}
	}
	return list
}

func compareContainerPorts(a, b string) int {
	aPort, aProto, _ := strings.Cut(a, "/")
	bPort, bProto, _ := strings.Cut(b, "/")
	aNumber, aErr := strconv.Atoi(aPort)
	bNumber, bErr := strconv.Atoi(bPort)
	if aErr == nil && bErr == nil && aNumber != bNumber {
		if aNumber < bNumber {
			return -1
		}
		return 1
	}
	return strings.Compare(aProto, bProto)
}

func containerMounts(mounts []inspectMount) []ContainerMount {
	if len(mounts) == 0 {
		return nil
	}
	list := make([]ContainerMount, 0, len(mounts))
	for _, mount := range mounts {
		list = append(list, ContainerMount{
			Type: mount.Type, Name: mount.Name, Source: mount.Source,
			Destination: mount.Destination, ReadOnly: !mount.RW,
		})
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Destination < list[b].Destination })
	return list
}

func containerNetworks(networks map[string]inspectNetwork) []ContainerNetwork {
	if len(networks) == 0 {
		return nil
	}
	list := make([]ContainerNetwork, 0, len(networks))
	for name, network := range networks {
		list = append(list, ContainerNetwork{Name: name, Address: network.IPAddress})
	}
	sort.Slice(list, func(a, b int) bool { return list[a].Name < list[b].Name })
	return list
}

func containerRestartPolicy(name string, maximumRetryCount int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "no"
	}
	if name == "on-failure" && maximumRetryCount > 0 {
		return name + ":" + strconv.Itoa(maximumRetryCount)
	}
	return name
}
