package workspace

import "testing"

func BenchmarkNewGridTwelve(b *testing.B) {
	sessions := make([]SessionID, MaxPanes)
	for index := range sessions {
		sessions[index] = SessionID(index + 1)
	}
	b.ReportAllocs()
	for range b.N {
		if _, err := NewGrid(sessions, MaxGridColumns); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetRatio(b *testing.B) {
	layout := benchmarkLayout(b, 64)
	split := layout.Root()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		ratio := float32(40+index%20) / 100
		if err := layout.SetRatio(split, ratio); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMove64(b *testing.B) {
	layout := benchmarkLayout(b, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		placement := Before
		if index&1 != 0 {
			placement = After
		}
		if err := layout.Move(64, 1, Vertical, 0.5, placement); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSessions256(b *testing.B) {
	layout := benchmarkLayout(b, 256)
	dst := make([]SessionID, 0, layout.Len())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		dst = layout.Sessions(dst[:0])
	}
}

func BenchmarkDTO256(b *testing.B) {
	layout := benchmarkLayout(b, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = layout.DTO()
	}
}

func BenchmarkRestore256(b *testing.B) {
	dto := benchmarkLayout(b, 256).DTO()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Restore(dto); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkLayout(tb testing.TB, count SessionID) *Layout {
	tb.Helper()
	layout, err := New(1)
	if err != nil {
		tb.Fatal(err)
	}
	for session := SessionID(2); session <= count; session++ {
		if err := layout.Split(session-1, session, Horizontal, 0.5, After); err != nil {
			tb.Fatal(err)
		}
	}
	return &layout
}
