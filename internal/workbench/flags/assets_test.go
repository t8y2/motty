package flags

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestRenderedFlagsMatchTheirSVGSource(t *testing.T) {
	data, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files map[string]struct{ SVG, PNG string }
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 13 {
		t.Fatalf("flag asset count = %d, want 13", len(manifest.Files))
	}
	for name, hashes := range manifest.Files {
		for path, want := range map[string]string{"svg/" + name: hashes.SVG, "png/" + name[:len(name)-4] + ".png": hashes.PNG} {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got := sha256.Sum256(source)
			if hex.EncodeToString(got[:]) != want {
				t.Errorf("%s changed; regenerate with tools/flags", path)
			}
		}
	}
}
