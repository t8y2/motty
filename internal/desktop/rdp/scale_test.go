package rdp

import (
	"testing"

	rdpcore "motty/internal/rdpcore"
)

// A server refuses a scale that is not one Windows offers, and the library
// refuses to connect with one: whatever a display reports must land on a level.
func TestScalePercentIsAlwaysALevelWindowsOffers(t *testing.T) {
	for scale, want := range map[float32]uint32{0: 100, 1: 100, 1.1: 100, 1.25: 125, 1.33: 125, 1.5: 150, 1.75: 175, 2: 200, 2.2: 225, 3: 300, 9: 500} {
		got := scalePercent(scale)
		if got != want {
			t.Errorf("scalePercent(%v) = %d, want %d", scale, got, want)
		}
		options := rdpcore.DefaultOptions()
		options.Host, options.DesktopScaleFactor = "example.com", got
		if err := options.Validate(); err != nil {
			t.Errorf("scale %v gives %d, which the library refuses: %v", scale, got, err)
		}
	}
}

func TestRequestedSizeIsWhatTheDisplayChannelAccepts(t *testing.T) {
	for _, test := range []struct{ width, height, wantWidth, wantHeight int }{
		{1751, 1007, 1750, 1007},
		{10, 10, minSize, minSize},
		{1 << 20, 1 << 20, maxSize, maxSize},
	} {
		if width, height := clampSize(test.width, test.height); width != test.wantWidth || height != test.wantHeight {
			t.Errorf("clampSize(%d, %d) = %d, %d, want %d, %d", test.width, test.height, width, height, test.wantWidth, test.wantHeight)
		}
	}
}
