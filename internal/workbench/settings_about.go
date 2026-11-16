package workbench

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/assets"
	"motty/internal/i18n"
	"motty/internal/update"
)

// Where the project and its community are. A moved group or site is changed in
// one place, so the page and its help text cannot drift apart.
const (
	aboutQQURL      = "https://qm.qq.com/cgi-bin/qm/qr?k=&group_code=1131573299"
	aboutWeChatURL  = "https://docs.qq.com/doc/DVXBGYnFFdm9UWkVP"
	aboutDiscordURL = "https://discord.gg/6s5tn9H4p2"
	aboutIssuesURL  = "https://github.com/t8y2/motty/issues"
	aboutRepoURL    = "https://github.com/t8y2/motty"
	aboutDocsURL    = "https://motty.page"
	aboutVivaURL    = "https://vivaui.dev"
)

// linkMark is how a place to go is drawn: the glyph and its size, the chip it
// stands on and the color it is drawn in. A glyph that carries colors of its
// own (the QQ penguin) keeps them; the ink then only says how solid it is. A
// mark that comes as a picture, such as the application's own icon, brings its
// tile with it and is drawn as it is.
type linkMark struct {
	icon    uint8
	chip    gfx.Color
	ink     gfx.Color
	size    float32
	picture func() gfx.Bitmap
}

// aboutMarkInk and aboutMarkTile are the white a brand mark is drawn on and the
// color it is drawn in, which is what most of these logos ask for.
var (
	aboutMarkInk  = gfx.RGB(0xFF, 0xFF, 0xFF)
	aboutMarkTile = gfx.RGB(0xFF, 0xFF, 0xFF)
)

// The community's own marks, on the chip each one is drawn for: the logos that
// come in color stand on white, and the ones drawn white take their service's
// hue. These are the only hues on the page, so a place to go is told apart by
// the service it opens rather than by a decoration the page invented.
var (
	aboutQQMark      = linkMark{icon: iconQQ, chip: aboutMarkTile, ink: aboutMarkInk, size: 21}                   // Tencent's penguin
	aboutWeChatMark  = linkMark{icon: iconWeChat, chip: gfx.RGB(0x07, 0xC1, 0x60), ink: aboutMarkInk, size: 16}   // WeChat's green
	aboutDiscordMark = linkMark{icon: iconDiscord, chip: aboutMarkTile, ink: gfx.RGB(0x58, 0x65, 0xF2), size: 17} // Discord's blurple
	aboutIssuesMark  = linkMark{icon: iconBug, chip: gfx.RGB(0xC0, 0x55, 0x4E), ink: aboutMarkInk, size: 16}      // a bug's red, kept quiet
)

// The project's two: GitHub's mark on the same white chip the community icons
// use, and the application's own icon for the site it opens.
var (
	aboutRepoMark = linkMark{icon: iconGitHub, chip: aboutMarkTile, ink: gfx.RGB(0x18, 0x17, 0x17), size: 17}
	aboutDocsMark = linkMark{picture: aboutIconTile}
)

// aboutIconTile is the application icon in its rounded tile, the same mark the
// Dock shows, ready for the About page to paint at its own size.
var aboutIconTile = embeddedBitmap(assets.IconTile, 1<<60|3)

var (
	hitAboutCopyInfo = tree.ID(settingsHits, "settings-about-copy")
	hitAboutQQ       = tree.ID(settingsHits, "settings-about-qq")
	hitAboutWeChat   = tree.ID(settingsHits, "settings-about-wechat")
	hitAboutDiscord  = tree.ID(settingsHits, "settings-about-discord")
	hitAboutIssues   = tree.ID(settingsHits, "settings-about-issues")
	hitAboutRepo     = tree.ID(settingsHits, "settings-about-repo")
	hitAboutDocs     = tree.ID(settingsHits, "settings-about-docs")
	hitAboutViva     = tree.ID(settingsHits, "settings-about-viva")
)

// aboutBuildInfo is what this copy of the program is: its version, the platform
// it was built for, the Go runtime under it and the commit it came from.
type aboutBuildInfo struct {
	version     string
	versionText string // the version as the page shows it
	platform    string
	build       string // the commit and its date, or "" without one
	runtime     string
	copyText    string // the same facts as one block for a bug report
}

// aboutFacts reads the build once. The strings are made here, not on a paint,
// because a settings page repaints on every pointer move and must not allocate.
var aboutFacts = sync.OnceValue(func() aboutBuildInfo {
	info, _ := debug.ReadBuildInfo()
	return readAboutFacts(info)
})

func readAboutFacts(info *debug.BuildInfo) aboutBuildInfo {
	facts := aboutBuildInfo{
		version:  update.Current,
		platform: aboutPlatform(runtime.GOOS, runtime.GOARCH),
		runtime:  runtime.Version(),
	}
	facts.versionText = "v" + facts.version
	if info != nil {
		var revision, when string
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.time":
				when = setting.Value
			}
		}
		if len(revision) > 7 {
			revision = revision[:7]
		}
		if revision != "" {
			facts.build = revision
			if stamp, err := time.Parse(time.RFC3339, when); err == nil {
				facts.build += " · " + stamp.Format("2006-01-02")
			}
		}
	}
	facts.copyText = "Motty " + facts.version + "\nplatform: " + facts.platform + "\nruntime: " + facts.runtime + "\n"
	if facts.build != "" {
		facts.copyText += "build: " + facts.build + "\n"
	}
	return facts
}

// aboutPlatform names the operating system and architecture as a person reads
// them, while keeping the runtime's own names for anything unrecognised.
func aboutPlatform(goos, goarch string) string {
	system := goos
	switch goos {
	case "darwin":
		system = "macOS"
	case "windows":
		system = "Windows"
	case "linux":
		system = "Linux"
	case "freebsd":
		system = "FreeBSD"
	}
	switch goarch {
	case "amd64":
		goarch = "x86-64"
	case "386":
		goarch = "x86"
	}
	return system + " · " + goarch
}

// buildText names the build, with a word of its own when no commit was recorded.
func (facts aboutBuildInfo) buildText(m i18n.AboutMessages) string {
	if facts.build == "" {
		return m.BuildUnavailable
	}
	return facts.build
}

// Tiles are two to a row whenever the page is at least this wide; below it one
// tile per row keeps each one readable. The page width is known while it is
// declared, so the layout is chosen rather than approximated by a minimum cell.
const aboutTwoColumnMin = float32(460)

// A card's mark sits on a chip of its own color: 26 pixels is enough for the
// hue to read beside the name, yet small enough that the two lines of a card
// keep their room. A mark without a color of its own takes a quiet share of the
// page's ink instead, so the community's hues stay the ones that stand out.
const linkMarkSize = float32(26)

// tileBox is the inset grid of a group's card: rows of tiles with room between
// them. It takes the place of a row, so whatever follows it gets a hairline.
func (page *settingsPage) tileBox(children func()) {
	page.separator()
	ui.Column(page.c).FillWidth().Pad(14).Gap(10).Children(children)
}

// pair places two tiles side by side, or one above the other when the page is
// too narrow for two.
func (page *settingsPage) pair(first, second func()) {
	if page.width < aboutTwoColumnMin {
		ui.Column(page.c).FillWidth().Gap(10).Children(func() {
			first()
			second()
		})
		return
	}
	ui.Row(page.c).FillWidth().Gap(10).Children(func() {
		ui.Column(page.c).Grow(1).Basis(0).Children(first)
		ui.Column(page.c).Grow(1).Basis(0).Children(second)
	})
}

// factTile is one fact of the build: its name above its value.
func (page *settingsPage) factTile(icon uint8, label, value string) {
	c, pal := page.c, page.state.pal
	tile := ui.Column(c).FillWidth().Height(60).PadSides(10, 12, 10, 12).Justify(ui.Center).Gap(3).Background(pal.surfaceSoft).Radius(radius.item)
	page.matchAnchor(label, tile, 0)
	tile.Children(func() {
		ui.Row(c).Items(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, lucideIcons[icon]).Size(14, 14).Color(pal.muted).Shrink(0)
			ui.Text(c, label).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(15)
		})
		ui.Text(c, value).Mono().FontSize(metrics.FontSizeM).Color(pal.text).LineHeight(19).Ellipsis()
	})
}

// linkCard is one place to go: its mark, its name, what it is for, and a sign
// that it opens outside the window. The whole card is the target.
func (page *settingsPage) linkCard(id gfx.ID, mark linkMark, label, description string) {
	c, pal := page.c, page.state.pal
	canvas := page.canvas()
	background := pal.surfaceSoft
	if canvas.Hovered() == id {
		background = pal.surfaceHot
	}
	border := withAlpha(pal.line, 0.85)
	if canvas.Focused() == id {
		border = pal.accent
	}
	card := ui.Column(c).FillWidth().Height(86).PadSides(12, 12, 12, 12).Gap(5).Justify(ui.Center).Background(background).Radius(radius.control).Border(1, border)
	page.matchAnchor(label, card, 0)
	page.hitOn(card, id, label, gfx.HitOptions{Role: gfx.RoleLink, Cursor: gfx.PointerCursorHand, Description: description})
	card.Children(func() {
		ui.Row(c).Items(ui.Center).Gap(8).Children(func() {
			page.linkMarkIcon(mark)
			ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(20).Grow(1).Ellipsis()
			ui.Icon(c, lucideIcons[iconLink]).Size(12, 12).Color(pal.faint).Shrink(0)
		})
		ui.Paragraph(c, description, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(15)
	})
}

// linkMarkIcon draws a card's mark: its glyph, at the size that mark takes, on
// the chip of its service, or the picture that brings its own tile.
func (page *settingsPage) linkMarkIcon(mark linkMark) {
	c := page.c
	if mark.picture != nil {
		ui.Image(c, mark.picture()).Size(linkMarkSize, linkMarkSize).Shrink(0)
		return
	}
	chip := ui.Row(c).Size(linkMarkSize, linkMarkSize).Shrink(0).Center().Radius(radius.item).Background(mark.chip)
	chip.Children(func() {
		ui.Icon(c, lucideIcons[mark.icon]).Size(mark.size, mark.size).Color(mark.ink).Shrink(0)
	})
}

// vivaColophon closes the page with the framework that draws the window. It is
// not a place to go like the cards above, so it is one centered line below
// them, and the words and the mark are the whole of its target.
func (page *settingsPage) vivaColophon() {
	c, pal := page.c, page.state.pal
	m := page.state.messages
	color := pal.muted
	if canvas := page.canvas(); canvas.Hovered() == hitAboutViva || canvas.Focused() == hitAboutViva {
		color = pal.accent
	}
	ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Center).Children(func() {
		line := ui.Row(c).Items(ui.Center).Gap(5)
		page.matchAnchor(m.About.Viva, line, 0)
		page.hitOn(line, hitAboutViva, m.About.Viva, gfx.HitOptions{Role: gfx.RoleLink, Cursor: gfx.PointerCursorHand, Description: m.About.VivaDescription})
		line.Children(func() {
			ui.Icon(c, lucideIcons[iconLink]).Size(11, 11).Color(color).Shrink(0)
			ui.Text(c, m.About.Viva).FontSize(metrics.FontSizeS).Color(color).LineHeight(15)
		})
	})
}

// paintAboutSettings paints the About category: what this copy is, how to bring
// it up to date, where to report a problem and where the project lives.
func (state *State) paintAboutSettings(page *settingsPage) {
	m, pal := state.messages, state.pal
	facts := aboutFacts()
	copyAction := m.About.CopyAction
	if state.settings.aboutCopied {
		copyAction = m.About.CopiedAction
	}
	c := page.c
	page.groups(func() {
		page.group("", func() {
			// The wordmark is a picture, so it keeps a slot the page paints
			// once it is laid out, as the sidebar header does.
			row := ui.Row(c).Height(96).PadX(18).Gap(16).Items(ui.Center)
			row.Children(func() {
				ui.Column(c).Grow(1).Children(func() {
					page.add(settingsControl{kind: controlLogo, slot: ui.Slot(c).Size(lockupWidth, lockupHeight).Shrink(0)})
					ui.Paragraph(c, m.About.Tagline, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(16).MarginSides(6, 0, 0, 0)
				})
				page.button(hitAboutCopyInfo, copyAction, aiButtonOutline, false).Shrink(0)
			})
		})
		page.group(m.About.InfoTitle, func() {
			page.tileBox(func() {
				page.pair(func() {
					page.factTile(iconInfo, m.About.Version, facts.versionText)
				}, func() {
					page.factTile(iconDesktop, m.About.Platform, facts.platform)
				})
				page.pair(func() {
					page.factTile(iconWrench, m.About.Build, facts.buildText(m.About))
				}, func() {
					page.factTile(iconTerminal, m.About.Runtime, facts.runtime)
				})
			})
		})
		// The update controls change the copy the facts above describe, so they
		// sit with it rather than among the application's other preferences.
		page.group(m.About.UpdateTitle, func() {
			state.paintUpdateRows(page)
		})
		page.group(m.About.CommunityTitle, func() {
			page.tileBox(func() {
				page.pair(func() {
					page.linkCard(hitAboutQQ, aboutQQMark, m.About.QQ, m.About.QQDescription)
				}, func() {
					page.linkCard(hitAboutWeChat, aboutWeChatMark, m.About.WeChat, m.About.WeChatDescription)
				})
				page.pair(func() {
					page.linkCard(hitAboutDiscord, aboutDiscordMark, m.About.Discord, m.About.DiscordDescription)
				}, func() {
					page.linkCard(hitAboutIssues, aboutIssuesMark, m.About.Issues, m.About.IssuesDescription)
				})
			})
		})
		page.group(m.About.ProjectTitle, func() {
			page.tileBox(func() {
				page.pair(func() {
					page.linkCard(hitAboutRepo, aboutRepoMark, m.About.Repo, m.About.RepoDescription)
				}, func() {
					page.linkCard(hitAboutDocs, aboutDocsMark, m.About.Docs, m.About.DocsDescription)
				})
			})
		})
		page.vivaColophon()
	})
}

// clickAboutSettings handles the presses of the About page. It reports whether
// the identity was one of them.
func (state *State) clickAboutSettings(id gfx.ID) bool {
	switch id {
	case hitAboutCopyInfo:
		state.copyAboutInfo()
	case hitAboutQQ:
		state.openExternal(aboutQQURL)
	case hitAboutWeChat:
		state.openExternal(aboutWeChatURL)
	case hitAboutDiscord:
		state.openExternal(aboutDiscordURL)
	case hitAboutIssues:
		state.openExternal(aboutIssuesURL)
	case hitAboutRepo:
		state.openExternal(aboutRepoURL)
	case hitAboutDocs:
		state.openExternal(aboutDocsURL)
	case hitAboutViva:
		state.openExternal(aboutVivaURL)
	default:
		return false
	}
	return true
}

// copyAboutInfo puts the build's facts on the clipboard, ready to paste into a
// bug report.
func (state *State) copyAboutInfo() {
	if err := state.options.SetClipboardText(aboutFacts().copyText); err != nil {
		state.settings.aboutCopied = false
		state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		return
	}
	state.settings.aboutCopied = true
	state.status = state.messages.About.Copied
}
