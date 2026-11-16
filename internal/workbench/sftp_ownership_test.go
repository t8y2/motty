package workbench

import (
	"slices"
	"strings"
	"testing"

	"motty/internal/sftpclient"
)

func ownershipSuggestions() sftpOwnershipUI {
	var ui sftpOwnershipUI
	ui.load(sftpclient.IDNames{
		Users: map[uint32]string{
			1: "daemon", 2: "adams", 3: "zda", 4: "deploy", 5: "root",
			6: "dave", 7: "david", 8: "dawson", 9: "dana", 10: "darwin", 11: "daw", 12: "day",
		},
		Groups: map[uint32]string{20: "staff", 80: "admin", 81: "_appserveradm"},
	})
	return ui
}

func suggestionNames(accounts []sftpAccountOption) []string {
	names := make([]string, len(accounts))
	for index, account := range accounts {
		names[index] = account.name
	}
	return names
}

func TestSFTPOwnershipSuggestionsRankExactThenPrefixThenContained(t *testing.T) {
	ui := ownershipSuggestions()
	if got, want := suggestionNames(ui.matches(0, "da")), []string{"daemon", "dana", "darwin", "dave", "david", "daw", "dawson", "day"}; !slices.Equal(got, want) {
		t.Fatalf("prefix matches = %v, want %v", got, want)
	}
	// A name only contained in another one comes after every prefix match, so
	// a long account list cannot bury the one the user is typing.
	if got, want := suggestionNames(ui.matches(0, "adams")), []string{"adams"}; !slices.Equal(got, want) {
		t.Fatalf("exact match = %v, want %v", got, want)
	}
	// "zda" is only contained in one name; the prefix matches above filled the
	// list, so a contained-only query still finds it.
	if got := suggestionNames(ui.matches(0, "zda")); !slices.Equal(got, []string{"zda"}) {
		t.Fatalf("contained match = %v", got)
	}
}

func TestSFTPOwnershipSuggestionsMatchNumericIDsAndGroups(t *testing.T) {
	ui := ownershipSuggestions()
	if got, want := suggestionNames(ui.matches(1, "80")), []string{"admin"}; !slices.Equal(got, want) {
		t.Fatalf("group by id = %v, want %v", got, want)
	}
	if got, want := suggestionNames(ui.matches(1, "adm")), []string{"admin", "_appserveradm"}; !slices.Equal(got, want) {
		t.Fatalf("group by name = %v, want %v", got, want)
	}
}

func TestSFTPOwnershipSuggestionsAreBoundedAndSorted(t *testing.T) {
	ui := ownershipSuggestions()
	all := ui.matches(0, "")
	if len(all) != sftpOwnershipMatches {
		t.Fatalf("empty query returned %d accounts, want %d", len(all), sftpOwnershipMatches)
	}
	for index := 1; index < len(all); index++ {
		if all[index-1].name > all[index].name {
			t.Fatalf("suggestions are not sorted: %v", suggestionNames(all))
		}
	}
	if got, want := suggestionNames(ui.matches(0, "not-a-user")), []string(nil); !slices.Equal(got, want) {
		t.Fatalf("unmatched query = %v", got)
	}
}

func TestSFTPOwnershipValuesAreNamesOrNumericIDs(t *testing.T) {
	for _, valid := range []string{"deploy", "1001", "_appserveradm"} {
		if !validSFTPOwnershipValue(valid) {
			t.Fatalf("%q was refused", valid)
		}
	}
	for _, invalid := range []string{"", "de ploy", "user:group", strings.Repeat("a", sftpOwnershipMaxRunes+1)} {
		if validSFTPOwnershipValue(invalid) {
			t.Fatalf("%q was accepted", invalid)
		}
	}
}

// The suggestion list must reach the central pointer dispatcher like the
// chmod grid does: it is painted by hand into a dialog the page declares.
func TestSFTPOwnershipSuggestionsPickThroughTheEventPass(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPChown)
	if view.dialog != sftpDialogChown {
		t.Fatalf("dialog = %d, want ownership", view.dialog)
	}
	// The fixture has no account files; give the dialog the names a listing
	// would have read.
	view.ownership.load(sftpclient.IDNames{
		Users:  map[uint32]string{1001: "deploy", 80: "admin"},
		Groups: map[uint32]string{80: "admin", 50: "staff"},
	})
	view.ownership.lastQuery = [2]string{view.dialogEditor.String(), view.dialogEditor2.String()}

	driver := modalEventDriver(t, state)
	clickPaintedHit(t, driver, hitSFTPDialogField2)
	if !view.ownership.open[1] {
		t.Fatal("clicking the group field did not open its suggestions")
	}
	for index, want := range []string{"admin", "staff"} {
		hit, ok := hitByIDInSnapshot(driver.Snapshot(), hitSFTPChownGroupItem.At(index))
		if !ok || hit.Label != want {
			t.Fatalf("group suggestion %d = %+v, painted %v, want %q", index, hit, ok, want)
		}
	}
	clickPaintedHit(t, driver, hitSFTPChownGroupItem.At(1))
	if got := view.dialogEditor2.String(); got != "staff" {
		t.Fatalf("picked group = %q, want staff", got)
	}
	if view.ownership.open[1] {
		t.Fatal("picking a suggestion left the list open")
	}
}
