package rfb

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func FuzzEAXCipherAuthentication(f *testing.F) {
	for _, keySize := range []int{16, 32} {
		f.Add(bytes.Repeat([]byte{1}, keySize), []byte{}, []byte{}, []byte{}, uint32(0))
		f.Add(bytes.Repeat([]byte{2}, keySize), make([]byte, 16), []byte{0, 17}, bytes.Repeat([]byte{3}, 17), uint32(17))
	}
	f.Fuzz(func(t *testing.T, key, nonce, header, message []byte, mutation uint32) {
		if (len(key) != 16 && len(key) != 32) || len(nonce) > 256 || len(header) > 4096 || len(message) > eaxMaxRecord+1 {
			return
		}
		cipher, err := newEAXCipher(key)
		if err != nil {
			t.Fatal(err)
		}
		sealed := cipher.seal(nonce, message, header)
		plain, err := cipher.open(nonce, sealed, header)
		if err != nil || !bytes.Equal(plain, message) {
			t.Fatal("authenticated message did not round trip")
		}
		for _, field := range []string{"ciphertext or tag", "nonce", "header", "truncated tag"} {
			n, h, wire := bytes.Clone(nonce), bytes.Clone(header), bytes.Clone(sealed)
			switch field {
			case "ciphertext or tag":
				wire[int(mutation%uint32(len(wire)))] ^= 1
			case "nonce":
				n = append(n, 0)
			case "header":
				h = append(h, 0)
			case "truncated tag":
				wire = wire[:eaxTagSize-1]
			}
			if leaked, err := cipher.open(n, wire, h); leaked != nil || !errors.Is(err, ErrProtocol) {
				t.Fatalf("%s exposed unauthenticated plaintext", field)
			}
		}
	})
}

func FuzzEAXRecordInput(f *testing.F) {
	for _, keySize := range []int{16, 32} {
		key := bytes.Repeat([]byte{byte(keySize)}, keySize)
		cipher, err := newEAXCipher(key)
		if err != nil {
			f.Fatal(err)
		}
		for _, message := range [][]byte{nil, []byte("authenticated record")} {
			header := []byte{byte(len(message) >> 8), byte(len(message))}
			wire := append(bytes.Clone(header), cipher.seal(make([]byte, 16), message, header)...)
			f.Add(wire, uint8(1), keySize == 32)
		}
	}
	f.Add([]byte{}, uint8(0), false)
	f.Add([]byte{0xff, 0xff, 1}, uint8(31), true)
	f.Fuzz(func(t *testing.T, input []byte, fragment uint8, aes256 bool) {
		if len(input) > 2*(eaxMaxRecord+2+eaxTagSize) {
			return
		}
		keySize := 16
		if aes256 {
			keySize = 32
		}
		key := bytes.Repeat([]byte{byte(keySize)}, keySize)
		// A finite memory connection keeps malformed declared lengths bounded;
		// compare whole reads with arbitrary transport fragments.
		wholeRaw := &eaxMemoryConn{reader: bytes.NewReader(input)}
		whole := eaxTestConn(t, wholeRaw, key)
		want, wantErr := io.ReadAll(whole)
		chunkedRaw := &eaxMemoryConn{reader: bytes.NewReader(input), readLimit: int(fragment%31) + 1}
		chunked := eaxTestConn(t, chunkedRaw, key)
		got, gotErr := io.ReadAll(chunked)
		if !bytes.Equal(got, want) || (gotErr == nil) != (wantErr == nil) || errors.Is(gotErr, ErrProtocol) != errors.Is(wantErr, ErrProtocol) {
			t.Fatal("fragmentation changed plaintext or authentication outcome")
		}
		if len(got) > len(input) {
			t.Fatal("record input expanded beyond its ciphertext")
		}
		if errors.Is(gotErr, ErrProtocol) {
			if !chunkedRaw.closed {
				t.Fatal("invalid record left the connection open")
			}
			if n, err := chunked.Read(make([]byte, 1)); n != 0 || !errors.Is(err, ErrProtocol) {
				t.Fatal("invalid record exposed more plaintext")
			}
			if n, err := chunked.Write([]byte{1}); n != 0 || !errors.Is(err, ErrProtocol) {
				t.Fatal("invalid record allowed later writes")
			}
		}
	})
}
