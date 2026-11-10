package workbench

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/desktop"
	"motty/internal/i18n"
	"motty/internal/sshconn"
)

type securityDesktop struct {
	desktop.Session
	info desktop.Info
}

func (s *securityDesktop) Info() desktop.Info { return s.info }

func TestVNCSecurityStatusSeparatesProtocolAndTunnelProtection(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		state := &State{messages: i18n.For(language)}
		m := state.messages.Desktop
		for _, test := range []struct {
			mode              string
			encrypted, tunnel bool
			protection        string
		}{
			{"None", false, false, m.StripPlain},
			{"VNC Authentication", false, true, m.StripPlain},
			{"RA2", true, false, m.StripVNCEncrypted},
			{"RA256ne", false, true, m.SecurityAuthOnly},
			{"X509None", true, false, m.StripVNCEncrypted},
		} {
			view := &desktopView{session: &securityDesktop{info: desktop.Info{Security: test.mode, ProtocolEncrypted: test.encrypted, Encrypted: test.encrypted || test.tunnel}}}
			session := &Session{host: Host{Protocol: protocolVNC, ViaSSH: test.tunnel}}
			line := state.desktopStripLeft(view, session)
			if !strings.Contains(line, test.mode) || !strings.Contains(line, test.protection) {
				t.Fatalf("mode %s: status %q does not describe its protocol protection", test.mode, line)
			}
			if strings.Contains(line, m.StripEncrypted) != test.tunnel {
				t.Fatalf("mode %s: status %q misreports SSH protection", test.mode, line)
			}
		}
	}
}

func TestVNCSecurityMetadataFormattingIsCachedAndChangesLanguage(t *testing.T) {
	state := &State{messages: i18n.For(i18n.English)}
	remote := &securityDesktop{info: desktop.Info{Security: "RA2ne"}}
	view := &desktopView{session: remote}
	state.vncSecurityStatus(view)
	if allocs := testing.AllocsPerRun(50, func() { state.vncSecurityStatus(view) }); allocs != 0 {
		t.Fatalf("stable security metadata formatting allocated %v times", allocs)
	}
	state.messages = i18n.For(i18n.TraditionalChinese)
	if line := state.vncSecurityStatus(view); !strings.Contains(line, state.messages.Desktop.SecurityAuthOnly) {
		t.Fatalf("language change retained old security status: %q", line)
	}
	remote.info.Security, remote.info.ProtocolEncrypted = "RA2", true
	if line := state.vncSecurityStatus(view); !strings.Contains(line, state.messages.Desktop.StripVNCEncrypted) || strings.Contains(line, state.messages.Desktop.SecurityAuthOnly) {
		t.Fatalf("new negotiation retained old protection: %q", line)
	}
}

func TestVNCSecurityDetailsUseActualMetadataAndEnteredEndpoint(t *testing.T) {
	state := &State{messages: i18n.For(i18n.English)}
	info := desktop.Info{
		Security: "X509None", Authentication: "none", ProtocolEncrypted: true,
		Cipher: "TLS_AES_256_GCM_SHA384", TLSVersion: "TLS 1.3",
		SecurityTypes: "RA2, VeNCrypt, #250", SecuritySubtypes: "X509None, X509Plain",
	}
	session := &Session{
		host:    Host{Protocol: protocolVNC, Address: "2001:db8::1", Port: 5901, ViaSSH: true},
		hostKey: sshconn.HostKeyInfo{Type: vncCertificateType, Fingerprint: "SHA256:" + strings.Repeat("a", 64)},
		desk:    &desktopView{session: &securityDesktop{info: info}},
	}
	details := state.vncSecurityDetails(session)
	for _, value := range []string{"[2001:db8::1]:5901", info.Security, info.Cipher, info.TLSVersion, info.SecurityTypes, info.SecuritySubtypes, session.hostKey.Fingerprint, state.messages.Desktop.SecurityAuthNone, state.messages.Desktop.StripEncrypted} {
		if !strings.Contains(details, value) {
			t.Fatalf("security details omit %q: %q", value, details)
		}
	}
	// Seeing VeNCrypt among advertised top-level modes does not discover its
	// subtypes when the connection chose RSA-AES instead.
	info.Security, info.SecuritySubtypes = "RA2", ""
	session.desk.session = &securityDesktop{info: info}
	details = state.vncSecurityDetails(session)
	if strings.Contains(details, "X509Plain") || !strings.Contains(details, state.messages.Desktop.SecuritySubtypes+": "+state.messages.Desktop.SecurityNotReported) {
		t.Fatalf("RSA-AES details invented VeNCrypt subtypes: %q", details)
	}
	// An outer SSH identity must never be shown as the VNC server identity.
	session.hostKey.Type, session.hostKey.Fingerprint = "ssh-ed25519", "SHA256:ssh"
	if strings.Contains(state.vncSecurityDetails(session), "SHA256:ssh") {
		t.Fatal("VNC details showed the tunnel's fingerprint")
	}
	session.hostKey = sshconn.HostKeyInfo{}
	session.conn = &desktopConn{verifiedKey: sshconn.HostKeyInfo{Type: vncServerKeyType, Fingerprint: "SHA256:verified"}}
	if !strings.Contains(state.vncSecurityDetails(session), "SHA256:verified") {
		t.Fatal("details lost the verified fingerprint after handshake status was cleared")
	}
}

func TestVNCSecurityDetailsAreReachableWithHiddenBarsAndCopyFullValues(t *testing.T) {
	state, session, remote, _, _ := followState(t, 1, false, gfx.Size{Width: 1400, Height: 900})
	session.host.Protocol = protocolVNC
	session.desk.session = &securityDesktop{Session: remote, info: desktop.Info{Security: "RA2", Authentication: "user-password", ProtocolEncrypted: true, SecurityTypes: "RA2, #250"}}
	session.hostKey = sshconn.HostKeyInfo{Type: vncServerKeyType, Fingerprint: "SHA256:" + strings.Repeat("abcdef", 32)}
	canvas := paintDesk(state)
	for _, id := range []gfx.ID{hitDesktopSecurity, hitDesktopSecurityStatus} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("visible bars omit security action %v", id)
		}
	}
	session.desk.chromeless = true
	rightClickTab(state, hitTabList.At(int(state.deck.ActiveWorkspace())))
	canvas = paintDesk(state)
	if hit, ok := hitByID(canvas, hitTabMenuSecurity); !ok || hit.Disabled {
		t.Fatal("hidden bars make security details unreachable in the tab menu")
	}
	state.Click(hitTabMenuSecurity)
	if state.modal != modalDesktopSecurity || !session.desk.released {
		t.Fatal("security details did not release captured remote input")
	}
	canvas = paintDesk(state)
	hit, ok := hitByID(canvas, hitDesktopSecurityText)
	if !ok || hit.Role != gfx.RoleGroup || hit.Value != state.desktopSecurity.text || !strings.Contains(hit.Value, session.hostKey.Fingerprint) {
		t.Fatal("security details became an input field or truncated the fingerprint")
	}
	if state.EditorFor(hitDesktopSecurityText) != nil {
		t.Fatal("security details allow editing connection metadata")
	}
	state.options.SetClipboardText = func(string) error { return errors.New("clipboard refused") }
	state.Click(hitDesktopSecurityCopy)
	if state.desktopSecurity.err == "" || state.modal != modalDesktopSecurity {
		t.Fatal("copy failure was hidden or dismissed the details")
	}
	copied := ""
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.Click(hitDesktopSecurityCopy)
	if copied != hit.Value || state.desktopSecurity.err != "" {
		t.Fatal("copy omitted full detail values or retained the old failure")
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if state.modal != modalNone || !session.desk.released {
		t.Fatal("closing security details recaptured the remote keyboard")
	}
	state.openCommandPalette()
	found := false
	for _, entry := range state.commandPalette.entries {
		found = found || entry.target == hitDesktopSecurity
	}
	if !found {
		t.Fatal("security details are absent from the command palette")
	}
}

func TestVNCPasswordDialogNamesCredentialPurposeAndEndpoint(t *testing.T) {
	for _, account := range []bool{false, true} {
		state := newTestState(t, testOptions(t))
		session := &Session{host: Host{Protocol: protocolVNC}, attempt: 1, endpoint: sshconn.Endpoint{Host: "2001:db8::1", Port: 5900, User: vncAccount}}
		if account {
			session.endpoint.User = vncAccountPrefix
			session.questions = []sshconn.Question{{}, {Secret: true}}
		}
		state.preparePassword(session)
		if !strings.Contains(state.passwordDescription, "[2001:db8::1]:5900") || strings.Contains(state.passwordDescription, "vnc@") {
			t.Fatalf("login description misidentifies endpoint: %q", state.passwordDescription)
		}
		if account {
			if state.passwordQuestions[0].Text != state.messages.Desktop.VNCSystemUser || state.passwordQuestions[1].Text != state.messages.Desktop.VNCSystemPassword {
				t.Fatal("account login does not identify system credentials")
			}
		} else if state.passwordQuestions[0].Text != state.messages.Desktop.VNCPassword {
			t.Fatal("classic VNC login does not identify its password")
		}
	}
	state := newTestState(t, testOptions(t))
	session := &Session{host: Host{Protocol: protocolVNC}, attempt: 1, endpoint: sshconn.Endpoint{Host: "vnc.example.com", Port: 5900, User: vncAccountPrefix + "alice"}, questions: []sshconn.Question{{Secret: true}}}
	state.preparePassword(session)
	if state.passwordQuestions[0].Text != state.messages.Desktop.VNCSystemPassword || !strings.Contains(state.passwordDescription, "alice@vnc.example.com:5900") {
		t.Fatal("known account login does not identify the account's password")
	}
}

func TestVNCPasswordDialogKeepsTunnelAccountNamesSeparate(t *testing.T) {
	for _, user := range []string{vncAccount, vncAccountPrefix + "alice"} {
		state := newTestState(t, testOptions(t))
		conn := &desktopConn{}
		session := &Session{
			host: Host{Protocol: protocolVNC, ViaSSH: true}, conn: conn, attempt: 1,
			endpoint:  sshconn.Endpoint{Host: "ssh.example.com", Port: 22, User: user},
			questions: []sshconn.Question{{Text: "SSH password", Secret: true}},
		}
		state.preparePassword(session)
		if state.passwordQuestions[0].Text != session.questions[0].Text || isVNCPasswordPrompt(session) {
			t.Fatalf("SSH tunnel account %q was treated as a VNC password", user)
		}
		conn.asking = true
		state.preparePassword(session)
		if !isVNCPasswordPrompt(session) || state.passwordQuestions[0].Text == session.questions[0].Text {
			t.Fatalf("VNC password prompt for %q retained the SSH label", user)
		}
	}
}

func TestVNCSecurityToolbarDoesNotAllocateForAdditionalControls(t *testing.T) {
	state, session, remote, _, _ := followState(t, 1, false, gfx.Size{Width: 1400, Height: 900})
	session.host.Protocol = protocolVNC
	session.desk.session = &securityDesktop{Session: remote, info: desktop.Info{Security: "RA2", ProtocolEncrypted: true}}
	var canvas gfx.Canvas
	var baseline float64
	for _, width := range []float32{639, 640, 800} {
		size := gfx.Size{Width: width, Height: 480}
		for range 3 {
			canvas.Reset()
			state.paintDesktopPage(&canvas, size, session)
		}
		allocations := testing.AllocsPerRun(50, func() {
			canvas.Reset()
			state.paintDesktopPage(&canvas, size, session)
		})
		if width == 639 {
			baseline = allocations
		} else if allocations > baseline {
			t.Fatalf("toolbar at %.0fpx allocated %v times, without additional controls %v", width, allocations, baseline)
		}
		previous := float32(-1)
		for _, id := range []gfx.ID{hitDesktopCtrlAltDel, hitDesktopFit, hitDesktopActual, hitDesktopFullscreen, hitDesktopSecurity} {
			hit, ok := hitByID(&canvas, id)
			if !ok {
				continue
			}
			if hit.Bounds.X <= previous {
				t.Fatal("toolbar changed the established action order")
			}
			previous = hit.Bounds.X
		}
	}
}

func TestVNCSecuritySmallWindowDetailsScrollAndRemainAccessible(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		for _, presentation := range []string{"bars", "chromeless", "fullscreen"} {
			t.Run(fmt.Sprintf("%v/%s", language, presentation), func(t *testing.T) {
				state, session, remote, _, _ := followState(t, 1, false, gfx.Size{Width: 640, Height: 480})
				state.messages = i18n.For(language)
				session.host.Protocol = protocolVNC
				session.desk.session = &securityDesktop{Session: remote, info: desktop.Info{
					Security: "X509None", Authentication: "none", ProtocolEncrypted: true,
					Cipher: "TLS_AES_256_GCM_SHA384", TLSVersion: "TLS 1.3",
					SecurityTypes: strings.Repeat("RA2, RA256, VeNCrypt, #250, ", 20),
				}}
				session.hostKey = sshconn.HostKeyInfo{Type: vncCertificateType, Fingerprint: "SHA256:" + strings.Repeat("abcdef", 32)}
				driver := desktopDriver(t, state)
				if err := driver.Resize(640, 480); err != nil {
					t.Fatal(err)
				}
				if presentation == "fullscreen" {
					state.toggleDesktopFullscreen(session)
				} else {
					session.desk.chromeless = presentation == "chromeless"
				}
				if err := repaintDesktopDriver(driver, state); err != nil {
					t.Fatal(err)
				}
				if presentation == "bars" {
					hit, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopSecurityStatus)
					if !ok {
						t.Fatal("small window has no security status action")
					}
					if err := driver.Click(hit.Bounds.X+2, hit.Bounds.Y+2); err != nil {
						t.Fatal(err)
					}
				} else {
					state.Key(gfx.KeyEvent{Target: hitTerminalList.At(int(session.ID)), Text: "g", Modifiers: gfx.ModifierControl | gfx.ModifierAlt})
					if !session.desk.released {
						t.Fatal("keyboard escape did not release the hidden desktop")
					}
					result := state.Shortcut(gfx.KeyEvent{Text: "p", Modifiers: keymap.Primary | gfx.ModifierShift})
					if !result.Handled || state.modal != modalCommandPalette {
						t.Fatal("hidden desktop cannot reach the command palette")
					}
					index := -1
					for visible, candidate := range state.commandPalette.visible {
						if state.commandPalette.entries[candidate].target == hitDesktopSecurity {
							index = visible
						}
					}
					if index < 0 {
						t.Fatal("hidden desktop command palette omits security details")
					}
					state.commandPalette.selected = index
					state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitCommandPaletteSearch})
				}
				if state.modal != modalDesktopSecurity {
					t.Fatal("security action did not open its dialog")
				}
				if err := repaintDesktopDriver(driver, state); err != nil {
					t.Fatal(err)
				}
				hit, ok := hitByIDInSnapshot(driver.Snapshot(), hitDesktopSecurityText)
				if !ok || hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > 640 || hit.Bounds.Y+hit.Bounds.Height > 480 {
					t.Fatalf("small window cannot read the details: %+v", hit.Bounds)
				}
				closeFound := false
				for _, action := range driver.Snapshot().Hits {
					if action.ID == hitDesktopSecurityClose && action.Label == state.messages.ConnClose {
						closeFound = true
					}
				}
				if !closeFound {
					t.Fatal("details close action has the wrong purpose")
				}
				point := gfx.Point{X: hit.TextBounds.X, Y: hit.TextBounds.Y + 1}
				before, valid := state.desktopSecurity.layout.OffsetAt(point)
				if !valid {
					t.Fatal("details have no text placement for their wrapped rows")
				}
				state.Key(gfx.KeyEvent{Key: gfx.KeyEnd, Target: hitDesktopSecurityText})
				if err := repaintDesktopDriver(driver, state); err != nil {
					t.Fatal(err)
				}
				after, _ := state.desktopSecurity.layout.OffsetAt(point)
				if after <= before {
					t.Fatal("End cannot reveal long security values")
				}
				state.Scroll(gfx.ScrollEvent{Target: hitDesktopSecurityText, DeltaY: -1e6, Precise: true})
				if err := repaintDesktopDriver(driver, state); err != nil {
					t.Fatal(err)
				}
				start, _ := state.desktopSecurity.layout.OffsetAt(point)
				if start != before {
					t.Fatal("wheel cannot return to the first detail row")
				}
				copied := ""
				state.options.SetClipboardText = func(value string) error { copied = value; return nil }
				state.Click(hitDesktopSecurityCopy)
				if copied != hit.Value || !strings.Contains(copied, session.hostKey.Fingerprint) {
					t.Fatal("small-window copy truncated a long security value")
				}
			})
		}
	}
}
