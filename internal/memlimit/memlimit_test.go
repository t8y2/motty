package memlimit

import "testing"

func TestForDesktopsFollowsThePicture(t *testing.T) {
	const mib = 1 << 20

	if got := ForDesktops(nil); got != 0 {
		t.Fatalf("no pictures: %d, want 0 (no limit)", got)
	}
	if got := ForDesktops([]Size{{0, 0}, {-5, 10}}); got != 0 {
		t.Fatalf("empty pictures: %d, want 0", got)
	}

	small := ForDesktops([]Size{{1920, 1080}})
	if small != floorBytes {
		t.Fatalf("1080p limit %d, want the %d floor", small, floorBytes)
	}

	fiveK := ForDesktops([]Size{{5120, 2880}})
	if fiveK <= small {
		t.Fatalf("5K limit %d not above 1080p %d", fiveK, small)
	}
	// The measured full application holds about 600 MB at 5K; the limit must
	// bound it without sitting below the live picture.
	if fiveK < 600*mib || fiveK > 1024*mib {
		t.Fatalf("5K limit %d outside the useful range", fiveK)
	}

	eightK := ForDesktops([]Size{{7680, 4320}})
	if eightK <= fiveK {
		t.Fatalf("8K limit %d not above 5K %d", eightK, fiveK)
	}

	two := ForDesktops([]Size{{5120, 2880}, {5120, 2880}})
	if two <= fiveK {
		t.Fatalf("two 5K pictures: %d not above one %d", two, fiveK)
	}
	if two >= 2*fiveK {
		t.Fatalf("two 5K pictures: %d, want the shared base and cache counted once", two)
	}

	var many []Size
	for i := 0; i < 16; i++ {
		many = append(many, Size{7680, 4320})
	}
	if got := ForDesktops(many); got != ceilingBytes {
		t.Fatalf("sixteen 8K pictures: %d, want the %d ceiling", got, ceilingBytes)
	}
}
