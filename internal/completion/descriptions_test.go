package completion

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"sync"
	"testing"
)

func TestDescriptionsMatchContentAndFallBack(t *testing.T) {
	data, err := EncodeDescriptions(map[string]string{
		"Delete a branch": "删除分支",
		"Show version":    "显示版本",
	})
	if err != nil {
		t.Fatal(err)
	}
	table, err := DecodeDescriptions(data)
	if err != nil {
		t.Fatal(err)
	}
	for source, want := range map[string]string{
		"Delete a branch":      "删除分支",
		"Show version":         "显示版本",
		"Delete all branches":  "Delete all branches",
		"New upstream command": "New upstream command",
		"":                     "",
	} {
		if got := table.Lookup(source); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", source, got, want)
		}
	}
	var absent *Descriptions
	if got := absent.Lookup("Show version"); got != "Show version" {
		t.Fatalf("nil table = %q", got)
	}
	second, err := EncodeDescriptions(map[string]string{"Show version": "显示版本", "Delete a branch": "删除分支"})
	if err != nil || !bytes.Equal(data, second) {
		t.Fatalf("encoding is not reproducible: %v", err)
	}
	for range 10 {
		var workers sync.WaitGroup
		for range 8 {
			workers.Go(func() {
				if table.Lookup("Delete a branch") != "删除分支" {
					t.Error("concurrent lookup lost a translation")
				}
			})
		}
		workers.Wait()
	}
}

func TestDescriptionsRejectCorruptAssets(t *testing.T) {
	asset, err := EncodeDescriptions(map[string]string{"Delete a branch": "删除分支", "Show version": "显示版本"})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(asset))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	for name, mutate := range map[string]func([]byte) []byte{
		"magic":              func(data []byte) []byte { data[0] = 'X'; return data },
		"count":              func(data []byte) []byte { binary.LittleEndian.PutUint32(data[4:], 0xffffffff); return data },
		"unordered keys":     func(data []byte) []byte { copy(data[56:72], data[40:56]); return data },
		"first offset":       func(data []byte) []byte { binary.LittleEndian.PutUint32(data[72:], 1); return data },
		"descending offsets": func(data []byte) []byte { binary.LittleEndian.PutUint32(data[76:], 0xffffffff); return data },
		"trailing text":      func(data []byte) []byte { return append(data, 'x') },
		"invalid UTF8":       func(data []byte) []byte { data[84] = 0xff; return data },
		"control":            func(data []byte) []byte { data[84] = '\n'; return data },
		"truncated":          func(data []byte) []byte { return data[:len(data)-1] },
		"empty":              func(data []byte) []byte { return nil },
	} {
		t.Run(name, func(t *testing.T) {
			data := mutate(bytes.Clone(raw))
			if len(data) >= 40 {
				digest := sha256.Sum256(data[40:])
				copy(data[8:40], digest[:])
			}
			if _, err := DecodeDescriptions(gzipDescriptionsTest(t, data)); err == nil {
				t.Fatal("corrupt table was accepted")
			}
		})
	}
	asset[len(asset)-1] ^= 0xff
	if _, err := DecodeDescriptions(asset); err == nil {
		t.Fatal("gzip checksum failure was accepted")
	}
	if _, err := DecodeDescriptions(gzipDescriptionsTest(t, make([]byte, 16<<20+1))); err == nil {
		t.Fatal("oversized expansion was accepted")
	}
}

func TestDescriptionsRejectUnnormalizedTranslations(t *testing.T) {
	for _, translated := range []string{"", "\n删除", "删除\x1b[0m", string([]byte{0xff}), " 删除", string(bytes.Repeat([]byte("中"), 161))} {
		if _, err := EncodeDescriptions(map[string]string{"Delete": translated}); err == nil {
			t.Errorf("accepted %q", translated)
		}
	}
}

func TestBuiltinDescriptionsAreSharedAcrossConcurrentReaders(t *testing.T) {
	var workers sync.WaitGroup
	tables := make(chan *Descriptions, 8)
	for range 8 {
		workers.Go(func() {
			table, err := BuiltinChineseDescriptions()
			if err != nil {
				t.Error(err)
				return
			}
			tables <- table
		})
	}
	workers.Wait()
	close(tables)
	var first *Descriptions
	for table := range tables {
		if first == nil {
			first = table
		}
		if table != first || table.Lookup("output the last part of files") != "输出文件末尾的内容" {
			t.Fatal("concurrent builtin readers did not share the same valid table")
		}
	}
}

func BenchmarkChineseDescriptionsDecode(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(chineseDescriptionData)))
	for b.Loop() {
		if _, err := DecodeDescriptions(chineseDescriptionData); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChineseDescriptionLookup(b *testing.B) {
	table, err := BuiltinChineseDescriptions()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if table.Lookup("output the last part of files") != "输出文件末尾的内容" {
			b.Fatal("missing translation")
		}
	}
}

func gzipDescriptionsTest(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
