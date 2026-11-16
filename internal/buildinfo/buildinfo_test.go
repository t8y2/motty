package buildinfo

import "testing"

func TestProductNameMatchesTheBuildTag(t *testing.T) {
	want, wantBadge := "Motty", ""
	if Development {
		want, wantBadge = "Motty Dev", "DEV"
	}
	if ProductName != want || Development != developmentBuild || DockBadge != wantBadge {
		t.Fatalf("ProductName = %q, want %q; Development = %t, want %t; DockBadge = %q, want %q",
			ProductName, want, Development, developmentBuild, DockBadge, wantBadge)
	}
}
