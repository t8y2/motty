package workbench

import (
	"reflect"
	"testing"
)

func TestSnippetVariablesParsing(t *testing.T) {
	t.Run("plain command", func(t *testing.T) {
		cmd := "ls -la"
		if snippetHasVariables(cmd) {
			t.Errorf("expected no variables in %q", cmd)
		}
		if vars := parseSnippetVariables(cmd); len(vars) != 0 {
			t.Errorf("expected empty vars, got %v", vars)
		}
	})

	t.Run("simple vars and newlines", func(t *testing.T) {
		cmd := "echo '{{test}}'\necho '{{test2}}'"
		if !snippetHasVariables(cmd) {
			t.Errorf("expected variables in %q", cmd)
		}
		vars := parseSnippetVariables(cmd)
		expected := []snippetVariableDef{
			{Name: "test"},
			{Name: "test2"},
		}
		if !reflect.DeepEqual(vars, expected) {
			t.Errorf("expected %v, got %v", expected, vars)
		}
	})

	t.Run("dedupes by first occurrence", func(t *testing.T) {
		cmd := "echo {{a}} and {{b}} and {{a}}"
		vars := parseSnippetVariables(cmd)
		expected := []snippetVariableDef{
			{Name: "a"},
			{Name: "b"},
		}
		if !reflect.DeepEqual(vars, expected) {
			t.Errorf("expected %v, got %v", expected, vars)
		}
	})

	t.Run("reads default after colon and unicode names", func(t *testing.T) {
		cmd := "fallocate -l {{内存大小:4}}G"
		vars := parseSnippetVariables(cmd)
		expected := []snippetVariableDef{
			{Name: "内存大小", DefaultValue: "4", HasDefault: true},
		}
		if !reflect.DeepEqual(vars, expected) {
			t.Errorf("expected %v, got %v", expected, vars)
		}
	})

	t.Run("second occurrence adds default if first had none", func(t *testing.T) {
		cmd := "echo {{port}} on {{port:8080}}"
		vars := parseSnippetVariables(cmd)
		expected := []snippetVariableDef{
			{Name: "port", DefaultValue: "8080", HasDefault: true},
		}
		if !reflect.DeepEqual(vars, expected) {
			t.Errorf("expected %v, got %v", expected, vars)
		}
	})
}

func TestSnippetVariablesDockerGoTemplates(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"ls -lh $(docker inspect --format='{{.LogPath}}' moviepilot)", false},
		{"ls -lh $(docker inspect --format='{{- .LogPath -}}' moviepilot)", false},
		{"docker inspect --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' moviepilot", false},
		{"docker inspect --format='{{range $k, $v := .NetworkSettings.Networks}}{{$v.IPAddress}}{{end}}' moviepilot", false},
		{"docker inspect --format='{{$p := .LogPath}}{{$p}}' moviepilot", false},
		{"docker inspect --format='{{json .}}' moviepilot", false},
		{"docker inspect --format='{{printf \"%s\" .}}' moviepilot", false},
		{"docker inspect --format='{{println .}}' moviepilot", false},
	}

	for _, tc := range cases {
		if snippetHasVariables(tc.cmd) != tc.want {
			t.Errorf("cmd %q: snippetHasVariables = %v, want %v", tc.cmd, snippetHasVariables(tc.cmd), tc.want)
		}
		res, ok := applySnippetVariables(tc.cmd, nil)
		if !ok || res != tc.cmd {
			t.Errorf("applySnippetVariables(%q) = (%q, %v), want (%q, true)", tc.cmd, res, ok, tc.cmd)
		}
	}
}

func TestSnippetVariablesMixedWithGoTemplates(t *testing.T) {
	t.Run("mix with field literals", func(t *testing.T) {
		cmd := "ls -lh $(docker inspect --format='{{.LogPath}}' {{container:moviepilot}})"
		if !snippetHasVariables(cmd) {
			t.Errorf("expected variables in %q", cmd)
		}
		res, ok := applySnippetVariables(cmd, nil)
		if !ok {
			t.Fatalf("expected ok with default")
		}
		expected := "ls -lh $(docker inspect --format='{{.LogPath}}' moviepilot)"
		if res != expected {
			t.Errorf("expected %q, got %q", expected, res)
		}
	})

	t.Run("mix with action blocks", func(t *testing.T) {
		cmd := "docker inspect --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' {{container:moviepilot}}"
		res, ok := applySnippetVariables(cmd, nil)
		if !ok {
			t.Fatalf("expected ok with default")
		}
		expected := "docker inspect --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' moviepilot"
		if res != expected {
			t.Errorf("expected %q, got %q", expected, res)
		}
	})

	t.Run("variable named end outside go template block", func(t *testing.T) {
		cmd := "echo {{end}}"
		if !snippetHasVariables(cmd) {
			t.Errorf("expected variables in %q", cmd)
		}
		res, ok := applySnippetVariables(cmd, map[string]string{"end": "done"})
		if !ok || res != "echo done" {
			t.Errorf("expected 'echo done', got %q, %v", res, ok)
		}
	})

	t.Run("variable named end near go template block", func(t *testing.T) {
		cmd := "docker inspect --format='{{.LogPath}}' {{end:done}}"
		res, ok := applySnippetVariables(cmd, nil)
		if !ok || res != "docker inspect --format='{{.LogPath}}' done" {
			t.Errorf("expected 'docker inspect --format='{{.LogPath}}' done', got %q, %v", res, ok)
		}
	})

	t.Run("does not replace Go template actions when snippet variable has same name", func(t *testing.T) {
		cmd := "docker inspect --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' {{end:done}}"
		res, ok := applySnippetVariables(cmd, nil)
		if !ok {
			t.Fatalf("expected ok")
		}
		expected := "docker inspect --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' done"
		if res != expected {
			t.Errorf("expected %q, got %q", expected, res)
		}
	})
}

func TestSnippetVariablesApplyAndPreview(t *testing.T) {
	cmd := "ssh -p {{port:22}} {{user}}@{{host:localhost}}"

	// Missing user without default
	res, ok := applySnippetVariables(cmd, map[string]string{"port": "2222"})
	if ok {
		t.Errorf("expected false due to missing required variable 'user', got res=%q", res)
	}

	// Preview with partial values
	preview := previewSnippetCommand(cmd, map[string]string{"port": "2222"})
	expectedPreview := "ssh -p 2222 {{user}}@localhost"
	if preview != expectedPreview {
		t.Errorf("expected preview %q, got %q", expectedPreview, preview)
	}

	// Apply with all values
	res, ok = applySnippetVariables(cmd, map[string]string{
		"port": "2222",
		"user": "ubuntu",
	})
	if !ok {
		t.Fatalf("expected ok")
	}
	expected := "ssh -p 2222 ubuntu@localhost"
	if res != expected {
		t.Errorf("expected %q, got %q", expected, res)
	}
}
