package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"motty/internal/ai/store"
)

func writePiLauncherFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func piPackageFixture(t *testing.T, prefix, name, entry string) string {
	t.Helper()
	dir := filepath.Join(prefix, "node_modules", filepath.FromSlash(name))
	raw, err := json.Marshal(map[string]any{"name": name, "bin": map[string]string{"pi": entry}})
	if err != nil {
		t.Fatal(err)
	}
	writePiLauncherFixture(t, filepath.Join(dir, "package.json"), string(raw))
	path := filepath.Join(dir, filepath.FromSlash(entry))
	writePiLauncherFixture(t, path, "// fixture")
	return path
}

func piNodeFixture(t *testing.T, dir string) string {
	t.Helper()
	// Running a copy of this test executable lets capture check the actual
	// argv and quoting without requiring an installed Node or Pi account.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "node.exe")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPiLauncherNodeHelper(t *testing.T) {
	if os.Getenv("MOTTY_TEST_PI_LAUNCHER") != "yes" {
		return
	}
	raw, _ := json.Marshal(os.Args)
	_, _ = os.Stdout.Write(raw)
	os.Exit(0)
}

func TestPiLaunchersUseSelectedInstallAndManifestEntry(t *testing.T) {
	const old = "@mariozechner/pi-coding-agent"
	const current = "@earendil-works/pi-coding-agent"
	for _, tc := range []struct {
		name, pkg, entry, ext, script string
	}{
		{"old npm", old, "dist/cli.js", ".cmd", `@echo off
"%_prog%" "%dp0%\node_modules\@mariozechner\pi-coding-agent\dist\cli.js" %*`},
		{"early new npm", current, "dist/cli.js", ".cmd", `"%_prog%" "%dp0%\node_modules\@earendil-works\pi-coding-agent\dist\cli.js" %*`},
		{"new npm", current, "dist/bundle/cli.js", ".cmd", `"%_prog%" "%dp0%\node_modules\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*`},
		{"PowerShell npm", old, "dist/cli.js", ".ps1", `& "$basedir/node$exe" "$basedir/node_modules/@mariozechner/pi-coding-agent/dist/cli.js" $args`},
		{"batch launcher", current, "dist/bundle/cli.js", ".bat", `node "%~dp0node_modules\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := filepath.Join(t.TempDir(), "Pi install with spaces")
			node := piNodeFixture(t, prefix)
			entry := piPackageFixture(t, prefix, tc.pkg, tc.entry)
			other := current
			if tc.pkg == current {
				other = old
			}
			piPackageFixture(t, prefix, other, "dist/other.js")
			launcher := filepath.Join(prefix, "pi"+tc.ext)
			writePiLauncherFixture(t, launcher, tc.script)
			// A co-located official launcher must not override the npm shim.
			writePiLauncherFixture(t, filepath.Join(prefix, "pi-launcher.js"), "// other install")
			c, err := resolve(store.KindPiCLI, launcher)
			if err != nil || !strings.EqualFold(c.program, node) || len(c.args) != 1 || !strings.EqualFold(c.args[0], entry) {
				t.Fatalf("resolved = %+v, error = %v", c, err)
			}
			if got, err := ExecutablePath(store.KindPiCLI, launcher); err != nil || got != launcher {
				t.Fatalf("saved launcher = %q, error = %v", got, err)
			}
			c.args = append([]string{"-test.run=^TestPiLauncherNodeHelper$", "--"}, c.args...)
			c.args = append(c.args, "argument with spaces", "literal&argument")
			c.env = []string{"MOTTY_TEST_PI_LAUNCHER=yes"}
			raw, err := capture(t.Context(), c, 4096)
			var args []string
			if err != nil || json.Unmarshal(raw, &args) != nil || len(args) < 4 || !strings.EqualFold(args[3], entry) || !reflect.DeepEqual(args[4:], []string{"argument with spaces", "literal&argument"}) {
				t.Fatalf("argv = %s, error = %v", raw, err)
			}
			// An upgrade may move bin.pi while the selected shim still has
			// its previous reference. Resolve from this same package again.
			upgraded := piPackageFixture(t, prefix, tc.pkg, "new entry/main.js")
			if err := os.Remove(entry); err != nil {
				t.Fatal(err)
			}
			c, err = resolve(store.KindPiCLI, launcher)
			if err != nil || len(c.args) != 1 || !strings.EqualFold(c.args[0], upgraded) {
				t.Fatalf("upgraded = %+v, error = %v", c, err)
			}
		})
	}
}

func TestPiOfficialLaunchersKeepManagedEntryAndNode(t *testing.T) {
	for _, tc := range []struct{ ext, script string }{
		{".cmd", `SET "node=node"
IF EXIST "%LOCALAPPDATA%\pi-node\current\node.exe" SET "node=%LOCALAPPDATA%\pi-node\current\node.exe"
"%node%" "%~dp0pi-launcher.js" %*`},
		{".ps1", `$basedir=Split-Path $MyInvocation.MyCommand.Definition -Parent
$launcher=Join-Path $basedir "pi-launcher.js"
$standaloneNode=Join-Path $env:LOCALAPPDATA "pi-node/current/node.exe"
& $node $launcher $args`},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "official Pi with spaces", "bin")
			piNodeFixture(t, bin)
			t.Setenv("LOCALAPPDATA", filepath.Join(root, "local app data"))
			node := piNodeFixture(t, filepath.Join(os.Getenv("LOCALAPPDATA"), "pi-node", "current"))
			entry := filepath.Join(bin, "pi-launcher.js")
			writePiLauncherFixture(t, entry, "// managed launcher")
			launcher := filepath.Join(bin, "pi"+tc.ext)
			writePiLauncherFixture(t, launcher, tc.script)
			c, err := resolve(store.KindPiCLI, launcher)
			if err != nil || !strings.EqualFold(c.program, node) || len(c.args) != 1 || !strings.EqualFold(c.args[0], entry) {
				t.Fatalf("resolved = %+v, error = %v", c, err)
			}
			c, err = resolve(store.KindPiCLI, entry)
			if err != nil || !strings.EqualFold(c.program, node) {
				t.Fatalf("direct managed JS Node = %+v, error = %v", c, err)
			}
			fallbackDir := filepath.Join(root, "path node")
			fallback := piNodeFixture(t, fallbackDir)
			t.Setenv("PATH", fallbackDir)
			if err := os.Remove(node); err != nil {
				t.Fatal(err)
			}
			c, err = resolve(store.KindPiCLI, launcher)
			if err != nil || !strings.EqualFold(c.program, fallback) {
				t.Fatalf("standalone removed = %+v, error = %v", c, err)
			}
			writePiLauncherFixture(t, launcher, strings.ReplaceAll(tc.script, "pi-node", "unused-node"))
			c, err = resolve(store.KindPiCLI, launcher)
			if err != nil || !strings.EqualFold(c.program, fallback) {
				t.Fatalf("default official Node = %+v, error = %v", c, err)
			}
		})
	}
}

func TestPiPATHDiscoveryAndLinksStayWithSelectedLauncher(t *testing.T) {
	root := t.TempDir()
	selected := filepath.Join(root, "selected")
	node := piNodeFixture(t, selected)
	entry := piPackageFixture(t, selected, "@mariozechner/pi-coding-agent", "dist/cli.js")
	launcher := filepath.Join(selected, "pi.cmd")
	writePiLauncherFixture(t, launcher, `node "%dp0%\node_modules\@mariozechner\pi-coding-agent\dist\cli.js" %*`)
	other := filepath.Join(root, "other")
	piNodeFixture(t, other)
	otherEntry := piPackageFixture(t, other, "@earendil-works/pi-coding-agent", "dist/bundle/cli.js")
	writePiLauncherFixture(t, filepath.Join(other, "pi.cmd"), `node "%dp0%\node_modules\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*`)
	t.Setenv("PATH", selected+string(os.PathListSeparator)+other)
	c, err := resolve(store.KindPiCLI, "")
	if err != nil || !strings.EqualFold(c.program, node) || len(c.args) != 1 || !strings.EqualFold(c.args[0], entry) {
		t.Fatalf("PATH discovery = %+v, error = %v", c, err)
	}
	c, err = resolve(store.KindPiCLI, filepath.Join(other, "pi.cmd"))
	if err != nil || len(c.args) != 1 || !strings.EqualFold(c.args[0], otherEntry) {
		t.Fatalf("explicit override = %+v, error = %v", c, err)
	}
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(other, "linked.cmd")
		if err := os.Symlink(launcher, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		c, err := resolve(store.KindPiCLI, link)
		if err != nil || !strings.EqualFold(c.program, node) || len(c.args) != 1 || !strings.EqualFold(c.args[0], entry) {
			t.Fatalf("linked launcher = %+v, error = %v", c, err)
		}
	})
}

func TestPiLocalNPMShimAndDirectEntry(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "node_modules", ".bin")
	node := piNodeFixture(t, bin)
	t.Setenv("PATH", bin)
	entry := piPackageFixture(t, root, "@earendil-works/pi-coding-agent", "dist/bundle/cli.js")
	launcher := filepath.Join(bin, "pi.cmd")
	writePiLauncherFixture(t, launcher, `node "%dp0%\..\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*`)
	for _, path := range []string{launcher, entry} {
		c, err := resolve(store.KindPiCLI, path)
		if err != nil || !strings.EqualFold(c.program, node) || len(c.args) != 1 || !strings.EqualFold(c.args[0], entry) {
			t.Fatalf("%q = %+v, error = %v", path, c, err)
		}
	}
	// npm also permits the package's single bin to be a string.
	writePiLauncherFixture(t, filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent", "package.json"), `{"name":"@earendil-works/pi-coding-agent","bin":"dist/bundle/cli.js"}`)
	if _, err := resolve(store.KindPiCLI, launcher); err != nil {
		t.Fatal(err)
	}
}

func TestPiUnsupportedLauncherDoesNotSelectAnotherPackage(t *testing.T) {
	prefix := t.TempDir()
	piNodeFixture(t, prefix)
	piPackageFixture(t, prefix, "@earendil-works/pi-coding-agent", "dist/bundle/cli.js")
	launcher := filepath.Join(prefix, "pi.cmd")
	writePiLauncherFixture(t, launcher, `node "%dp0%\node_modules\@mariozechner\pi-coding-agent\dist\cli.js" %*`)
	if _, err := resolve(store.KindPiCLI, launcher); err == nil || err.Error() != "cli: unsupported_launcher" {
		t.Fatal("unrelated install substituted", err)
	}
	for _, bin := range []string{"", "../outside.js", `C:\outside.js`, "missing.js"} {
		t.Run(bin, func(t *testing.T) {
			dir := filepath.Join(prefix, "node_modules", "@mariozechner", "pi-coding-agent")
			raw, _ := json.Marshal(map[string]any{"name": "@mariozechner/pi-coding-agent", "bin": map[string]string{"pi": bin}})
			writePiLauncherFixture(t, filepath.Join(dir, "package.json"), string(raw))
			if _, err := resolve(store.KindPiCLI, launcher); err == nil {
				t.Fatal("invalid bin accepted", bin)
			}
		})
	}
}

func TestPiOfficialLauncherFollowsVersionChange(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable for managed launcher execution")
	}
	root := filepath.Join(t.TempDir(), "managed Pi with spaces")
	bin := filepath.Join(root, "bin")
	launcher := filepath.Join(bin, "pi.cmd")
	writePiLauncherFixture(t, launcher, `node "%~dp0pi-launcher.js" %*`)
	// A managed launcher chooses its release at execution time. Motty must
	// keep invoking it after current-version changes, rather than pin a release.
	writePiLauncherFixture(t, filepath.Join(bin, "pi-launcher.js"), `const fs = require("node:fs");
const path = require("node:path");
const root = path.join(__dirname, "..", "install");
const version = fs.readFileSync(path.join(root, "current-version"), "utf8").trim();
require(path.join(root, "releases", version, "cli.js"));`)
	for _, version := range []string{"before", "after"} {
		writePiLauncherFixture(t, filepath.Join(root, "install", "current-version"), version)
		writePiLauncherFixture(t, filepath.Join(root, "install", "releases", version, "cli.js"), `console.log(JSON.stringify({version: "`+version+`", args: process.argv.slice(2)}));`)
		c, err := resolve(store.KindPiCLI, launcher)
		if err != nil {
			t.Fatal(err)
		}
		if c.program != node {
			t.Fatalf("wrong Node: %q", c.program)
		}
		c.args = append(c.args, "argument with spaces")
		raw, err := capture(t.Context(), c, 4096)
		if err != nil || !strings.Contains(string(raw), `"version":"`+version+`"`) || !strings.Contains(string(raw), `"args":["argument with spaces"]`) {
			t.Fatalf("managed execution = %s, error = %v", raw, err)
		}
	}
}

// Opt-in checks the real selected installation using only --version/--help,
// with isolated Pi settings and no login, gateway request or inference.
func TestInstalledPiLauncherModels(t *testing.T) {
	path := os.Getenv("MOTTY_TEST_PI_LAUNCHER_PATH")
	if path == "" {
		t.Skip("opt-in installed Pi launcher test")
	}
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	t.Setenv("PI_SKIP_VERSION_CHECK", "1")
	t.Setenv("PI_TELEMETRY", "0")
	t.Setenv("PI_OFFLINE", "1")
	for _, selected := range []string{"", path, strings.TrimSuffix(path, filepath.Ext(path)) + ".ps1"} {
		models, err := New(store.KindPiCLI, selected).Models(t.Context())
		if err != nil || !reflect.DeepEqual(models, []string{"default"}) {
			t.Fatalf("selected %q: models = %v, error = %v", selected, models, err)
		}
	}
}

func TestPiLauncherFilesystemVariants(t *testing.T) {
	for _, variant := range []string{"nested manifest", "symlink bin", "junction", "official case"} {
		t.Run(variant, func(t *testing.T) {
			prefix := filepath.Join(t.TempDir(), "Pi with spaces")
			piNodeFixture(t, prefix)
			t.Setenv("PATH", prefix)
			entry := piPackageFixture(t, prefix, "@earendil-works/pi-coding-agent", "dist/bundle/cli.js")
			launcher := filepath.Join(prefix, "pi.cmd")
			writePiLauncherFixture(t, launcher, `node "%dp0%\node_modules\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*`)
			switch variant {
			case "nested manifest":
				writePiLauncherFixture(t, filepath.Join(prefix, "node_modules", "@earendil-works", "pi-coding-agent", "dist", "package.json"), `{"type":"module"}`)
			case "symlink bin":
				target := filepath.Join(filepath.Dir(entry), "target.js")
				if err := os.Rename(entry, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, entry); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				launcher = entry
			case "junction":
				link := filepath.Join(filepath.Dir(prefix), "linked Pi")
				out, err := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/c", "mklink", "/J", link, prefix).CombinedOutput()
				if err != nil {
					t.Skipf("junction unavailable: %v: %s", err, out)
				}
				defer os.Remove(link)
				launcher = filepath.Join(link, "pi.cmd")
			case "official case":
				entry = filepath.Join(prefix, "pi-launcher.js")
				writePiLauncherFixture(t, entry, "// official")
				writePiLauncherFixture(t, launcher, `node "%~dp0PI-LAUNCHER.JS" %*`)
			}
			c, err := resolve(store.KindPiCLI, launcher)
			if err != nil || len(c.args) != 1 {
				t.Fatalf("resolved = %+v, error = %v", c, err)
			}
			got, err := os.Stat(c.args[0])
			want, wantErr := os.Stat(entry)
			if err != nil || wantErr != nil || !os.SameFile(got, want) {
				t.Fatalf("selected another entry: %v, %v", err, wantErr)
			}
		})
	}
}

func TestPiLauncherIgnoresCommentedInstallations(t *testing.T) {
	for _, tc := range []struct{ ext, comment, active string }{
		{".cmd", `REM "%dp0%\node_modules\@mariozechner\pi-coding-agent\dist\cli.js"`, `node "%dp0%\node_modules\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*`},
		{".cmd", `:: "%dp0%\node_modules\@mariozechner\pi-coding-agent\dist\cli.js"`, `node "%dp0%\node_modules\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*`},
		{".ps1", `# "$basedir/node_modules/@mariozechner/pi-coding-agent/dist/cli.js"`, `& node "$basedir/node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js" $args`},
		{".ps1", "<#\n" + `"$basedir/node_modules/@mariozechner/pi-coding-agent/dist/cli.js"` + "\n#>", `& node "$basedir/node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js" $args`},
	} {
		t.Run(tc.comment, func(t *testing.T) {
			prefix := t.TempDir()
			piNodeFixture(t, prefix)
			piPackageFixture(t, prefix, "@mariozechner/pi-coding-agent", "dist/cli.js")
			entry := piPackageFixture(t, prefix, "@earendil-works/pi-coding-agent", "dist/bundle/cli.js")
			launcher := filepath.Join(prefix, "pi"+tc.ext)
			writePiLauncherFixture(t, launcher, tc.comment+"\n"+tc.active)
			c, err := resolve(store.KindPiCLI, launcher)
			if err != nil || len(c.args) != 1 || !strings.EqualFold(c.args[0], entry) {
				t.Fatalf("resolved = %+v, error = %v", c, err)
			}
			if err := os.Remove(entry); err != nil {
				t.Fatal(err)
			}
			if _, err := resolve(store.KindPiCLI, launcher); err == nil {
				t.Fatal("commented installation substituted for missing active entry")
			}
		})
	}
}

func TestPiOfficialDiscoveryWithoutUpdatedPATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", filepath.Join(home, "empty path"))
	launcher := filepath.Join(home, ".pi", "agent", "bin", "pi.cmd")
	writePiLauncherFixture(t, launcher, `node "%~dp0pi-launcher.js" %*`)
	path, err := ExecutablePath(store.KindPiCLI, "")
	if err != nil || !strings.EqualFold(path, launcher) {
		t.Fatalf("official fallback = %q, error = %v", path, err)
	}
	if _, err := ExecutablePath(store.KindPiCLI, filepath.Join(home, "missing", "pi.cmd")); err == nil {
		t.Fatal("explicit missing launcher replaced by default installation")
	}
}

func TestPiLauncherRejectsDifferentInstallBranchesEvenWhenOneIsMissing(t *testing.T) {
	prefix := t.TempDir()
	piNodeFixture(t, prefix)
	old := piPackageFixture(t, prefix, "@mariozechner/pi-coding-agent", "dist/cli.js")
	piPackageFixture(t, prefix, "@earendil-works/pi-coding-agent", "dist/bundle/cli.js")
	launcher := filepath.Join(prefix, "pi.cmd")
	writePiLauncherFixture(t, launcher, `if "%USE_OLD_PI%"=="1" (
node "%dp0%\node_modules\@mariozechner\pi-coding-agent\dist\cli.js" %*
) else (
node "%dp0%\node_modules\@earendil-works\pi-coding-agent\dist\bundle\cli.js" %*
)`)
	if _, err := resolve(store.KindPiCLI, launcher); err == nil {
		t.Fatal("ambiguous installation branches accepted")
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(store.KindPiCLI, launcher); err == nil {
		t.Fatal("missing installation branch silently replaced by the other install")
	}
}
