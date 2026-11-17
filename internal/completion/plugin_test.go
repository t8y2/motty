package completion

import (
	"reflect"
	"testing"
)

func pluginTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	compose := &Node{
		Token: "docker-compose",
		Desc:  "Docker Compose",
		Flags: []Flag{{Long: "file", Short: "f", Value: true, Desc: "compose file"}},
		Children: []*Node{
			{Token: "down", Desc: "stop and remove", Flags: []Flag{{Long: "volumes", Short: "v"}}},
			{Token: "up", Desc: "create and start", Flags: []Flag{{Long: "detach", Short: "d"}}},
		},
	}
	buildx := &Node{Token: "docker-buildx", Desc: "Docker Buildx"}
	docker := &Node{
		Token: "docker",
		Desc:  "containers",
		Children: []*Node{
			{Token: "build", Desc: "build an image"},
			{Token: "container", Desc: "manage containers"},
		},
	}
	ssh := &Node{Token: "ssh", Desc: "remote login"}
	keygen := &Node{Token: "ssh-keygen", Desc: "key generator"}
	catalog, err := NewCatalog("test", "v0", "deadbeef", "MIT", []*Node{docker, compose, buildx, ssh, keygen})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func TestPluginsIndex(t *testing.T) {
	catalog := pluginTestCatalog(t)

	var names []string
	for _, plugin := range catalog.Plugins("docker") {
		names = append(names, plugin.Name)
	}
	if want := []string{"buildx", "compose"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("docker plugins = %v, want %v", names, want)
	}
	if catalog.Plugins("ssh") != nil {
		t.Fatalf("ssh has plugins: %v", catalog.Plugins("ssh"))
	}
	if plugin := pluginCommand(catalog, "docker", "compose"); plugin == nil || plugin.Token != "docker-compose" {
		t.Fatalf("docker compose resolves to %v", plugin)
	}
}

func TestCompletePluginSubcommand(t *testing.T) {
	catalog := pluginTestCatalog(t)

	result := Complete(catalog, "docker comp")
	if !hasCandidate(result, "compose") {
		t.Fatalf("docker comp = %v", candidateTexts(result))
	}
	for _, candidate := range result.Candidates {
		if candidate.Text == "compose" && candidate.Insert != "ose" {
			t.Fatalf("compose insert = %q, want ose", candidate.Insert)
		}
	}

	result = Complete(catalog, "docker compose ")
	for _, want := range []string{"down", "up", "--file"} {
		if !hasCandidate(result, want) {
			t.Fatalf("docker compose lacks %q in %v", want, candidateTexts(result))
		}
	}

	result = Complete(catalog, "docker compose up --d")
	if !hasCandidate(result, "--detach") {
		t.Fatalf("docker compose up --d = %v", candidateTexts(result))
	}
}

func TestCompletePluginListIsAlphabetical(t *testing.T) {
	catalog := pluginTestCatalog(t)

	result := Complete(catalog, "docker ")
	position := map[string]int{}
	for index, text := range candidateTexts(result) {
		position[text] = index
	}
	for _, name := range []string{"build", "buildx", "compose", "container"} {
		if _, ok := position[name]; !ok {
			t.Fatalf("docker  lacks %q in %v", name, candidateTexts(result))
		}
	}
	if !(position["build"] < position["buildx"] && position["buildx"] < position["compose"] &&
		position["compose"] < position["container"]) {
		t.Fatalf("docker  is not in name order: %v", candidateTexts(result))
	}
}

func TestCompletePluginNeedsPluginHost(t *testing.T) {
	catalog := pluginTestCatalog(t)

	result := Complete(catalog, "ssh key")
	if hasCandidate(result, "keygen") {
		t.Fatalf("ssh key offered an independent program: %v", candidateTexts(result))
	}
	if result := Complete(catalog, "ssh-key"); !hasCandidate(result, "ssh-keygen") {
		t.Fatalf("ssh-key = %v", candidateTexts(result))
	}
}

func TestCompleteSubcommandBeatsPlugin(t *testing.T) {
	compose := &Node{
		Token: "docker-compose",
		Desc:  "Docker Compose",
		Children: []*Node{
			{Token: "up", Desc: "create and start"},
		},
	}
	docker := &Node{
		Token: "docker",
		Desc:  "containers",
		Children: []*Node{
			{Token: "compose", Desc: "the built-in subcommand", Children: []*Node{{Token: "local", Desc: "local compose"}}},
		},
	}
	catalog, err := NewCatalog("test", "v0", "deadbeef", "MIT", []*Node{docker, compose})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	result := Complete(catalog, "docker compose ")
	if !hasCandidate(result, "local") {
		t.Fatalf("docker compose = %v", candidateTexts(result))
	}
	if hasCandidate(result, "up") {
		t.Fatalf("the plugin shadowed the real subcommand: %v", candidateTexts(result))
	}

	result = Complete(catalog, "docker ")
	count := 0
	for _, text := range candidateTexts(result) {
		if text == "compose" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("compose listed %d times: %v", count, candidateTexts(result))
	}
}
