package remotetools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Kind is a sort of Docker object besides containers.
type Kind uint8

// The kinds listed.
const (
	Images Kind = iota
	Volumes
	Networks
)

// Resource is one image, volume or network as the cells its table shows. Key is
// what Remove addresses it by: an image's reference (or its ID when untagged), a
// volume's name, a network's ID.
type Resource struct {
	Key   string
	Cells []string
}

// listing is the command and the number of `|` separated fields it prints.
func (kind Kind) listing() (command string, fields int) {
	switch kind {
	case Volumes:
		return "docker volume ls --format '{{.Name}}|{{.Driver}}|{{.Scope}}' 2>&1", 3
	case Networks:
		return "docker network ls --format '{{.ID}}|{{.Name}}|{{.Driver}}|{{.Scope}}' 2>&1", 4
	}
	return "docker images --format '{{.ID}}|{{.Repository}}:{{.Tag}}|{{.Size}}|{{.CreatedSince}}' 2>&1", 4
}

func (kind Kind) legacyListing() (command, unsupportedField string) {
	switch kind {
	case Volumes:
		return "docker volume ls --format '{{.Name}}|{{.Driver}}' 2>&1", "Scope"
	case Networks:
		return "docker network ls --format '{{.ID}}|{{.Name}}|{{.Driver}}' 2>&1", "Scope"
	default:
		return "docker images --format '{{.ID}}|{{.Repository}}:{{.Tag}}|{{.Size}}' 2>&1", "CreatedSince"
	}
}

func missingTemplateField(output []byte, field string) bool {
	text := string(output)
	return strings.Contains(text, "<."+field+">") && strings.Contains(text, "can't evaluate field "+field)
}

func appendMissingResourceField(output []byte) []byte {
	var rows strings.Builder
	for _, row := range strings.Split(string(output), "\n") {
		row = strings.TrimSuffix(row, "\r")
		if row != "" {
			rows.WriteString(row)
			rows.WriteString("|\n")
		}
	}
	return []byte(rows.String())
}

// Docker object names are a restricted alphabet; an image reference may also
// carry a registry path, a tag and a digest.
var referencePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:/@-]{0,255}$`)

// Resources lists every object of the kind.
func Resources(ctx context.Context, runner Runner, kind Kind) ([]Resource, error) {
	command, fields := kind.listing()
	output, err := runner.RunCommand(ctx, command, maxOutput)
	legacyCommand, optionalField := kind.legacyListing()
	legacy := err != nil && missingTemplateField(output, optionalField)
	if legacy {
		output, err = runner.RunCommand(ctx, legacyCommand, maxOutput)
	}
	if err != nil {
		text := strings.TrimSpace(string(output))
		if missingDockerCLI(output) {
			return nil, ErrNoDocker
		}
		if text == "" {
			return nil, err
		}
		line, _, _ := strings.Cut(text, "\n")
		return nil, &Failure{Message: line, Err: err}
	}
	if legacy {
		output = appendMissingResourceField(output)
	}
	return ParseResources(kind, output, fields)
}

// ParseResources reads a listing command's output.
func ParseResources(kind Kind, output []byte, fields int) ([]Resource, error) {
	var list []Resource
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", fields)
		if len(parts) != fields {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		resource := Resource{Cells: parts}
		switch kind {
		case Images:
			// Cells: name (repository:tag), ID, size, age. An image with several
			// tags is one row per tag; an untagged one is removed by its ID.
			resource.Key = parts[1]
			if strings.HasSuffix(parts[1], ":<none>") || parts[1] == "<none>:<none>" {
				resource.Key = parts[0]
			}
			resource.Cells = []string{parts[1], parts[0], parts[2], parts[3]}
		case Volumes:
			resource.Key = parts[0]
		case Networks:
			resource.Key = parts[0]
			resource.Cells = []string{parts[1], parts[0], parts[2], parts[3]}
		}
		if !referencePattern.MatchString(resource.Key) {
			return nil, &Failure{Message: line, Err: ErrMalformed}
		}
		list = append(list, resource)
	}
	if kind == Volumes {
		// Docker names a volume nobody named with 64 hex digits. A server can have
		// hundreds of them, so the named ones come first and survive the limit.
		sort.SliceStable(list, func(a, b int) bool { return !anonymousVolume(list[a].Key) && anonymousVolume(list[b].Key) })
	}
	if len(list) > maxItems {
		list = list[:maxItems]
	}
	return list, nil
}

var anonymousVolumePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func anonymousVolume(name string) bool { return anonymousVolumePattern.MatchString(name) }

// RemoveResource removes one image, volume or network by its Key. Docker itself
// refuses what is in use, and its one-line reason is returned.
func RemoveResource(ctx context.Context, runner Runner, kind Kind, key string) error {
	if !referencePattern.MatchString(key) {
		return fmt.Errorf("remotetools: %q is not a docker object name", key)
	}
	verb := "rmi"
	switch kind {
	case Volumes:
		verb = "volume rm"
	case Networks:
		verb = "network rm"
	}
	return run(ctx, runner, "docker "+verb+" "+key+" 2>&1")
}

// ValidName reports whether name is acceptable for a volume or network created
// here: the characters Docker allows in an object name.
func ValidName(name string) bool { return validName(name) }

// PullCommand is the command line that pulls an image, for the user to run in
// their own terminal: a pull takes as long as the network does, prints its
// progress, and the user may want to stop it.
func PullCommand(reference string) (string, bool) {
	if !referencePattern.MatchString(reference) {
		return "", false
	}
	return "docker pull " + reference, true
}

// CreateResource creates an empty volume or a network (the default bridge
// driver) with the given name. Docker refuses a name in use; its one-line
// reason is returned.
func CreateResource(ctx context.Context, runner Runner, kind Kind, name string) error {
	if !validName(name) {
		return fmt.Errorf("remotetools: %q is not a docker object name", name)
	}
	switch kind {
	case Volumes:
		return run(ctx, runner, "docker volume create "+name+" 2>&1")
	case Networks:
		return run(ctx, runner, "docker network create "+name+" 2>&1")
	}
	return fmt.Errorf("remotetools: images are pulled, not created")
}
