package workbench

import (
	"errors"
	layoutcore "motty/internal/workspace"
	"slices"
	"strings"
	"testing"
)

func TestNewWorkspaceTemplateValidatesAndStabilizesInput(t *testing.T) {
	hostIDs := []string{" host_a ", "host_b"}
	template, err := NewWorkspaceTemplate(" Production ", hostIDs, layoutcore.GridSpec(2))
	if err != nil {
		t.Fatal(err)
	}
	if !validWorkspaceTemplateID(template.ID) {
		t.Fatalf("template id = %q", template.ID)
	}
	if template.Name != "Production" || !slices.Equal(template.HostIDs, []string{"host_a", "host_b"}) || !isGrid(template.Layout, 2) {
		t.Fatalf("template = %+v", template)
	}
	if err := template.Validate(); err != nil {
		t.Fatal(err)
	}
	hostIDs[0] = "changed"
	if template.HostIDs[0] != "host_a" {
		t.Fatal("template retained the caller's host slice")
	}
}

func TestNewWorkspaceTemplateAcceptsBoundaryCounts(t *testing.T) {
	for count := 1; count <= maxWorkspaceTemplateHosts; count++ {
		for columns := 1; columns <= 3; columns++ {
			hostIDs := make([]string, count)
			for index := range hostIDs {
				hostIDs[index] = "host_" + strings.Repeat("x", index+1)
			}
			template, err := NewWorkspaceTemplate("Boundary", hostIDs, columnsSpec(uint8(columns)))
			if err != nil {
				t.Fatalf("%d hosts in %d columns: %v", count, columns, err)
			}
			if err := template.Validate(); err != nil {
				t.Fatalf("%d hosts in %d columns: %v", count, columns, err)
			}
		}
	}
}

func TestNewWorkspaceTemplateIDsAreStableAndUnique(t *testing.T) {
	first, err := NewWorkspaceTemplate("Production", []string{"host_a"}, layoutcore.GridSpec(1))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewWorkspaceTemplate("Production", []string{"host_a"}, layoutcore.GridSpec(1))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("two templates got the same id %q", first.ID)
	}
	renamed := first
	renamed.Name = "Renamed"
	if renamed.ID != first.ID {
		t.Fatal("renaming changed the template id")
	}
}

func TestNewWorkspaceTemplateRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		hostIDs []string
		columns uint8
		want    error
	}{
		{"empty name", " ", []string{"host_a"}, 1, ErrWorkspaceTemplateName},
		{"long name", strings.Repeat("a", maxWorkspaceTemplateNameRunes+1), []string{"host_a"}, 1, ErrWorkspaceTemplateName},
		{"control name", "line\nbreak", []string{"host_a"}, 1, ErrWorkspaceTemplateName},
		{"no hosts", "Name", nil, 1, ErrWorkspaceTemplateHosts},
		{"too many hosts", "Name", hostIDs(13), 2, ErrWorkspaceTemplateHosts},
		{"empty host", "Name", []string{""}, 1, ErrWorkspaceTemplateHostID},
		{"control host", "Name", []string{"host\n_a"}, 1, ErrWorkspaceTemplateHostID},
		{"duplicate host", "Name", []string{"host_a", "host_a"}, 2, ErrWorkspaceTemplateDupID},
		{"zero columns", "Name", []string{"host_a"}, 0, ErrWorkspaceTemplateLayout},
		{"four columns", "Name", []string{"host_a"}, 4, ErrWorkspaceTemplateLayout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			template, err := NewWorkspaceTemplate(test.title, test.hostIDs, columnsSpec(test.columns))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if template.ID != "" || template.Name != "" || len(template.HostIDs) != 0 || template.Layout.Kind != 0 {
				t.Fatalf("invalid input returned %+v", template)
			}
		})
	}
}

func TestWorkspaceTemplateValidateRejectsMalformedStoredValues(t *testing.T) {
	valid, err := NewWorkspaceTemplate("Production", []string{"host_a", "host_b"}, layoutcore.GridSpec(2))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*WorkspaceTemplate)
	}{
		{"empty id", func(template *WorkspaceTemplate) { template.ID = "" }},
		{"bad id prefix", func(template *WorkspaceTemplate) { template.ID = "ws_bad" }},
		{"non-hex id", func(template *WorkspaceTemplate) { template.ID = "ws_" + strings.Repeat("z", 32) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			template := valid
			test.mutate(&template)
			if err := template.Validate(); !errors.Is(err, ErrWorkspaceTemplateID) {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
	if err := (WorkspaceTemplate{ID: valid.ID, Name: "Name", HostIDs: []string{"host_a", "host_a"}, Layout: layoutcore.GridSpec(1)}).Validate(); !errors.Is(err, ErrWorkspaceTemplateDupID) {
		t.Fatalf("stored duplicate = %v", err)
	}
}

func TestNormalizeWorkspaceTemplateReturnsTheValidatedValue(t *testing.T) {
	template := WorkspaceTemplate{
		ID:      "ws_0123456789abcdef0123456789abcdef",
		Name:    " Production ",
		HostIDs: []string{" host_a ", " host_b "},
		Layout:  layoutcore.GridSpec(2),
	}
	normalized, err := normalizeWorkspaceTemplate(template)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Name != "Production" || !slices.Equal(normalized.HostIDs, []string{"host_a", "host_b"}) {
		t.Fatalf("normalized template = %+v", normalized)
	}
	if template.Name != " Production " || template.HostIDs[0] != " host_a " {
		t.Fatal("normalization changed the input value")
	}
	if err := template.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a normalized copy to remain valid", err)
	}
}

func hostIDs(count int) []string {
	ids := make([]string, count)
	for index := range ids {
		ids[index] = "host_" + strings.Repeat("a", index+1)
	}
	return ids
}
