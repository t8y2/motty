package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTargetedToolsDescribeTheLockedDefaultWithoutChangingMCP(t *testing.T) {
	before := All()
	originals := make(map[string]Spec, len(before))
	for _, spec := range before {
		originals[spec.Name] = spec
	}
	targeted := Targeted()
	if len(before) != len(targeted)+5 {
		t.Fatal("targeted catalog changed the available tool set")
	}
	for _, spec := range targeted {
		original, ok := originals[spec.Name]
		if !ok || spec.Name == NameListHosts || spec.Name == NameToolOutputRead || isJobTool(spec.Name) || spec.Kind != original.Kind || !json.Valid(spec.Schema) {
			t.Fatalf("invalid targeted tool: %+v", spec)
		}
		var schema struct {
			Properties map[string]struct{ Description string }
		}
		if err := json.Unmarshal(spec.Schema, &schema); err != nil {
			t.Fatal(err)
		}
		if session, ok := schema.Properties["session"]; ok &&
			(!strings.Contains(session.Description, "locked primary session") || !strings.Contains(session.Description, "@referenced")) {
			t.Fatalf("tool %s still describes the active tab: %s", spec.Name, session.Description)
		}
		if spec.Name == NameListSessions && !strings.Contains(spec.Description, "not the currently visible tab") {
			t.Fatal("active marker was not explained as the locked default")
		}
		fromAll, _ := Lookup(spec.Name)
		if fromAll.Schema == nil || string(fromAll.Schema) != string(original.Schema) || fromAll.Description != original.Description {
			t.Fatal("targeted catalog modified the external MCP catalog")
		}
	}
}
