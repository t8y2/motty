package completion

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	descriptionHeaderSize = 40
	descriptionKeySize    = 16
	maxDescriptionEntries = 100000
	maxDescriptionBytes   = 16 << 20
	maxDescriptionGzip    = 8 << 20
)

type Descriptions struct {
	keys    string
	offsets string
	text    string
}

func (table *Descriptions) Lookup(english string) string {
	if table == nil || english == "" || len(english) > 160*4 {
		return english
	}
	digest := sha256.Sum256([]byte(english))
	key := string(digest[:descriptionKeySize])
	count := len(table.keys) / descriptionKeySize
	index := sort.Search(count, func(index int) bool {
		return table.keys[index*descriptionKeySize:(index+1)*descriptionKeySize] >= key
	})
	if index == count || table.keys[index*descriptionKeySize:(index+1)*descriptionKeySize] != key {
		return english
	}
	start := binary.LittleEndian.Uint32([]byte(table.offsets[index*4:]))
	end := binary.LittleEndian.Uint32([]byte(table.offsets[(index+1)*4:]))
	return table.text[start:end]
}

func EncodeDescriptions(translations map[string]string) ([]byte, error) {
	if len(translations) > maxDescriptionEntries {
		return nil, errors.New("too many completion translations")
	}
	type entry struct {
		key  [descriptionKeySize]byte
		text string
	}
	entries := make([]entry, 0, len(translations))
	textBytes := 0
	for english, translated := range translations {
		if english == "" || translated == "" || !isNormalized(english, 160) || !isNormalized(translated, 160) {
			return nil, fmt.Errorf("invalid completion translation for %q", NormalizeText(english, 160))
		}
		digest := sha256.Sum256([]byte(english))
		entries = append(entries, entry{key: [descriptionKeySize]byte(digest[:descriptionKeySize]), text: translated})
		textBytes += len(translated)
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare(entries[left].key[:], entries[right].key[:]) < 0
	})
	indexBytes := len(entries)*descriptionKeySize + (len(entries)+1)*4
	if descriptionHeaderSize+indexBytes+textBytes > maxDescriptionBytes {
		return nil, errors.New("completion translations exceed size limit")
	}
	raw := make([]byte, descriptionHeaderSize+indexBytes+textBytes)
	copy(raw, "MZH1")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(entries)))
	keys := raw[descriptionHeaderSize : descriptionHeaderSize+len(entries)*descriptionKeySize]
	offsets := raw[descriptionHeaderSize+len(keys) : descriptionHeaderSize+indexBytes]
	text := raw[descriptionHeaderSize+indexBytes:]
	position := 0
	for index, item := range entries {
		if index > 0 && item.key == entries[index-1].key {
			return nil, errors.New("completion description key collision")
		}
		copy(keys[index*descriptionKeySize:], item.key[:])
		binary.LittleEndian.PutUint32(offsets[index*4:], uint32(position))
		position += copy(text[position:], item.text)
	}
	binary.LittleEndian.PutUint32(offsets[len(entries)*4:], uint32(position))
	digest := sha256.Sum256(raw[descriptionHeaderSize:])
	copy(raw[8:descriptionHeaderSize], digest[:])
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(raw); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if compressed.Len() > maxDescriptionGzip {
		return nil, errors.New("compressed completion translations exceed size limit")
	}
	return compressed.Bytes(), nil
}

func DecodeDescriptions(compressed []byte) (*Descriptions, error) {
	if len(compressed) > maxDescriptionGzip {
		return nil, errors.New("compressed completion translations exceed size limit")
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, maxDescriptionBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxDescriptionBytes || len(raw) < descriptionHeaderSize+4 || string(raw[:4]) != "MZH1" {
		return nil, errors.New("invalid completion translation header")
	}
	count := int(binary.LittleEndian.Uint32(raw[4:]))
	if count > maxDescriptionEntries || count < 0 {
		return nil, errors.New("too many completion translations")
	}
	indexEnd := descriptionHeaderSize + count*descriptionKeySize + (count+1)*4
	if indexEnd > len(raw) {
		return nil, errors.New("truncated completion translation index")
	}
	digest := sha256.Sum256(raw[descriptionHeaderSize:])
	if !bytes.Equal(raw[8:descriptionHeaderSize], digest[:]) {
		return nil, errors.New("completion translation checksum mismatch")
	}
	data := string(raw)
	table := &Descriptions{
		keys:    data[descriptionHeaderSize : descriptionHeaderSize+count*descriptionKeySize],
		offsets: data[descriptionHeaderSize+count*descriptionKeySize : indexEnd],
		text:    data[indexEnd:],
	}
	previous := uint32(0)
	for index := 0; index <= count; index++ {
		offset := binary.LittleEndian.Uint32([]byte(table.offsets[index*4:]))
		if index == 0 && offset != 0 || offset < previous || uint64(offset) > uint64(len(table.text)) {
			return nil, errors.New("invalid completion translation offset")
		}
		if index > 0 {
			translated := table.text[previous:offset]
			if translated == "" || !isNormalized(translated, 160) {
				return nil, errors.New("invalid completion translation text")
			}
		}
		if index > 0 && index < count && table.keys[(index-1)*descriptionKeySize:index*descriptionKeySize] >= table.keys[index*descriptionKeySize:(index+1)*descriptionKeySize] {
			return nil, errors.New("unordered completion translation keys")
		}
		previous = offset
	}
	if int(previous) != len(table.text) {
		return nil, errors.New("trailing completion translation text")
	}
	return table, nil
}
