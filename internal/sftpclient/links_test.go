package sftpclient

import "testing"

func TestNormalizeWindowsJunctionTarget(t *testing.T) {
	root := `D:\app\.output`
	link := `D:\app\.output\server\node_modules\vue`
	inside := `\??\D:\app\.output\server\node_modules\.pnpm\vue@3.5.0\node_modules\vue`
	if got, ok := normalizeWindowsJunctionTarget(root, link, inside); !ok || got != ".pnpm/vue@3.5.0/node_modules/vue" {
		t.Fatalf("in-tree junction target = (%q, %t), want (.pnpm/vue@3.5.0/node_modules/vue, true)", got, ok)
	}

	// Drive-letter case and \\?\ prefix differences still resolve inside root.
	insideLowerDrive := `\\?\d:\app\.output\server\node_modules\.pnpm\vue@3.5.0\node_modules\vue`
	if got, ok := normalizeWindowsJunctionTarget(root, link, insideLowerDrive); !ok || got != ".pnpm/vue@3.5.0/node_modules/vue" {
		t.Fatalf("lowercase-drive junction target = (%q, %t), want (.pnpm/vue@3.5.0/node_modules/vue, true)", got, ok)
	}

	scopedLink := `D:\app\.output\server\node_modules\@vue\runtime-core`
	scopedInside := `\??\D:\app\.output\server\node_modules\.pnpm\@vue+runtime-core@3.5.0\node_modules\@vue\runtime-core`
	if got, ok := normalizeWindowsJunctionTarget(root, scopedLink, scopedInside); !ok || got != "../.pnpm/@vue+runtime-core@3.5.0/node_modules/@vue/runtime-core" {
		t.Fatalf("scoped junction target = (%q, %t), want (../.pnpm/@vue+runtime-core@3.5.0/node_modules/@vue/runtime-core, true)", got, ok)
	}

	outside := `\??\D:\pnpm-store\v3\files\vue`
	if got, ok := normalizeWindowsJunctionTarget(root, link, outside); ok || got != "" {
		t.Fatalf("external junction target = (%q, %t), want (\"\", false)", got, ok)
	}

	otherDrive := `\??\E:\app\.output\server\node_modules\vue`
	if got, ok := normalizeWindowsJunctionTarget(root, link, otherDrive); ok || got != "" {
		t.Fatalf("other-drive junction target = (%q, %t), want (\"\", false)", got, ok)
	}
}
