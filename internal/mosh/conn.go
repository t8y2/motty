package mosh

// Datagram framing constants shared by the transport and the client. Mosh
// wraps every encrypted datagram as:
//
//	[dirSeq : 8 bytes big-endian][AES-128-OCB3 ciphertext][tag : 16 bytes]
//
// The top bit of dirSeq is the direction (0 client→server, 1 server→client);
// the low 63 bits are the datagram sequence used as the OCB nonce and as the
// replay check.
const (
	dirToServer = uint64(0)
	dirToClient = uint64(1) << 63
	seqMask     = ^(uint64(1) << 63)

	// minDatagram is the smallest authenticated datagram: nonce header plus
	// OCB tag.
	minDatagram = 8 + 16
)
