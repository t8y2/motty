package completion

import (
	"strings"
	"testing"
)

func TestCommonCatalogCandidatesAreSafeAndDescribed(t *testing.T) {
	catalog, err := Common()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Source != "motty-common" || catalog.License != "Apache-2.0" {
		t.Fatalf("untracked common source: %s %s", catalog.Source, catalog.License)
	}
	for _, root := range catalog.Roots() {
		for _, line := range []string{root.Token[:len(root.Token)-1], root.Token + " -", root.Token + " --", root.Token + " "} {
			result := Complete(catalog, line)
			for _, candidate := range result.Candidates {
				if !SafeInsert(candidate.Insert) || !candidate.FromCatalog {
					t.Errorf("unsafe common candidate for %q: %+v", line, candidate)
				}
				if strings.ContainsAny(candidate.Desc, "\r\n\x1b") {
					t.Errorf("control character in %q: %+v", line, candidate)
				}
			}
		}
	}
}

func TestCommonCatalogKeepsSevenCatOptions(t *testing.T) {
	catalog, err := Common()
	if err != nil {
		t.Fatal(err)
	}
	result := Complete(catalog, "cat -")
	for _, name := range []string{"-b", "-e", "-n", "-s", "-t", "-u", "-v"} {
		if !hasCandidate(result, name) {
			t.Errorf("cat - lacks %s", name)
		}
	}
}

func TestCommonWindowsSuggestionsUseRealOptionSpelling(t *testing.T) {
	catalog, err := Common()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ line, option string }{
		{"pwsh -", "-NoProfile"},
		{"pwsh -No", "-NoProfile"},
		{"pwsh -NoProfile -", "-Command"},
		{"powershell -", "-NoProfile"},
		{"cmd /", "/C"},
		{"dir /", "/A"},
		{"Get-Command -", "-Name"},
		{"Get-Command -Nam", "-Name"},
		{"Get-ChildItem -", "-Path"},
	} {
		if !hasCandidate(Complete(catalog, test.line), test.option) {
			t.Errorf("%s lacks native option %s", test.line, test.option)
		}
	}
}

func TestCommonCommandMatrixHasRepresentativeSyntax(t *testing.T) {
	builtin, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	common, err := Common()
	if err != nil {
		t.Fatal(err)
	}
	groups := []string{
		"cat ls cd cp mv rm mkdir chmod chown grep sed awk find head tail sort uniq cut wc tee xargs du df tar gzip",
		"ssh scp rsync curl wget ping ip ss netstat ps kill top systemctl journalctl apt dnf yum",
		"git docker kubectl make gcc go cargo rustc java javac npm yarn pnpm node python pip brew",
		"pwsh powershell cmd dir Get-Command Get-ChildItem",
	}
	var nameOnly, missing []string
	total := 0
	for _, group := range groups {
		for _, name := range strings.Fields(group) {
			total++
			root := builtin.Root(name) != nil || common.Root(name) != nil
			if !root {
				missing = append(missing, name)
				continue
			}
			complete := false
			for _, catalog := range []*Catalog{builtin, common} {
				for _, suffix := range []string{" ", " -", " /"} {
					for _, candidate := range Complete(catalog, name+suffix).Candidates {
						if candidate.FromCatalog && candidate.Desc != "" && SafeInsert(candidate.Insert) {
							complete = true
						}
					}
				}
			}
			if !complete {
				nameOnly = append(nameOnly, name)
			}
		}
	}
	if len(missing) != 0 || len(nameOnly) != 0 {
		t.Fatalf("matrix audit: complete=%d, name-only=%v, missing=%v", total-len(missing)-len(nameOnly), nameOnly, missing)
	}
}
