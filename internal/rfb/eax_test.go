package rfb

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func eaxHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestEAXPublishedVectors(t *testing.T) {
	// The first ten cases are the EAX paper's published AES-128 vectors:
	// https://web.cs.ucdavis.edu/~rogaway/papers/eax.pdf
	// AES-256 cases are Wycheproof aes_eax_test.json tcIds 93 through 96:
	// https://github.com/C2SP/wycheproof/blob/main/testvectors_v1/aes_eax_test.json
	vectors := []struct{ key, nonce, header, plaintext, ciphertext string }{
		{"233952dee4d5ed5f9b9c6d6ff80ff478", "62ec67f9c3a4a407fcb2a8c49031a8b3", "6bfb914fd07eae6b", "", "e037830e8389f27b025a2d6527e79d01"},
		{"91945d3f4dcbee0bf45ef52255f095a4", "becaf043b0a23d843194ba972c66debd", "fa3bfd4806eb53fa", "f7fb", "19dd5c4c9331049d0bdab0277408f67967e5"},
		{"01f74ad64077f2e704c0f60ada3dd523", "70c3db4f0d26368400a10ed05d2bff5e", "234a3463c1264ac6", "1a47cb4933", "d851d5bae03a59f238a23e39199dc9266626c40f80"},
		{"d07cf6cbb7f313bdde66b727afd3c5e8", "8408dfff3c1a2b1292dc199e46b7d617", "33cce2eabff5a79d", "481c9e39b1", "632a9d131ad4c168a4225d8e1ff755939974a7bede"},
		{"35b6d0580005bbc12b0587124557d2c2", "fdb6b06676eedc5c61d74276e1f8e816", "aeb96eaebe2970e9", "40d0c07da5e4", "071dfe16c675cb0677e536f73afe6a14b74ee49844dd"},
		{"bd8e6e11475e60b268784c38c62feb22", "6eac5c93072d8e8513f750935e46da1b", "d4482d1ca78dce0f", "4de3b35c3fc039245bd1fb7d", "835bb4f15d743e350e728414abb8644fd6ccb86947c5e10590210a4f"},
		{"7c77d6e813bed5ac98baa417477a2e7d", "1a8c98dcd73d38393b2bf1569deefc19", "65d2017990d62528", "8b0a79306c9ce7ed99dae4f87f8dd61636", "02083e3979da014812f59f11d52630da30137327d10649b0aa6e1c181db617d7f2"},
		{"5fff20cafab119ca2fc73549e20f5b0d", "dde59b97d722156d4d9aff2bc7559826", "54b9f04e6a09189a", "1bda122bce8a8dbaf1877d962b8592dd2d56", "2ec47b2c4954a489afc7ba4897edcdae8cc33b60450599bd02c96382902aef7f832a"},
		{"a4a4782bcffd3ec5e7ef6d8c34a56123", "b781fcf2f75fa5a8de97a9ca48e522ec", "899a175897561d7e", "6cf36720872b8513f6eab1a8a44438d5ef11", "0de18fd0fdd91e7af19f1d8ee8733938b1e8e7f6d2231618102fdb7fe55ff1991700"},
		{"8395fcf1e95bebd697bd010bc766aac3", "22e7add93cfc6393c57ec0b3c17d6b44", "126735fcc320d25a", "ca40d7446e545ffaed3bd12a740a659ffbbb3ceab7", "cb8920f87a6c75cff39627b56e3ed197c552d295a7cfc46afc253b4652b1af3795b124ab6e"},
		{"80ba3192c803ce965ea371d5ff073cf0f43b6a2ab576b208426e11409c09b9b0", "4da5bf8dfd5852c1ea12379d", "", "", "4d293af9a8fe4ac034f14b14334c16ae"},
		{"cc56b680552eb75008f5484b4cb803fa5063ebd6eab91f6ab6aef4916a766273", "99e23ec48985bccdeeab60f1", "", "2a", "8cc460d5ff45235c3c2491c7e6a32491d6"},
		{"51e4bf2bad92b7aff1a4bc05550ba81df4b96fabf41c12c7b00e60e48db7e152", "4f07afedfdc3b6c2361823d3", "", "be3308f72a2c6aed", "6038296421fb50070a91c72219c0b9ad716accd910e04e13"},
		{"59d4eafb4de0cfc7d3db99a8f54b15d7b39f0acc8da69763b019c1699f87674a", "2fcb1b38a99e71b84740ad9b", "", "549b365af913f3b081131ccb6b825588", "c4066e265a948f40e05e37fa400fde1b611de27128955c54edd7a4d6d23e78ee"},
	}
	for index, v := range vectors {
		t.Run(fmt.Sprint(index+1), func(t *testing.T) {
			key, nonce := eaxHex(t, v.key), eaxHex(t, v.nonce)
			header, plaintext, ciphertext := eaxHex(t, v.header), eaxHex(t, v.plaintext), eaxHex(t, v.ciphertext)
			c, err := newEAXCipher(key)
			if err != nil {
				t.Fatal(err)
			}
			if got := c.seal(nonce, plaintext, header); !bytes.Equal(got, ciphertext) {
				t.Fatalf("published ciphertext = %x, want %x", got, ciphertext)
			}
			got, err := c.open(nonce, ciphertext, header)
			if err != nil || !bytes.Equal(got, plaintext) {
				t.Fatalf("published plaintext = %x, %v; want %x", got, err, plaintext)
			}
		})
	}
}

func TestEAXRejectsUnauthenticatedInputs(t *testing.T) {
	c, err := newEAXCipher(bytes.Repeat([]byte{1}, 16))
	if err != nil {
		t.Fatal(err)
	}
	nonce, header := make([]byte, 16), []byte{0, 4}
	sealed := c.seal(nonce, []byte("text"), header)
	for _, field := range []string{"nonce", "header", "ciphertext", "tag", "short tag"} {
		t.Run(field, func(t *testing.T) {
			n, h, data := bytes.Clone(nonce), bytes.Clone(header), bytes.Clone(sealed)
			switch field {
			case "nonce":
				n[0] ^= 1
			case "header":
				h[0] ^= 1
			case "ciphertext":
				data[0] ^= 1
			case "tag":
				data[len(data)-1] ^= 1
			case "short tag":
				data = data[:15]
			}
			if got, err := c.open(n, data, h); got != nil || !errors.Is(err, ErrProtocol) {
				t.Fatalf("unauthenticated input exposed %x; error %v", got, err)
			}
		})
	}
}

type eaxMemoryConn struct {
	reader                *bytes.Reader
	output                bytes.Buffer
	readLimit, writeLimit int
	writeFailureAt        int
	writeFailure          error
	closed                bool
	deadlines             [3]time.Time
}

func (conn *eaxMemoryConn) Read(dst []byte) (int, error) {
	if conn.closed {
		return 0, net.ErrClosed
	}
	if conn.reader == nil {
		return 0, io.EOF
	}
	if conn.readLimit > 0 {
		dst = dst[:min(len(dst), conn.readLimit)]
	}
	return conn.reader.Read(dst)
}

func (conn *eaxMemoryConn) Write(src []byte) (int, error) {
	if conn.closed {
		return 0, net.ErrClosed
	}
	if conn.writeLimit > 0 {
		src = src[:min(len(src), conn.writeLimit)]
	}
	if conn.writeFailure != nil {
		remaining := max(0, conn.writeFailureAt-conn.output.Len())
		if remaining <= len(src) {
			n, _ := conn.output.Write(src[:remaining])
			return n, conn.writeFailure
		}
	}
	return conn.output.Write(src)
}

func (conn *eaxMemoryConn) Close() error                  { conn.closed = true; return nil }
func (conn *eaxMemoryConn) LocalAddr() net.Addr           { return &net.TCPAddr{Port: 1000} }
func (conn *eaxMemoryConn) RemoteAddr() net.Addr          { return &net.TCPAddr{Port: 2000} }
func (conn *eaxMemoryConn) SetDeadline(t time.Time) error { conn.deadlines[0] = t; return nil }
func (conn *eaxMemoryConn) SetReadDeadline(t time.Time) error {
	conn.deadlines[1] = t
	return nil
}
func (conn *eaxMemoryConn) SetWriteDeadline(t time.Time) error {
	conn.deadlines[2] = t
	return nil
}

func eaxTestConn(t *testing.T, raw net.Conn, key []byte) *eaxConn {
	t.Helper()
	stream, err := newEAXConn(raw, key, key)
	if err != nil {
		t.Fatal(err)
	}
	return stream.(*eaxConn)
}

func eaxTestRecord(t *testing.T, key, message []byte, nonce [16]byte) []byte {
	t.Helper()
	c, err := newEAXCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	header := binary.BigEndian.AppendUint16(nil, uint16(len(message)))
	return append(header, c.seal(nonce[:], message, header)...)
}

func TestEAXRecordBoundariesAndChunkedIO(t *testing.T) {
	for _, keyBytes := range []int{16, 32} {
		for _, length := range []int{0, 1, 16, 17, 65535, 65536, 131071} {
			t.Run(fmt.Sprintf("key%d/length%d", keyBytes, length), func(t *testing.T) {
				key := bytes.Repeat([]byte{byte(keyBytes)}, keyBytes)
				message := make([]byte, length)
				for i := range message {
					message[i] = byte(i * 17)
				}
				raw := &eaxMemoryConn{writeLimit: 7}
				writer := eaxTestConn(t, raw, key)
				if n, err := writer.Write(message); n != length || err != nil {
					t.Fatalf("Write = %d, %v", n, err)
				}
				wire := bytes.Clone(raw.output.Bytes())
				cipher, _ := newEAXCipher(key)
				var decoded []byte
				records := 0
				for len(wire) > 0 {
					header := wire[:2]
					frameLength := int(binary.BigEndian.Uint16(header))
					if frameLength == 0 || frameLength > 65535 {
						t.Fatalf("invalid emitted frame length %d", frameLength)
					}
					end := 2 + frameLength + 16
					// Expected nonces are independently specified, not obtained
					// through the implementation's counter helper.
					nonce := [16]byte{byte(records)}
					plain, err := cipher.open(nonce[:], wire[2:end], header)
					if err != nil {
						t.Fatal(err)
					}
					decoded = append(decoded, plain...)
					wire = wire[end:]
					records++
				}
				if !bytes.Equal(decoded, message) || records != (length+65534)/65535 {
					t.Fatalf("decoded %d bytes in %d records", len(decoded), records)
				}
				readerRaw := &eaxMemoryConn{reader: bytes.NewReader(raw.output.Bytes()), readLimit: 3}
				reader := eaxTestConn(t, readerRaw, key)
				var received bytes.Buffer
				buffer := make([]byte, 31)
				for {
					n, err := reader.Read(buffer)
					received.Write(buffer[:n])
					if err != nil {
						if !errors.Is(err, io.EOF) {
							t.Fatal(err)
						}
						break
					}
					if n == 0 {
						t.Fatal("nonempty Read returned no progress")
					}
				}
				if !bytes.Equal(received.Bytes(), message) {
					t.Fatalf("received %d bytes, want %d", received.Len(), len(message))
				}
			})
		}
	}
}

func TestEAXEmptyRecordsAndZeroLengthOperations(t *testing.T) {
	key := bytes.Repeat([]byte{2}, 16)
	wire := eaxTestRecord(t, key, nil, [16]byte{})
	wire = append(wire, eaxTestRecord(t, key, []byte("next"), [16]byte{1})...)
	raw := &eaxMemoryConn{reader: bytes.NewReader(wire)}
	conn := eaxTestConn(t, raw, key)
	if n, err := conn.Read(nil); n != 0 || err != nil || raw.reader.Len() != len(wire) {
		t.Fatalf("zero-length Read consumed bytes: %d, %v", n, err)
	}
	if n, err := conn.Write(nil); n != 0 || err != nil || raw.output.Len() != 0 {
		t.Fatalf("zero-length Write emitted bytes: %d, %v", n, err)
	}
	buffer := make([]byte, 10)
	if n, err := conn.Read(buffer); n != 4 || err != nil || string(buffer[:n]) != "next" {
		t.Fatalf("Read after empty record = %q, %v", buffer[:n], err)
	}
	if conn.readNonce[0] != 2 || conn.writeNonce != [16]byte{} {
		t.Fatal("zero-length operations changed the wrong nonce")
	}
}

func TestEAXTamperingClosesBothDirectionsBeforeExposingPlaintext(t *testing.T) {
	key := bytes.Repeat([]byte{3}, 16)
	valid := eaxTestRecord(t, key, []byte("secret"), [16]byte{})
	for _, field := range []string{"ciphertext", "tag", "smaller length", "larger length"} {
		t.Run(field, func(t *testing.T) {
			wire := bytes.Clone(valid)
			switch field {
			case "ciphertext":
				wire[2] ^= 1
			case "tag":
				wire[len(wire)-1] ^= 1
			case "smaller length":
				wire[1]--
			case "larger length":
				wire[1]++
			}
			wire = append(wire, eaxTestRecord(t, key, []byte("later"), [16]byte{1})...)
			raw := &eaxMemoryConn{reader: bytes.NewReader(wire)}
			conn := eaxTestConn(t, raw, key)
			dst := bytes.Repeat([]byte{0x7f}, 30)
			before := bytes.Clone(dst)
			if n, err := conn.Read(dst); n != 0 || !errors.Is(err, ErrProtocol) || !bytes.Equal(dst, before) {
				t.Fatalf("tampered Read exposed %x: n=%d, err=%v", dst, n, err)
			}
			if !raw.closed {
				t.Fatal("unauthenticated stream stayed open")
			}
			if n, err := conn.Read(dst); n != 0 || !errors.Is(err, ErrProtocol) {
				t.Fatalf("continued reading after authentication failure: %d, %v", n, err)
			}
			if n, err := conn.Write([]byte("password")); n != 0 || !errors.Is(err, ErrProtocol) || raw.output.Len() != 0 {
				t.Fatalf("continued writing after authentication failure: %d, %v", n, err)
			}
		})
	}
}

func TestEAXTruncatedRecords(t *testing.T) {
	key := bytes.Repeat([]byte{4}, 16)
	valid := eaxTestRecord(t, key, []byte("contents"), [16]byte{})
	for _, length := range []int{0, 1, 2, 3, 17, len(valid) - 1} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			raw := &eaxMemoryConn{reader: bytes.NewReader(valid[:length])}
			conn := eaxTestConn(t, raw, key)
			if n, err := conn.Read(make([]byte, 50)); n != 0 || (!errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF)) {
				t.Fatalf("truncated Read = %d, %v", n, err)
			}
			if length > 0 && !raw.closed {
				t.Fatal("partially consumed record stayed open")
			}
		})
	}
}

func TestEAXStreamAllowsFragmentedAndCombinedRFBMessages(t *testing.T) {
	key := bytes.Repeat([]byte{5}, 16)
	hash := bytes.Repeat([]byte{0x12}, 20)
	message := append(bytes.Clone(hash), 1)
	message = append(message, []byte("stream")...)
	for _, cuts := range [][]int{{20, 21}, {1, 7, 13, 19, 20, 21, 24}, nil} {
		t.Run(fmt.Sprint(cuts), func(t *testing.T) {
			var wire []byte
			start := 0
			boundaries := append(append([]int(nil), cuts...), len(message))
			for i, end := range boundaries {
				wire = append(wire, eaxTestRecord(t, key, message[start:end], [16]byte{byte(i)})...)
				start = end
			}
			conn := eaxTestConn(t, &eaxMemoryConn{reader: bytes.NewReader(wire), readLimit: 1}, key)
			gotHash, gotSubtype := make([]byte, 20), make([]byte, 1)
			if _, err := io.ReadFull(conn, gotHash); err != nil || !bytes.Equal(gotHash, hash) {
				t.Fatalf("hash = %x, %v", gotHash, err)
			}
			if _, err := io.ReadFull(conn, gotSubtype); err != nil || !bytes.Equal(gotSubtype, []byte{1}) {
				t.Fatalf("subtype = %x, %v", gotSubtype, err)
			}
			if got, err := io.ReadAll(conn); err != nil || string(got) != "stream" {
				t.Fatalf("stream = %q, %v", got, err)
			}
		})
	}
}

func TestEAXNonceOrderingReplayAndExhaustion(t *testing.T) {
	key := bytes.Repeat([]byte{6}, 16)
	t.Run("little-endian carry", func(t *testing.T) {
		nonce := [16]byte{255, 255, 7}
		if !incrementEAXNonce(&nonce) || nonce != [16]byte{0, 0, 8} {
			t.Fatalf("nonce carry = %x", nonce)
		}
	})
	t.Run("replayed record", func(t *testing.T) {
		wire := eaxTestRecord(t, key, []byte("one"), [16]byte{})
		wire = append(wire, wire...)
		conn := eaxTestConn(t, &eaxMemoryConn{reader: bytes.NewReader(wire)}, key)
		got := make([]byte, 3)
		if _, err := io.ReadFull(conn, got); err != nil || string(got) != "one" {
			t.Fatalf("first message = %q, %v", got, err)
		}
		if n, err := conn.Read(got); n != 0 || !errors.Is(err, ErrProtocol) {
			t.Fatalf("replayed message accepted = %d, %v", n, err)
		}
	})
	var lastNonce [16]byte
	for i := range lastNonce {
		lastNonce[i] = 255
	}
	t.Run("receive exhaustion", func(t *testing.T) {
		wire := eaxTestRecord(t, key, []byte("last"), lastNonce)
		conn := eaxTestConn(t, &eaxMemoryConn{reader: bytes.NewReader(wire)}, key)
		conn.readNonce = lastNonce
		got := make([]byte, 4)
		if _, err := io.ReadFull(conn, got); err != nil || string(got) != "last" {
			t.Fatalf("last nonce message = %q, %v", got, err)
		}
		if n, err := conn.Read(got); n != 0 || !errors.Is(err, ErrProtocol) {
			t.Fatalf("receive nonce wrapped = %d, %v", n, err)
		}
	})
	t.Run("send exhaustion", func(t *testing.T) {
		raw := &eaxMemoryConn{}
		conn := eaxTestConn(t, raw, key)
		conn.writeNonce = lastNonce
		if n, err := conn.Write([]byte("last")); n != 4 || err != nil {
			t.Fatalf("last nonce Write = %d, %v", n, err)
		}
		if got, want := raw.output.Bytes(), eaxTestRecord(t, key, []byte("last"), lastNonce); !bytes.Equal(got, want) {
			t.Fatal("last valid send nonce was not used")
		}
		before := raw.output.Len()
		if n, err := conn.Write([]byte("again")); n != 0 || !errors.Is(err, ErrProtocol) || raw.output.Len() != before {
			t.Fatalf("send nonce wrapped = %d, %v", n, err)
		}
	})
}

func TestEAXPartialWritesReportAcceptedPlaintext(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 16)
	message := bytes.Repeat([]byte{0x42}, 65535+10)
	const firstRecordWireLength = 2 + 65535 + 16
	for _, tc := range []struct {
		name      string
		failureAt int
		accepted  int
	}{
		{"before first record", 0, 0},
		{"partial first record", 18, 0},
		{"complete first record with error", firstRecordWireLength, 65535},
		{"partial second record", firstRecordWireLength + 3, 65535},
		{"complete second record with error", firstRecordWireLength + 2 + 10 + 16, len(message)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := &eaxMemoryConn{writeLimit: 7, writeFailureAt: tc.failureAt, writeFailure: io.ErrClosedPipe}
			conn := eaxTestConn(t, raw, key)
			if n, err := conn.Write(message); n != tc.accepted || !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("Write = %d, %v; want accepted %d", n, err, tc.accepted)
			}
			if !raw.closed {
				t.Fatal("partially written encrypted record stayed open")
			}
			before := raw.output.Len()
			if n, err := conn.Write([]byte("next")); n != 0 || err == nil || raw.output.Len() != before {
				t.Fatalf("continued after failed record: %d, %v", n, err)
			}
		})
	}
}

type eaxWriterFunc func([]byte) (int, error)

func (writer eaxWriterFunc) Write(data []byte) (int, error) { return writer(data) }

func TestEAXInvalidShortWriters(t *testing.T) {
	for _, n := range []int{0, -1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			complete, err := writeEAXRecord(eaxWriterFunc(func([]byte) (int, error) { return n, nil }), []byte{1})
			if complete || !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("invalid writer count accepted = %t, %v", complete, err)
			}
		})
	}
}

type eaxChunkConn struct {
	net.Conn
	limit int
}

func (conn eaxChunkConn) Read(dst []byte) (int, error) {
	return conn.Conn.Read(dst[:min(len(dst), conn.limit)])
}
func (conn eaxChunkConn) Write(src []byte) (int, error) {
	return conn.Conn.Write(src[:min(len(src), conn.limit)])
}

func TestEAXFullDuplexIndependentDirections(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	_ = a.SetDeadline(time.Now().Add(5 * time.Second))
	_ = b.SetDeadline(time.Now().Add(5 * time.Second))
	keyA, keyB := bytes.Repeat([]byte{8}, 16), bytes.Repeat([]byte{9}, 32)
	left, err := newEAXConn(eaxChunkConn{a, 7}, keyB, keyA)
	if err != nil {
		t.Fatal(err)
	}
	right, err := newEAXConn(eaxChunkConn{b, 11}, keyA, keyB)
	if err != nil {
		t.Fatal(err)
	}
	leftText, rightText := bytes.Repeat([]byte("left-"), 700), bytes.Repeat([]byte("right-"), 600)
	errors := make(chan error, 4)
	go func() { _, err := left.Write(leftText); errors <- err }()
	go func() { _, err := right.Write(rightText); errors <- err }()
	go func() {
		got := make([]byte, len(rightText))
		_, err := io.ReadFull(left, got)
		if err == nil && !bytes.Equal(got, rightText) {
			err = fmt.Errorf("wrong left received plaintext")
		}
		errors <- err
	}()
	go func() {
		got := make([]byte, len(leftText))
		_, err := io.ReadFull(right, got)
		if err == nil && !bytes.Equal(got, leftText) {
			err = fmt.Errorf("wrong right received plaintext")
		}
		errors <- err
	}()
	for range 4 {
		if err := <-errors; err != nil {
			t.Error(err)
		}
	}
}

func TestEAXConcurrentWritesStayWholeRecords(t *testing.T) {
	key := bytes.Repeat([]byte{10}, 16)
	raw := &eaxMemoryConn{writeLimit: 3}
	writer := eaxTestConn(t, raw, key)
	const count = 30
	var wg sync.WaitGroup
	errors := make(chan error, count)
	for i := range count {
		wg.Go(func() {
			message := bytes.Repeat([]byte{byte(i)}, 40)
			_, err := writer.Write(message)
			errors <- err
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	reader := eaxTestConn(t, &eaxMemoryConn{reader: bytes.NewReader(raw.output.Bytes()), readLimit: 2}, key)
	seen := map[byte]bool{}
	for range count {
		got := make([]byte, 40)
		if _, err := io.ReadFull(reader, got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 40 || !bytes.Equal(got, bytes.Repeat(got[:1], 40)) || seen[got[0]] {
			t.Fatalf("interleaved or duplicated concurrent record: %x", got)
		}
		seen[got[0]] = true
	}
	if len(seen) != count {
		t.Fatal("concurrent writer lost records")
	}
}

func TestEAXDeadlineTimeoutBeforeRecordCanBeReset(t *testing.T) {
	key := bytes.Repeat([]byte{11}, 16)
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	reader, writer := eaxTestConn(t, a, key), eaxTestConn(t, b, key)
	_ = reader.SetReadDeadline(time.Now().Add(-time.Second))
	if n, err := reader.Read(make([]byte, 10)); n != 0 || err == nil {
		t.Fatalf("expired deadline = %d, %v", n, err)
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("expired deadline not exposed as net.Error: %v", err)
	}
	if reader.failed() != nil || reader.readNonce != [16]byte{} {
		t.Fatal("untouched timed-out record corrupted the connection")
	}
	_ = reader.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = writer.SetWriteDeadline(time.Now().Add(5 * time.Second))
	done := make(chan error, 1)
	go func() { _, err := writer.Write([]byte("retry")); done <- err }()
	buffer := make([]byte, 10)
	if n, err := reader.Read(buffer); n != 5 || err != nil || string(buffer[:n]) != "retry" {
		t.Fatalf("Read after deadline reset = %q, %v", buffer[:n], err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestEAXConnDelegatesMetadataDeadlinesAndClose(t *testing.T) {
	key := bytes.Repeat([]byte{12}, 16)
	raw := &eaxMemoryConn{}
	conn := eaxTestConn(t, raw, key)
	if conn.LocalAddr().String() != raw.LocalAddr().String() || conn.RemoteAddr().String() != raw.RemoteAddr().String() {
		t.Fatal("connection addresses changed")
	}
	deadline := time.Now().Add(time.Second)
	_ = conn.SetDeadline(deadline)
	_ = conn.SetReadDeadline(deadline.Add(time.Second))
	_ = conn.SetWriteDeadline(deadline.Add(2 * time.Second))
	for i, got := range raw.deadlines {
		if want := deadline.Add(time.Duration(i) * time.Second); !got.Equal(want) {
			t.Fatalf("deadline %d = %v, want %v", i, got, want)
		}
	}
	if err := conn.Close(); err != nil || !raw.closed {
		t.Fatalf("Close not forwarded: %v", err)
	}
}

func TestEAXRejectsInvalidConfiguration(t *testing.T) {
	for _, length := range []int{0, 1, 15, 17, 24, 31, 33} {
		t.Run(fmt.Sprintf("key%d", length), func(t *testing.T) {
			if c, err := newEAXCipher(make([]byte, length)); c != nil || err == nil {
				t.Fatalf("invalid key accepted: %v, %v", c, err)
			}
			if conn, err := newEAXConn(&eaxMemoryConn{}, bytes.Repeat([]byte{1}, 16), make([]byte, length)); conn != nil || err == nil {
				t.Fatalf("invalid write key accepted: %v, %v", conn, err)
			}
		})
	}
	if conn, err := newEAXConn(nil, make([]byte, 16), make([]byte, 16)); conn != nil || err == nil {
		t.Fatalf("nil connection accepted: %v, %v", conn, err)
	}
}
