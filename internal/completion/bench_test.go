package completion

import "testing"

func BenchmarkBuiltinDecode(b *testing.B) {
	data := BuiltinBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for index := 0; index < b.N; index++ {
		if _, err := Decode(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComplete(b *testing.B) {
	catalog, err := Builtin()
	if err != nil {
		b.Fatal(err)
	}
	lines := []string{"git s", "git commit --a", "docker cont", "docker compose ", "git checkout", "git ", "tar --"}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if result := Complete(catalog, lines[index%len(lines)]); result.Suppressed || len(result.Candidates) == 0 {
			b.Fatalf("no result for %q", lines[index%len(lines)])
		}
	}
}
