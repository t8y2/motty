package workspace

import (
	"errors"
	"math"
	"slices"
	"testing"
)

func TestNewGridBuildsRowMajorLayouts(t *testing.T) {
	tests := []struct {
		name     string
		sessions []SessionID
		columns  int
		want     []gridRect
	}{
		{
			name: "one", sessions: []SessionID{1}, columns: 1,
			want: []gridRect{{0, 0, 1, 1}},
		},
		{
			name: "two columns", sessions: []SessionID{1, 2}, columns: 2,
			want: []gridRect{{0, 0, 0.5, 1}, {0.5, 0, 0.5, 1}},
		},
		{
			name: "three columns", sessions: []SessionID{1, 2, 3}, columns: 3,
			want: []gridRect{{0, 0, 1.0 / 3, 1}, {1.0 / 3, 0, 1.0 / 3, 1}, {2.0 / 3, 0, 1.0 / 3, 1}},
		},
		{
			name: "four in two columns", sessions: []SessionID{1, 2, 3, 4}, columns: 2,
			want: []gridRect{
				{0, 0, 0.5, 0.5}, {0.5, 0, 0.5, 0.5},
				{0, 0.5, 0.5, 0.5}, {0.5, 0.5, 0.5, 0.5},
			},
		},
		{
			name: "five in two columns expands last row", sessions: []SessionID{1, 2, 3, 4, 5}, columns: 2,
			want: []gridRect{
				{0, 0, 0.5, 1.0 / 3}, {0.5, 0, 0.5, 1.0 / 3},
				{0, 1.0 / 3, 0.5, 1.0 / 3}, {0.5, 1.0 / 3, 0.5, 1.0 / 3},
				{0, 2.0 / 3, 1, 1.0 / 3},
			},
		},
		{
			name: "five in three columns share last row", sessions: []SessionID{1, 2, 3, 4, 5}, columns: 3,
			want: []gridRect{
				{0, 0, 1.0 / 3, 0.5}, {1.0 / 3, 0, 1.0 / 3, 0.5}, {2.0 / 3, 0, 1.0 / 3, 0.5},
				{0, 0.5, 0.5, 0.5}, {0.5, 0.5, 0.5, 0.5},
			},
		},
		{
			name: "seven in three columns expands last row", sessions: []SessionID{1, 2, 3, 4, 5, 6, 7}, columns: 3,
			want: []gridRect{
				{0, 0, 1.0 / 3, 1.0 / 3}, {1.0 / 3, 0, 1.0 / 3, 1.0 / 3}, {2.0 / 3, 0, 1.0 / 3, 1.0 / 3},
				{0, 1.0 / 3, 1.0 / 3, 1.0 / 3}, {1.0 / 3, 1.0 / 3, 1.0 / 3, 1.0 / 3}, {2.0 / 3, 1.0 / 3, 1.0 / 3, 1.0 / 3},
				{0, 2.0 / 3, 1, 1.0 / 3},
			},
		},
		{
			name: "eight in three columns share last row", sessions: []SessionID{1, 2, 3, 4, 5, 6, 7, 8}, columns: 3,
			want: []gridRect{
				{0, 0, 1.0 / 3, 1.0 / 3}, {1.0 / 3, 0, 1.0 / 3, 1.0 / 3}, {2.0 / 3, 0, 1.0 / 3, 1.0 / 3},
				{0, 1.0 / 3, 1.0 / 3, 1.0 / 3}, {1.0 / 3, 1.0 / 3, 1.0 / 3, 1.0 / 3}, {2.0 / 3, 1.0 / 3, 1.0 / 3, 1.0 / 3},
				{0, 2.0 / 3, 0.5, 1.0 / 3}, {0.5, 2.0 / 3, 0.5, 1.0 / 3},
			},
		},
		{
			name: "twelve in three columns", sessions: []SessionID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, columns: 3,
			want: []gridRect{
				{0, 0, 1.0 / 3, 0.25}, {1.0 / 3, 0, 1.0 / 3, 0.25}, {2.0 / 3, 0, 1.0 / 3, 0.25},
				{0, 0.25, 1.0 / 3, 0.25}, {1.0 / 3, 0.25, 1.0 / 3, 0.25}, {2.0 / 3, 0.25, 1.0 / 3, 0.25},
				{0, 0.5, 1.0 / 3, 0.25}, {1.0 / 3, 0.5, 1.0 / 3, 0.25}, {2.0 / 3, 0.5, 1.0 / 3, 0.25},
				{0, 0.75, 1.0 / 3, 0.25}, {1.0 / 3, 0.75, 1.0 / 3, 0.25}, {2.0 / 3, 0.75, 1.0 / 3, 0.25},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layout, err := NewGrid(test.sessions, test.columns)
			if err != nil {
				t.Fatal(err)
			}
			if err := layout.Validate(); err != nil {
				t.Fatal(err)
			}
			assertSessions(t, &layout, test.sessions...)
			assertGridRects(t, &layout, test.want)
		})
	}
}

func TestNewGridIsDeterministicForEveryPaneCount(t *testing.T) {
	for count := 1; count <= MaxPanes; count++ {
		for columns := MinGridColumns; columns <= MaxGridColumns; columns++ {
			sessions := make([]SessionID, count)
			for index := range sessions {
				sessions[index] = SessionID(index + 1)
			}
			first, err := NewGrid(sessions, columns)
			if err != nil {
				t.Fatalf("%d panes in %d columns: %v", count, columns, err)
			}
			second, err := NewGrid(slices.Clone(sessions), columns)
			if err != nil {
				t.Fatalf("%d panes in %d columns: %v", count, columns, err)
			}
			assertSessions(t, &first, sessions...)
			assertSessions(t, &second, sessions...)
			assertDTOEqual(t, first.DTO(), second.DTO())
		}
	}
}

func TestNewGridRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		sessions []SessionID
		columns  int
		want     error
	}{
		{"empty", nil, 1, ErrInvalidPaneCount},
		{"too many", []SessionID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}, 1, ErrInvalidPaneCount},
		{"zero columns", []SessionID{1}, 0, ErrInvalidColumns},
		{"too many columns", []SessionID{1}, MaxGridColumns + 1, ErrInvalidColumns},
		{"zero session", []SessionID{1, 0}, 2, ErrInvalidSession},
		{"duplicate session", []SessionID{1, 2, 1}, 2, ErrSessionExists},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			layout, err := NewGrid(test.sessions, test.columns)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewGrid() error = %v, want %v", err, test.want)
			}
			if layout.Root() != 0 || layout.Len() != 0 {
				t.Fatalf("invalid input returned layout %+v", layout)
			}
		})
	}
}

type gridRect struct {
	x      float32
	y      float32
	width  float32
	height float32
}

func assertGridRects(t *testing.T, layout *Layout, want []gridRect) {
	t.Helper()
	got := appendGridRects(nil, layout, layout.Root(), gridRect{width: 1, height: 1})
	if len(got) != len(want) {
		t.Fatalf("rectangles = %v, want %v", got, want)
	}
	for index := range want {
		if math.Abs(float64(got[index].x-want[index].x)) > 1e-6 ||
			math.Abs(float64(got[index].y-want[index].y)) > 1e-6 ||
			math.Abs(float64(got[index].width-want[index].width)) > 1e-6 ||
			math.Abs(float64(got[index].height-want[index].height)) > 1e-6 {
			t.Fatalf("rectangle %d = %+v, want %+v (all %v)", index, got[index], want[index], got)
		}
	}
}

func appendGridRects(dst []gridRect, layout *Layout, id NodeID, bounds gridRect) []gridRect {
	node, ok := layout.Node(id)
	if !ok {
		return dst
	}
	if node.Kind == KindLeaf {
		return append(dst, bounds)
	}
	first := bounds
	second := bounds
	if node.Axis == Horizontal {
		first.width *= node.Ratio
		second.x += first.width
		second.width -= first.width
	} else {
		first.height *= node.Ratio
		second.y += first.height
		second.height -= first.height
	}
	dst = appendGridRects(dst, layout, node.First, first)
	return appendGridRects(dst, layout, node.Second, second)
}
