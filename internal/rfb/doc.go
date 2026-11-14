// Package rfb is a client for the Remote Framebuffer protocol (RFC 6143), the
// protocol of VNC. It has no dependency on a user interface: pixels go to a
// Sink, input comes in through the Send methods, and package
// internal/desktop/vnc connects the two to the workbench.
//
// What it speaks: protocol versions 3.3, 3.7 and 3.8; the security types None
// and VNC Authentication, plus RSA-AES with verified server keys and AES-128/256-EAX
// (types 5/129 for the session, types 6/130 for authentication only with explicit
// permission), and VeNCrypt 0.2 X.509 TLS authentication. RealVNC's known 5.0
// banner negotiates to 3.8. A 32-bit true-colour pixel format is requested from the
// server, so no pixel is ever converted; supported encodings are Raw, CopyRect, RRE,
// Hextile and ZRLE; the DesktopSize pseudo-encoding; pointer, key and
// clipboard (Latin-1 text) messages. What it does not: anonymous TLS, bare Plain,
// unspecified vendor security types, Apple Remote Desktop authentication, the cursor shape
// pseudo-encoding (the server then draws the cursor into the framebuffer),
// extended clipboard, Tight and the other encodings, audio.
//
// A server is not trusted. Every length it sends is checked against a limit
// before memory is allocated for it, every rectangle against the framebuffer,
// and no decoder reads further than the structure it is decoding says.
package rfb
