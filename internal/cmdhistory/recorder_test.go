package cmdhistory

import (
	"strings"
	"testing"
	"time"

	"motty/internal/term"
)

// shell plays a terminal and the program behind it: output goes through the
// real emulator, keys go to the recorder, and the test decides what the program
// would echo back.
type shell struct {
	t      *testing.T
	screen *term.Terminal
	rec    Recorder
	now    time.Duration
}

func newShell(t *testing.T, cols int) *shell {
	return &shell{t: t, screen: term.New(cols, 10, 100), now: time.Second}
}

func (s *shell) out(text string) {
	s.screen.Write([]byte(text))
	s.rec.Output(s.now)
}

// prompt prints a prompt and lets the output settle, as a shell waiting does.
func (s *shell) prompt(text string) {
	s.out("\r\n" + text)
	s.now += 300 * time.Millisecond
}

func (s *shell) send(keys string) (string, bool) {
	return s.rec.Typed([]byte(keys), TermScreen{s.screen}, s.now)
}

// typeEchoed types text one key at a time and the terminal echoes each.
func (s *shell) typeEchoed(text string) {
	for _, r := range text {
		if command, ok := s.send(string(r)); ok {
			s.t.Fatalf("a key ended a line: %q", command)
		}
		s.out(string(r))
		s.now += 10 * time.Millisecond
	}
}

// typeHidden types text the program does not echo.
func (s *shell) typeHidden(text string) {
	for _, r := range text {
		s.send(string(r))
		s.now += 10 * time.Millisecond
	}
}

func (s *shell) enter() (string, bool) {
	command, ok := s.send("\r")
	s.out("\r\n")
	return command, ok
}

func (s *shell) wantRecorded(want string) {
	s.t.Helper()
	command, ok := s.enter()
	if !ok || command != want {
		s.t.Fatalf("recorded %q (%v), want %q", command, ok, want)
	}
}

func (s *shell) wantNothing() {
	s.t.Helper()
	if command, ok := s.enter(); ok {
		s.t.Fatalf("recorded %q, want nothing", command)
	}
}

func TestACommandAtAPromptIsRecorded(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("user@host:~$ ")
	s.typeEchoed("ls -la /var/log")
	s.wantRecorded("ls -la /var/log")
}

func TestAPasswordTypedWithoutEchoIsNeverRecorded(t *testing.T) {
	s := newShell(t, 80)
	// A prompt whose wording gives nothing away: only the missing echo does.
	s.prompt("Enter: ")
	s.typeHidden("hunter2")
	s.wantNothing()
	// The same with no wording and no echo, as `read -s x` does.
	s.prompt("")
	s.typeHidden("hunter2")
	s.wantNothing()
}

func TestAPromptThatAsksForASecretIsNeverRecordedEvenIfEchoed(t *testing.T) {
	for _, prompt := range []string{"[sudo] password for user: ", "Enter passphrase for key '/home/u/.ssh/id': ", "Verification code: ", "请输入密码: ", "請輸入密碼: ", "驗證碼: ", "OTP> "} {
		s := newShell(t, 80)
		s.prompt(prompt)
		s.typeEchoed("123456")
		s.wantNothing()
	}
	// The wording can be on the line above the prompt.
	s := newShell(t, 80)
	s.out("\r\nEnter the token for the deploy server")
	s.prompt("> ")
	s.typeEchoed("abc123")
	s.wantNothing()
}

func TestALineStartingWithASpaceIsNotRecorded(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("$ ")
	s.typeEchoed(" ls")
	s.wantNothing()
}

func TestCommandsThatCarryASecretAreNotRecorded(t *testing.T) {
	for _, command := range []string{
		"mysql -u root --password=hunter2", "mysql --password hunter2", "export DB_PASSWORD=hunter2",
		"curl -H 'Authorization: Basic abc' https://x", "curl -H 'Authorization: Bearer abc' https://x",
		"curl https://user:hunter2@example.com/x", "echo -----BEGIN RSA PRIVATE KEY-----", "AWS_SECRET_ACCESS_KEY=abc aws s3 ls",
		"git push https://ghp_abc@github.com/x?token=abc", "API_KEY=abc ./run", "sshpass -p hunter2 ssh host",
	} {
		s := newShell(t, 200)
		s.prompt("$ ")
		s.typeEchoed(command)
		if got, ok := s.enter(); ok {
			t.Errorf("%q was recorded as %q", command, got)
		}
	}
	for _, command := range []string{"ls -p", "grep -rn todo src", "docker run -p 80:80 nginx", "ssh user@host"} {
		s := newShell(t, 200)
		s.prompt("$ ")
		s.typeEchoed(command)
		s.wantRecorded(command)
	}
}

func TestRecalledAndCompletedLinesAreRecordedAsTheyEndedUp(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("$ ")
	s.send("\x1b[A") // Up
	s.out("echo recalled")
	s.wantRecorded("echo recalled")

	s.prompt("$ ")
	s.typeEchoed("ls /et")
	s.send("\t")
	s.out("c/")
	s.wantRecorded("ls /etc/")

	// Recalled, then edited: Up, delete a key, type another.
	s.prompt("$ ")
	s.send("\x1b[A")
	s.out("make test")
	s.send("\x7f")
	s.out("\b \b")
	s.typeEchoed("ts")
	s.wantRecorded("make tests")

	// An arrow key at a prompt that does not echo leaves nothing to record.
	s.prompt("Enter: ")
	s.send("\x1b[A")
	s.typeHidden("hunter2")
	s.wantNothing()
}

func TestNothingIsRecordedOnTheAlternateScreen(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("$ ")
	s.out("\x1b[?1049h")
	s.now += time.Second
	s.typeEchoed("ihello")
	s.wantNothing()
}

func TestALongCommandIsJoinedAcrossWrappedRows(t *testing.T) {
	s := newShell(t, 20)
	s.prompt("$ ")
	// Wraps onto three rows, with a space falling at a row's end.
	command := "echo aaaaaaaaaa bbbbbbbbbbbbbbbbbbbb cc"
	s.typeEchoed(command)
	s.wantRecorded(command)

	// A prompt that itself wraps.
	s = newShell(t, 20)
	s.prompt("user@a-long-host:~/projects/x$ ")
	s.typeEchoed("ls")
	s.wantRecorded("ls")

	// At the bottom of the screen the line scrolls as it wraps.
	s = newShell(t, 20)
	s.out(strings.Repeat("\r\nfiller", 12))
	s.prompt("$ ")
	s.typeEchoed(command)
	s.wantRecorded(command)
}

func TestConsecutiveDuplicatesAreRecordedOnce(t *testing.T) {
	s := newShell(t, 80)
	for i := 0; i < 3; i++ {
		s.prompt("$ ")
		s.typeEchoed("ls")
		if i == 0 {
			s.wantRecorded("ls")
		} else {
			s.wantNothing()
		}
	}
	s.prompt("$ ")
	s.typeEchoed("pwd")
	s.wantRecorded("pwd")
	s.prompt("$ ")
	s.typeEchoed("ls")
	s.wantRecorded("ls")
}

func TestPastedLinesAndTypedAheadKeysAreNotRecorded(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("$ ")
	if _, ok := s.send("ls\rpwd\r"); ok {
		t.Fatal("two lines at once were recorded")
	}
	s.prompt("$ ")
	s.out("ls\r\npwd")
	s.send("\x1b[200~line one\nline two\x1b[201~")
	s.wantNothing()

	// Keys pressed while output is still arriving belong to no prompt yet.
	s.out("\r\nrunning...")
	s.now += 5 * time.Millisecond
	s.typeEchoed("whoami")
	s.wantNothing()
}

func TestInputThatIsNotAtAPromptIsNotRecorded(t *testing.T) {
	// `cat > file` reads lines from column zero.
	s := newShell(t, 80)
	s.prompt("")
	s.typeEchoed("PASSWORD_FILE_BODY")
	s.wantNothing()
	// Ctrl+C abandons the line; the next one is a fresh start.
	s.prompt("$ ")
	s.typeEchoed("sleep 100")
	s.send("\x03")
	s.out("^C")
	s.prompt("$ ")
	s.typeEchoed("ls")
	s.wantRecorded("ls")
}

func TestARightHandPromptAndGreySuggestionsAreNotPartOfTheCommand(t *testing.T) {
	// zsh: a right prompt at the end of the row.
	s := newShell(t, 40)
	s.prompt("$ \x1b7\x1b[30G12:00:00\x1b8")
	s.typeEchoed("ls")
	s.wantRecorded("ls")

	s = newShell(t, 40)
	s.prompt("$ \x1b7\x1b[30G12:00:00\x1b8")
	s.send("\x1b[A")
	s.out("\x1b7\x1b[2K\r$ git status\x1b[30G12:00:00\x1b8\x1b[11C") // the shell redraws the line
	s.wantRecorded("git status")

	// fish: the rest of a suggestion in grey after what was typed.
	s = newShell(t, 40)
	s.prompt("$ ")
	s.typeEchoed("git ch")
	s.out("\x1b7\x1b[90meckout\x1b[0m\x1b8")
	s.wantRecorded("git ch")

	s = newShell(t, 40)
	s.prompt("$ ")
	s.send("\x1b[A")
	s.out("ls\x1b7\x1b[90m -la\x1b[0m\x1b8")
	s.wantRecorded("ls")
}

func TestAPromptOfSeveralLinesIsRead(t *testing.T) {
	s := newShell(t, 60)
	s.out("\r\n~/projects/motty on main")
	s.prompt("\r\n❯ ")
	s.typeEchoed("go test ./...")
	s.wantRecorded("go test ./...")
}

func TestAnOverlongCommandIsNotRecorded(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("$ ")
	long := "echo " + strings.Repeat("x", MaxCommand)
	for i := 0; i < len(long); i += 70 {
		s.typeEchoed(long[i:min(i+70, len(long))])
	}
	if command, ok := s.enter(); ok {
		t.Fatalf("a %d byte command was recorded: %.20q", len(command), command)
	}
}

func (s *shell) line() (string, bool) { return s.rec.Line(TermScreen{s.screen}) }

func TestTheLineBeingTypedIsAvailableToSuggestFor(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("user@host:~$ ")
	if line, ok := s.line(); ok {
		t.Fatalf("a line nobody has typed in yet is %q", line)
	}
	s.typeEchoed("git ch")
	if line, ok := s.line(); !ok || line != "git ch" {
		t.Fatalf("line = %q, %v", line, ok)
	}
	// A line completed by the shell is read off the screen, not from the keys.
	s.send("\t")
	s.out("eckout")
	if line, ok := s.line(); !ok || line != "git checkout" {
		t.Fatalf("after completion line = %q, %v", line, ok)
	}
}

func TestNoLineIsOfferedWhereAnythingSecretOrNotTheShellsOwnCouldBe(t *testing.T) {
	s := newShell(t, 80)
	s.prompt("Enter: ")
	s.typeHidden("hunter2")
	if line, ok := s.line(); ok {
		t.Fatalf("a password nobody saw echoed is %q", line)
	}
	s = newShell(t, 80)
	s.prompt("[sudo] password for user: ")
	s.typeEchoed("abc")
	if line, ok := s.line(); ok {
		t.Fatalf("an answer to a password prompt is %q", line)
	}
	s = newShell(t, 80)
	s.prompt("$ ")
	s.typeEchoed(" ls")
	if line, ok := s.line(); ok {
		t.Fatalf("a line kept out of history on purpose is %q", line)
	}
	// The shell's own suggestion sits to the right of the cursor.
	s = newShell(t, 80)
	s.prompt("$ ")
	s.typeEchoed("git ")
	s.out("\x1b[90mstatus\x1b[0m\x1b[6D")
	if line, ok := s.line(); ok {
		t.Fatalf("with the shell suggesting, line = %q", line)
	}
	// A full-screen program owns the screen.
	s = newShell(t, 80)
	s.prompt("$ ")
	s.typeEchoed("vi")
	s.out("\x1b[?1049h")
	if line, ok := s.line(); ok {
		t.Fatalf("on the alternate screen line = %q", line)
	}
}

func TestARightHandPromptDoesNotHideTheLine(t *testing.T) {
	s := newShell(t, 40)
	s.out("\r\n$ " + strings.Repeat(" ", 28) + "12:30\x1b[3G")
	s.now += 300 * time.Millisecond
	s.typeEchoed("ls")
	if line, ok := s.line(); !ok || line != "ls" {
		t.Fatalf("with a right-hand prompt line = %q, %v", line, ok)
	}
}

func TestSuggestContinuesWithTheMostRecentCommandThatStartsTheSame(t *testing.T) {
	entries := []Entry{{Command: "git status"}, {Command: "git checkout main"}, {Command: "git checkout dev"}}
	if rest, ok := Suggest(entries, "git c"); !ok || rest != "heckout main" {
		t.Fatalf("rest = %q, %v", rest, ok)
	}
	if _, ok := Suggest(entries, "git status"); ok {
		t.Fatal("a command that is already complete has nothing to add")
	}
	if _, ok := Suggest(entries, "ls"); ok {
		t.Fatal("no command starts that way")
	}
	if _, ok := Suggest([]Entry{{Command: "mysql --password hunter2"}}, "mysql --password"); ok {
		t.Fatal("a line that reads like a secret was suggested for")
	}
}

func TestSuggestAllReturnsUpToMaxMatchesNewestFirst(t *testing.T) {
	entries := []Entry{{Command: "git status"}, {Command: "git checkout main"}, {Command: "git checkout dev"}, {Command: "git config"}}
	matches := SuggestAll(entries, "git c", 2)
	if len(matches) != 2 || matches[0] != "heckout main" || matches[1] != "heckout dev" {
		t.Fatalf("matches = %q", matches)
	}
	if got := SuggestAll(entries, "git c", 10); len(got) != 3 {
		t.Fatalf("without the cap every match returns, got %q", got)
	}
	if got := SuggestAll(entries, "git c", 0); got != nil {
		t.Fatalf("a zero cap returns nothing, got %q", got)
	}
	if got := SuggestAll(entries, "", 5); got != nil {
		t.Fatalf("an empty line returns nothing, got %q", got)
	}
	if got := SuggestAll([]Entry{{Command: "mysql --password hunter2"}}, "mysql --password", 5); got != nil {
		t.Fatal("a line that reads like a secret was suggested for")
	}
	if got := SuggestAll(entries, "git status", 5); got != nil {
		t.Fatal("a command that is already complete has nothing to add")
	}
}
