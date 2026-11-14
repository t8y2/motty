// Package mosh is a client for the mosh State Synchronization Protocol (SSP).
//
// Mosh carries an interactive terminal over UDP: a server process started over
// SSH keeps the terminal's screen and the client sends keystrokes and receives
// screen updates. The session survives address changes and packet loss, and the
// screen state is idempotent. This package implements the client half only.
//
// It does not start the server, resolve the host, or decide how the network is
// reached: the caller bootstraps mosh-server over SSH, parses the
// "MOSH CONNECT <port> <key>" line and supplies a datagram socket. That keeps
// the application's proxy, interface and address-family policy in the
// application, as internal/rdpcore does for TCP.
//
// Security: datagrams are authenticated with AES-128-OCB3 under the one-time
// key the SSH bootstrap carried, and replay is rejected by the datagram
// sequence. A server is not trusted beyond that key: every length is checked
// before memory is allocated, reassembly and decompression are bounded, and
// unknown protobuf fields are skipped.
//
// The protocol layer is a trimmed, modified copy of unixshells/mosh-go; see
// NOTICE.md.
package mosh
