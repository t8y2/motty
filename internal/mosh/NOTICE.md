# Origin of this directory

`internal/mosh` is a trimmed and modified copy of the client half of
<https://github.com/unixshells/mosh-go> at tag `v0.5.2`
(commit `0284bd652aab3dfff5b0b74945c23901d7537b61`), MIT licensed; its licence
text is `LICENSE` here.

It is a copy and not a `go.mod` dependency because Motty needs a different
scheduler (no resident ticker), the shutdown handshake, caller-supplied
datagrams and the server's echo acknowledgement for local prediction, and
because the upstream module's other dependencies (`creack/pty`, `vt-go`,
charmbracelet) are used only by its server, framebuffer and WASM demo, which
this client does not contain.

This is a fork; it does not track upstream. Changes, all marked where they are
in code:

Removed (not used by a client): `server.go`, `framebuffer.go`, `predict.go`,
`cmd/`, the WASM build, the demo assets and the upstream README.

Rewritten:

* `transport.go`: mosh's own timing constants (8 ms send delay, 20–250 ms frame
  interval, 50–1000 ms RTO, 100 ms delayed ack, 3 s empty ack), a deadline
  driven `Tick`/`NextWake` instead of mosh-go's 8 ms resident ticker, the
  `new_num = UINT64_MAX` shutdown handshake in both directions, and ack-only
  instructions that name the newest sent state as their reference so they
  cannot create a state whose diff was lost.
* `client.go`: no goroutines but one socket loop, `InputID`/`AckedInput` for
  prediction liveness, `Drain`/`Done`/`Err`, a caller-supplied unconnected
  `net.PacketConn`, and a read error that ends the session instead of leaving
  a silently dead one.
* `conn.go`: the datagram framing constants, split out of the upstream client.

Kept from upstream: `ocb.go`, `pb.go` and the fragment framing in
`fragment.go`, with one change there — the maximum fragment payload is mosh's
500-byte send MTU rather than upstream's 1300, which also stays below the IPv6
minimum MTU. `ocb_test.go`, `fragment_test.go` and `pb_test.go` are upstream
tests, including RFC 7253 vectors for OCB3; `transport_test.go`,
`client_test.go`, `interop_test.go` and `fuzz_test.go` are Motty's.
