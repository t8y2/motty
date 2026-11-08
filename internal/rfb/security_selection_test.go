package rfb

import "testing"

func TestSecuritySelectionPrefersRA2AndPreservesLegacyBehavior(t *testing.T) {
	for _, test := range []struct {
		name     string
		offered  []byte
		password bool
		want     byte
	}{
		{"RA2 before legacy", []byte{2, 1, 5}, true, 5},
		{"RA2 with unsupported extensions", []byte{129, 130, 6, 19, 5, 5, 1, 2}, true, 5},
		{"legacy password", []byte{1, 2}, true, 2},
		{"legacy none", []byte{1, 2}, false, 1},
		{"legacy password question", []byte{2}, false, 2},
		{"extensions only", []byte{6, 129, 130, 18, 19, 13, 30}, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := chooseSecurity(test.offered, test.password); got != test.want {
				t.Fatalf("selected type %d, want %d", got, test.want)
			}
		})
	}
}
