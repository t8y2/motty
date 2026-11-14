package rfb

import "testing"

func TestSecuritySelectionPrefersRA2AndPreservesLegacyBehavior(t *testing.T) {
	for _, test := range []struct {
		name     string
		offered  []byte
		password bool
		account  bool
		want     byte
	}{
		{"RA2 before legacy", []byte{2, 1, 5}, true, true, 5},
		{"RA2 with unsupported extensions", []byte{129, 130, 6, 19, 5, 5, 1, 2}, true, true, 5},
		{"ARD with an account", []byte{2, 1, 30}, true, true, 30},
		{"ARD without an account falls back to the password", []byte{2, 1, 30}, true, false, 2},
		{"ARD only", []byte{30, 33, 36, 35}, false, true, 30},
		{"ARD only without an account", []byte{30, 33, 36, 35}, true, false, 30},
		{"legacy password", []byte{1, 2}, true, false, 2},
		{"legacy none", []byte{1, 2}, false, false, 1},
		{"legacy password question", []byte{2}, false, false, 2},
		{"extensions only", []byte{6, 129, 130, 18, 19, 13}, true, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := chooseSecurity(test.offered, test.password, test.account); got != test.want {
				t.Fatalf("selected type %d, want %d", got, test.want)
			}
		})
	}
}
