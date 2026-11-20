package workbench

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"

	"motty/internal/autofill"

	"motty/internal/aiproviderimport"
)

// What a state paints, as text: the rectangles, shadows, texts and hits of one paint, so a
// change to how a screen is declared shows as a diff of what it draws. The control numbering
// the page assigns is normalized away: removing or adding an element shifts it without
// changing anything on screen. So is the draw order's counter, which keeps only its rank:
// what is painted over what stays, and the count of primitives before it does not.
//
//	go test ./internal/workbench -run TestPaintGolden            # compare
//	UPDATE_PAINT_GOLDEN=1 go test ./internal/workbench -run TestPaintGolden   # rewrite
var hitNumbers = regexp.MustCompile(`^hit \{[0-9]+ [0-9]+\} `)

var drawOrder = regexp.MustCompile(` ([0-9]+)\}$`)

// paintDump leaves the draw order of the rectangles and shadows as ranks.
func paintDump(canvas *gfx.Canvas) string {
	var b strings.Builder
	var shapes []string
	var orders []int
	for _, r := range canvas.Rectangles() {
		shapes = append(shapes, fmt.Sprintf("rect %v", r))
	}
	for _, s := range canvas.Shadows() {
		shapes = append(shapes, fmt.Sprintf("shadow %v", s))
	}
	for _, line := range shapes {
		order, _ := strconv.Atoi(drawOrder.FindStringSubmatch(line)[1])
		orders = append(orders, order)
	}
	ranks := slices.Clone(orders)
	slices.Sort(ranks)
	ranks = slices.Compact(ranks)
	for i, line := range shapes {
		rank, _ := slices.BinarySearch(ranks, orders[i])
		b.WriteString(drawOrder.ReplaceAllString(line, fmt.Sprintf(" #%d}", rank)) + "\n")
	}
	for _, t := range canvas.Texts() {
		fmt.Fprintf(&b, "text %q %v\n", t.Value, t.Bounds)
	}
	for _, h := range canvas.Hits() {
		line := fmt.Sprintf("hit %v %q %v %v disabled=%v\n", h.ID, h.Label, h.Role, h.Bounds, h.Disabled)
		b.WriteString(hitNumbers.ReplaceAllString(line, "hit "))
	}
	return b.String()
}

type paintScene struct {
	name  string
	sizes []gfx.Size
	open  func(t *testing.T) (state *State, paint func(*gfx.Canvas, gfx.Size))
}

var (
	paintDesktop = gfx.Size{Width: 1400, Height: 900}
	paintSmall   = gfx.Size{Width: 900, Height: 600}
)

func plainScene(name string, setup func(t *testing.T, state *State)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		_, options := newWorld(t)
		state := newTestState(t, options)
		setup(t, state)
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}

func settingsScene(name string, setup func(t *testing.T, world *settingsWorld)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		world := newSettingsWorld(t, newLister("m1"))
		pinPaintState(world.state)
		setup(t, world)
		return world.state, func(c *gfx.Canvas, size gfx.Size) { world.state.Paint(c, size) }
	}}
}

// extraPaintScenes are scenes a file of its own adds with init, so a change to one screen
// does not touch the table above.
var extraPaintScenes []paintScene

func paintScenes() []paintScene {
	return append(basePaintScenes(), extraPaintScenes...)
}

func basePaintScenes() []paintScene {
	candidates := []aiproviderimport.Candidate{
		{Name: "One", Kind: "openai", BaseURL: "https://a.example/v1", Models: []string{"m1", "m2"}},
		{Name: "Two", Kind: "openai", BaseURL: "https://b.example/v1", SkipReason: "duplicate"},
	}
	importing := func(set func(*aiSettingsUI)) func(*testing.T, *settingsWorld) {
		return func(t *testing.T, w *settingsWorld) {
			set(&w.state.ai.cfg)
			w.state.ai.cfg.dialog = aiDialogImport
		}
	}
	scenes := []paintScene{
		plainScene("vault-unlock", func(t *testing.T, s *State) { s.modal = modalVaultUnlock }),
		plainScene("vault-unlock-error", func(t *testing.T, s *State) {
			s.secrets.dialogError = "Wrong password."
			s.modal = modalVaultUnlock
		}),
		plainScene("workspace-save", func(t *testing.T, s *State) {
			id := launchTemporaryWorkspace(t, s, 1, 0)
			rightClickTab(s, hitTabList.At(int(id)))
			chooseMenu(t, s, hitTabMenuSaveWorkspace)
		}),
		{name: "xy-transfer", sizes: []gfx.Size{paintDesktop}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			state, session, _ := transferState(t)
			state.openXYSetup(session)
			return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
		}},
		plainScene("highlight-rule", func(t *testing.T, s *State) { s.openHighlightDialog() }),
		plainScene("highlight-rule-error", func(t *testing.T, s *State) {
			s.openHighlightDialog()
			s.highlighting.dialogError = "That pattern is not valid."
		}),
		plainScene("autofill-rule", func(t *testing.T, s *State) { s.openAutofillDialog(nil) }),
		plainScene("autofill-rule-text-error", func(t *testing.T, s *State) {
			s.openAutofillDialog(nil)
			s.autofillRules.send = autofill.SendText
			s.autofillRules.dialogError = "Name the rule."
		}),
		plainScene("sudo-sftp", func(t *testing.T, s *State) { s.sudoSFTP = sudoSFTPDialog{}; s.modal = modalSudoSFTP }),
		plainScene("moba-encoding", func(t *testing.T, s *State) {
			s.mobaEncoding = mobaEncodingPrompt{utf8: "abc", gb: "def"}
			s.modal = modalMobaEncoding
		}),
		plainScene("import-chooser", func(t *testing.T, s *State) { s.modal = modalImportSessions }),
		plainScene("development-import", func(t *testing.T, s *State) { s.showDevelopmentImport(3, false, nil) }),
		plainScene("development-import-error", func(t *testing.T, s *State) {
			s.showDevelopmentImport(3, false, fmt.Errorf("vault is locked"))
		}),
		plainScene("session-import-preview", func(t *testing.T, s *State) {
			s.sessionImport = sessionImportPreview{eligible: 2, passwords: make([]sessionImportPassword, 2)}
			s.modal = modalSessionImportPreview
		}),
		plainScene("credential", func(t *testing.T, s *State) { s.openCredentialDialog("") }),
		plainScene("credential-error", func(t *testing.T, s *State) {
			s.openCredentialDialog("")
			s.credentialForm.dialogError = "Something went wrong with this credential."
		}),
		plainScene("workspace-draft", func(t *testing.T, s *State) {
			s.activeSection = sectionWorkspaces
			s.Click(hitWorkspaceNew)
		}),
		settingsScene("ai-provider-form", func(t *testing.T, w *settingsWorld) { w.state.Click(hitAISetProviderAdd) }),
		settingsScene("ai-provider-form-error", func(t *testing.T, w *settingsWorld) {
			w.state.Click(hitAISetProviderAdd)
			w.state.ai.cfg.formErr = "The key is not valid."
		}),
		settingsScene("ai-probe", func(t *testing.T, w *settingsWorld) { w.state.ai.cfg.dialog = aiDialogProbe }),
		settingsScene("ai-import-sources-empty", importing(func(c *aiSettingsUI) { c.importStep = aiImportSources })),
		settingsScene("ai-import-sources", importing(func(c *aiSettingsUI) {
			c.importStep = aiImportSources
			c.importSources = []aiproviderimport.SourceInfo{{Path: "/x", Found: true, Candidates: candidates}, {Path: "/y"}}
		})),
		settingsScene("ai-import-sources-error", importing(func(c *aiSettingsUI) {
			c.importStep = aiImportSources
			c.importErr = "Could not read it."
		})),
		settingsScene("ai-import-preview-empty", importing(func(c *aiSettingsUI) { c.importStep = aiImportPreview })),
		settingsScene("ai-import-preview", importing(func(c *aiSettingsUI) {
			c.importStep = aiImportPreview
			c.importCandidates = candidates
			c.importSelected = map[int]bool{0: true}
		})),
		settingsScene("ai-import-done", importing(func(c *aiSettingsUI) {
			c.importStep = aiImportDone
			c.importResult = aiImportResult{Created: 2}
		})),
		settingsScene("ai-import-done-detail", importing(func(c *aiSettingsUI) {
			c.importStep = aiImportDone
			c.importResult = aiImportResult{Created: 1, Failed: 1, KeyFailed: 1, GatewayCreated: true, Err: "boom"}
		})),
		settingsScene("ai-confirm-delete", func(t *testing.T, w *settingsWorld) {
			w.addOpenAI(t, "A", "", "k1")
			w.addOpenAI(t, "B", "", "k2")
			w.state.reloadAI()
			w.state.Click(hitAISetProviderDelete.At(0))
		}),
	}
	for mode := 0; mode < 3; mode++ {
		scenes = append(scenes, settingsScene(fmt.Sprintf("ai-reasoning-mode-%d", mode), func(t *testing.T, w *settingsWorld) {
			cfg := &w.state.ai.cfg
			cfg.reasoningModelName, cfg.reasoningMode = "deepseek-v4-pro", mode
			cfg.dialog = aiDialogModelReasoning
		}))
	}
	return scenes
}

func TestPaintGolden(t *testing.T) {
	pinPaintEnvironment(t)
	update := os.Getenv("UPDATE_PAINT_GOLDEN") != ""
	for _, scene := range paintScenes() {
		for _, size := range scene.sizes {
			name := fmt.Sprintf("%s-%vx%v", scene.name, size.Width, size.Height)
			t.Run(name, func(t *testing.T) {
				_, paint := scene.open(t)
				var canvas gfx.Canvas
				paint(&canvas, size)
				got := paintDump(&canvas)
				path := filepath.Join("testdata", "paint", name+".txt")
				if update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("no golden for %s: run with UPDATE_PAINT_GOLDEN=1", name)
				}
				if diff := firstDifference(string(want), got); diff != "" {
					t.Errorf("%s paints differently from its golden (UPDATE_PAINT_GOLDEN=1 rewrites it)\n%s", name, diff)
				}
			})
		}
	}
}

func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a == b || samePaintLine(a, b) {
			continue
		}
		return fmt.Sprintf("line %d\n  want %s\n  got  %s", i+1, a, b)
	}
	return ""
}

// paintNumberTolerance is how far apart two printed numbers may be and still
// count as the same paint. The dumps print float32 values in full precision,
// and another architecture can compute the last digit differently: a color
// mixed through a different instruction lands 1e-8 away. A pixel of layout and
// one 8-bit color step (1/255, about 0.004) are both larger than this, so a
// change a person could see still fails.
const paintNumberTolerance = 0.002

// samePaintLine compares one dump line token by token: numbers within
// paintNumberTolerance, quoted text and everything else exactly.
func samePaintLine(a, b string) bool {
	at, bt := paintTokens(a), paintTokens(b)
	if len(at) != len(bt) {
		return false
	}
	for i := range at {
		if at[i] == bt[i] {
			continue
		}
		x, ok := parsePaintNumber(at[i])
		if !ok {
			return false
		}
		y, ok := parsePaintNumber(bt[i])
		if !ok || math.Abs(x-y) > paintNumberTolerance {
			return false
		}
	}
	return true
}

func parsePaintNumber(token string) (float64, bool) {
	value, err := strconv.ParseFloat(token, 64)
	return value, err == nil
}

// paintTokens splits a dump line into quoted strings, runs of digits and dots,
// and single characters. A quoted string stays whole, so a number in a label or
// a path is never read as a coordinate.
func paintTokens(line string) []string {
	tokens := make([]string, 0, 16)
	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == '"':
			start := i
			i++
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
			tokens = append(tokens, line[start:min(i, len(line))])
		case c >= '0' && c <= '9' || c == '.':
			start := i
			for i < len(line) && (line[i] >= '0' && line[i] <= '9' || line[i] == '.') {
				i++
			}
			tokens = append(tokens, line[start:i])
		default:
			tokens = append(tokens, string(c))
			i++
		}
	}
	return tokens
}
