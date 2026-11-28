package workbench

import (
	"fmt"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const themePreviewSample = "\x1b[32mtudou@motty\x1b[0m:\x1b[34m~/project\x1b[0m $ ls\r\n" +
	"\x1b[34mapp  docs  src\x1b[0m  \x1b[32mbuild.sh\x1b[0m  README.md\r\n" +
	"\x1b[36mconfig.json\x1b[0m  \x1b[31marchive.tar.gz\x1b[0m  go.mod\r\n" +
	"\x1b[32mtudou@motty\x1b[0m:\x1b[34m~/project\x1b[0m $ ll\r\n" +
	"total 144K\r\n" +
	"drwxr-xr-x  3 tudou tudou 4.0K Nov 28 14:30 \x1b[34mapp/\x1b[0m\r\n" +
	"drwxr-xr-x  4 tudou tudou 4.0K Nov 28 14:30 \x1b[34mdocs/\x1b[0m\r\n" +
	"drwxr-xr-x  8 tudou tudou 4.0K Nov 28 14:30 \x1b[34msrc/\x1b[0m\r\n" +
	"-rwxr-xr-x  1 tudou tudou  862 Nov 28 14:32 \x1b[32mbuild.sh\x1b[0m\r\n" +
	"-rw-r--r--  1 tudou tudou 2.5K Nov 28 14:33 README.md\r\n" +
	"-rw-r--r--  1 tudou tudou 1.2K Nov 28 14:34 \x1b[36mconfig.json\x1b[0m\r\n" +
	"-rw-r--r--  1 tudou tudou 120K Nov 28 14:35 \x1b[31marchive.tar.gz\x1b[0m\r\n" +
	"-rw-r--r--  1 tudou tudou  324 Nov 28 14:36 go.mod\r\n" +
	"\x1b[32mtudou@motty\x1b[0m:\x1b[34m~/project\x1b[0m $ "

func (state *State) declareThemePreview(c ui.C) {
	e := &state.themeEditor
	preview := e.previewScheme()
	p := paletteFromScheme(state.appearance.palette(), &preview)
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		ui.Row(c).Items(ui.Center).Children(func() {
			ui.Text(c, state.messages.Themes.LivePreview).Grow(1).Color(state.pal.text).FontSize(15)
			label := state.messages.Themes.Expand
			if e.expanded {
				label = state.messages.Themes.BackToEditor
			}
			ui.Button(c, label).Outline().Hit(hitThemePreview, label)
		})
		ui.Text(c, state.messages.Themes.FollowPreview).Color(state.pal.muted).FontSize(12)
		ui.Column(c).FillWidth().Background(p.shell).Border(1, p.line).Children(func() {
			ui.Row(c).FillWidth().Height(36).PadX(6).Gap(5).Items(ui.Center).Children(func() {
				for i, name := range []string{"project", "live", "deploy"} {
					color := p.shell
					if i == 0 {
						color = p.surface
					}
					ui.Row(c).Grow(1).Basis(0).Height(26).PadX(7).Gap(5).Items(ui.Center).Background(color).Radius(4).Children(func() {
						ui.Text(c, "›_").FontSize(11).Color(p.muted)
						ui.Text(c, name).FontSize(11).Color(p.text)
						ui.Text(c, "●").FontSize(8).Color(p.accent)
					})
				}
				ui.Text(c, "+").FontSize(16).Color(p.text)
				for _, mark := range []uint8{iconPaneMaximize, iconClose} {
					ui.Icon(c, lucideIcons[mark]).Size(12, 12).Color(p.muted)
				}
			})
			ui.Row(c).FillWidth().Height(30).PadX(10).Items(ui.Center).Children(func() {
				ui.Text(c, "●  project").Grow(1).FontSize(11).Color(p.text)
				ui.Text(c, state.messages.Themes.Connected).FontSize(10).Color(p.muted)
			})
			ui.Slot(c).FillWidth().Height(340).Clip().Draw(func(d *ui.DrawContext) { state.paintThemePreviewTerminal(d) })
			ui.Row(c).FillWidth().Height(30).Gap(4).PadX(5).Items(ui.Center).Children(func() {
				for i, name := range []string{"project", "logs", "server"} {
					color := p.shell
					if i == 0 {
						color = p.surface
					}
					ui.Row(c).Grow(1).Basis(0).Height(24).PadX(7).Items(ui.Center).Background(color).Radius(3).Children(func() {
						ui.Text(c, fmt.Sprintf("%d  %s", i+1, name)).FontSize(11).Color(p.text)
					})
				}
			})
		})
		ui.Row(c).Gap(3).Children(func() {
			for i := 0; i < 16; i++ {
				color := schemeRGB(preview.ansi[i])
				ui.Column(c).Grow(1).Basis(0).Height(24).Background(color).Center().Children(func() {
					ink := gfx.RGB(255, 255, 255)
					if luminance(rgb8(color)) > .5 {
						ink = gfx.RGB(0, 0, 0)
					}
					ui.Text(c, fmt.Sprint(i)).FontSize(10).Color(ink)
				})
			}
		})
	})
}
