//go:build windows

package serial

import "testing"

func TestAMissingPortFailsToOpenAndListingWorks(t *testing.T) {
	if _, err := Start(Config{Device: "COM98", Settings: Default}); err == nil {
		t.Fatal("opened COM98")
	}
	for _, port := range Ports() {
		if comNumber(port.Device) == 0 {
			t.Errorf("odd port %+v", port)
		}
	}
}

func TestComNumbersSortNumerically(t *testing.T) {
	if comNumber("COM10") != 10 || comNumber("COM3") != 3 || comNumber("COM") != 0 {
		t.Fatal("comNumber")
	}
}

// A port keeps the line whoever opened it last left behind, and Windows takes
// the frame from the DCB alone. The settings must overwrite every field they
// name, or another program's parity silently decides how this one reads.
func TestTheSettingsRewriteTheWholeLine(t *testing.T) {
	stale := dcb{
		BaudRate: 115200, ByteSize: 7, Parity: 2, StopBits: 2,
		Flags: flagBinary | flagParity | flagOutxCTS | 2*flagRTSControl,
	}
	for _, settings := range []Settings{
		Default,
		{Baud: 9600, DataBits: 8, Parity: ParityNone, StopBits: 1},
		{Baud: 19200, DataBits: 5, Parity: ParitySpace, StopBits: 2, Flow: FlowHardware},
	} {
		state := stale
		applySettings(&state, settings)
		wantStop := uint8(0) // ONESTOPBIT
		if settings.StopBits == 2 {
			wantStop = 2 // TWOSTOPBITS
		}
		if state.BaudRate != uint32(settings.Baud) || state.ByteSize != settings.DataBits ||
			state.Parity != uint8(settings.Parity) || state.StopBits != wantStop {
			t.Errorf("%v: kept %d %d%c%d", settings, state.BaudRate, state.ByteSize, "NOEMS"[state.Parity], state.StopBits)
		}
		if hasParity := state.Flags&flagParity != 0; hasParity != (settings.Parity != ParityNone) {
			t.Errorf("%v: fParity is %v", settings, hasParity)
		}
	}
}
