package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTargetedToolsDescribeTheLockedDefaultWithoutChangingMCP(t *testing.T) {
	before := All()
	schemas := make([]string, len(before))
	for index, spec := range before {
		schemas[index] = string(spec.Schema)
	}
	targeted := Targeted()
	if len(before) != len(targeted) {
		t.Fatal("targeted catalog changed the available tool set")
	}
	for index, spec := range targeted {
		if spec.Name != before[index].Name || spec.Kind != before[index].Kind || !json.Valid(spec.Schema) {
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
		if original, _ := Lookup(spec.Name); string(original.Schema) != schemas[index] || original.Description != before[index].Description {
			t.Fatal("targeted catalog modified the external MCP catalog")
		}
	}
}
