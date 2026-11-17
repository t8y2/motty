package workspace

import (
	"errors"
	"slices"
	"testing"
)

func sessionRange(count int) []SessionID { return sessionIDs(count) }

// Every preset must lay out every pane exactly once, for every pane count a
// workspace can hold, and stay a valid tree.
func TestNewLayoutPresetsCoverEveryPane(t *testing.T) {
	for _, spec := range Presets() {
		for count := 1; count <= MaxPanes; count++ {
			sessions := sessionRange(count)
			layout, err := NewLayout(sessions, spec)
			if err != nil {
				t.Fatalf("kind %d columns %d count %d: %v", spec.Kind, spec.Columns, count, err)
			}
			if err := layout.Validate(); err != nil {
				t.Fatalf("kind %d count %d: %v", spec.Kind, count, err)
			}
			got := layout.Sessions(nil)
			slices.Sort(got)
			if !slices.Equal(got, sessions) {
				t.Fatalf("kind %d count %d sessions = %v", spec.Kind, count, got)
			}
		}
	}
}

func TestNewLayoutMainShapes(t *testing.T) {
	right, err := NewLayout(sessionRange(3), LayoutSpec{Kind: LayoutMainRight})
	if err != nil {
		t.Fatal(err)
	}
	assertGridRects(t, &right, []gridRect{{0, 0, mainShare, 1}, {mainShare, 0, 1 - mainShare, 0.5}, {mainShare, 0.5, 1 - mainShare, 0.5}})
	bottom, err := NewLayout(sessionRange(3), LayoutSpec{Kind: LayoutMainBottom})
	if err != nil {
		t.Fatal(err)
	}
	assertGridRects(t, &bottom, []gridRect{{0, 0, 1, mainShare}, {0, mainShare, 0.5, 1 - mainShare}, {0.5, mainShare, 0.5, 1 - mainShare}})
}

// A custom spec numbers its panes by reading order, so it applies to whatever
// sessions fill those places and rebuilds the same arrangement.
func TestCustomSpecRoundTripsThroughPositions(t *testing.T) {
	layout, err := NewLayout(sessionRange(3), GridSpec(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.Move(1, 3, Vertical, 0.5, After); err != nil {
		t.Fatal(err)
	}
	spec := layout.Spec()
	if err := spec.Validate(3); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := NewLayout([]SessionID{10, 20, 30}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if order := rebuilt.Sessions(nil); !slices.Equal(order, []SessionID{10, 20, 30}) {
		t.Fatalf("reading order = %v, want the sessions in the order they were given", order)
	}
	if !rebuilt.Spec().Equal(spec) {
		t.Fatal("a rebuilt layout does not describe itself as the same spec")
	}
}

func TestLayoutSpecValidateRejectsMismatches(t *testing.T) {
	layout, _ := NewLayout(sessionRange(3), GridSpec(2))
	custom := layout.Spec()
	tests := []struct {
		name  string
		spec  LayoutSpec
		count int
		want  error
	}{
		{"zero kind", LayoutSpec{}, 2, ErrInvalidSpec},
		{"preset with columns", LayoutSpec{Kind: LayoutMainRight, Columns: 2}, 2, ErrInvalidSpec},
		{"grid with tree", LayoutSpec{Kind: LayoutGrid, Columns: 2, Tree: custom.Tree}, 3, ErrInvalidSpec},
		{"custom pane count mismatch", custom, 4, ErrInvalidSpec},
		{"custom without tree", LayoutSpec{Kind: LayoutCustom}, 1, ErrInvalidLayout},
		{"no panes", GridSpec(1), 0, ErrInvalidPaneCount},
	}
	for _, test := range tests {
		if err := test.spec.Validate(test.count); !errors.Is(err, test.want) {
			t.Errorf("%s: error = %v, want %v", test.name, err, test.want)
		}
	}
}

// Dropping a missing host from a custom layout keeps the arrangement of the
// others: the survivors stay a valid tree and keep their relative placement.
func TestRetainDropsMissingPositions(t *testing.T) {
	layout, _ := NewLayout(sessionRange(4), GridSpec(2))
	custom := layout.Spec()
	kept, err := custom.Retain([]int{1, 2, 4}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := kept.Validate(3); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := NewLayout(sessionRange(3), kept)
	if err != nil {
		t.Fatal(err)
	}
	assertGridRects(t, &rebuilt, []gridRect{{0, 0, 0.5, 0.5}, {0.5, 0, 0.5, 0.5}, {0, 0.5, 1, 0.5}})
}

// Docking a pane beside another gives a custom layout over the same panes: the
// order lists where each place of the new reading order came from, and the moved
// pane sits on the chosen side of its target.
func TestMovePositionDocksBesideTarget(t *testing.T) {
	spec, order, err := GridSpec(3).MovePosition(3, 1, 3, Vertical, After)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != LayoutCustom {
		t.Fatalf("kind = %d, want custom", spec.Kind)
	}
	// Pane 1 left its column; 2 now fills the left half, 3 and 1 stack on the right.
	if !slices.Equal(order, []int{2, 3, 1}) {
		t.Fatalf("order = %v, want [2 3 1]", order)
	}
	layout, err := NewLayout(sessionRange(3), spec)
	if err != nil {
		t.Fatal(err)
	}
	assertGridRects(t, &layout, []gridRect{{0, 0, 0.5, 1}, {0.5, 0, 0.5, 0.5}, {0.5, 0.5, 0.5, 0.5}})
	if _, _, err := GridSpec(3).MovePosition(3, 2, 2, Vertical, After); err == nil {
		t.Fatal("docking a pane beside itself succeeded")
	}
}
