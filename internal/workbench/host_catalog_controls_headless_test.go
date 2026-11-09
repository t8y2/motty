//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestHostCatalogControlsNativeSelectionOrderingAndExport(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 960, Height: 600}} {
		for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
			for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
				for _, light := range []bool{false, true} {
					t.Run(fmt.Sprintf("%dx%d/view%d/%s/light%v", int(size.Width), int(size.Height), mode, language, light), func(t *testing.T) {
						state := displayOrderState(t)
						state.hostView = mode
						state.setLanguage(language)
						if light {
							state.setAppearance(appearanceLight)
						} else {
							state.setAppearance(appearanceDark)
						}
						exportPath := filepath.Join(t.TempDir(), "catalog.json")
						state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return exportPath, nil }
						view, err := headless.Open(app.Config{
							Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: state.Paint,
							OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
							TextInput: func(context app.TextInputContext) *app.TextInput {
								if editor := state.EditorFor(context.ID); editor != nil {
									return &app.TextInput{Editor: editor}
								}
								return nil
							},
						}, nil, headless.Options{Scale: 1.25})
						if err != nil {
							t.Fatal(err)
						}
						defer view.Close()
						must := func(err error) {
							t.Helper()
							if err != nil {
								t.Fatal(err)
							}
						}
						must(view.Rest())
						hit := func(id gfx.ID) gfx.Hit {
							t.Helper()
							found, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
							if !ok {
								t.Fatalf("missing native control %s", tree.Text(id))
							}
							return found
						}
						click := func(id gfx.ID) {
							t.Helper()
							control := hit(id)
							if control.Disabled {
								t.Fatalf("disabled native control %s", tree.Text(id))
							}
							must(view.Click(control.Bounds.X+control.Bounds.Width/2, control.Bounds.Y+control.Bounds.Height/2))
						}
						shot := func(name string) {
							t.Helper()
							must(view.Rest())
							if directory := os.Getenv("MOTTY_HOST_CATALOG_SCREENSHOTS"); directory != "" && language == i18n.Chinese {
								must(os.MkdirAll(directory, 0700))
								must(view.Screenshot(filepath.Join(directory, fmt.Sprintf("%s-%dx%d-view%d-light%v.png", name, int(size.Width), int(size.Height), mode, light))))
							}
						}
						click(hitHostCheckList.At(1))
						group := hitHostGroupCheck.At(state.hostGroupIndex("Servers"))
						if !hit(hitHostSelectAll).Mixed || !hit(group).Mixed {
							t.Fatal("native partial selection is not mixed")
						}
						shot("partial")
						click(group)
						if len(state.hostSelections) != 7 || !hit(hitHostSelectAll).Checked {
							t.Fatal("native group selection missed offscreen hosts")
						}
						click(hitExportHosts)
						document, err := readHostCatalog(exportPath)
						if err != nil || len(document.Hosts) != 7 {
							t.Fatalf("native group export: hosts=%d err=%v", len(document.Hosts), err)
						}
						click(hitHostSelectAll)
						must(view.Keys("Space"))
						if len(state.hostSelections) != 7 {
							t.Fatal("focused select-all did not activate with Space")
						}
						must(view.Keys("Space"))
						click(hitHostOrder)
						shot("order-menu")
						click(hitHostOrderIP)
						if got, want := displayHostIDs(state), []string{"host_f", "host_b", "host_z", "host_c", "host_a", "host_e", "host_d"}; !slices.Equal(got, want) {
							t.Fatalf("native IP order = %v", got)
						}
						if !hit(hitHostSort).Disabled {
							t.Fatal("native manual reorder remained enabled")
						}
						click(hitHostOrder)
						click(hitHostOrderName)
						click(hitSearch)
						must(view.Type("Alpha"))
						click(hitHostSelectAll)
						if len(state.hostSelections) != 3 {
							t.Fatalf("native search select-all = %v", state.hostSelections)
						}
						click(hitExportHosts)
						document, err = readHostCatalog(exportPath)
						if err != nil || len(document.Hosts) != 3 {
							t.Fatalf("native filtered export: hosts=%d err=%v", len(document.Hosts), err)
						}
						shot("name-filtered")
						controls := []gfx.ID{hitHostSelectAll, hitHostOrder, hitHostSort, hitHostViewList, hitHostViewGrid, hitExportHosts}
						for index, id := range controls {
							control := hit(id)
							if control.Bounds.X < state.hostsBounds.X || control.Bounds.X+control.Bounds.Width > state.hostsBounds.X+state.hostsBounds.Width {
								t.Fatalf("native control escapes hosts panel: %s %+v", tree.Text(id), control.Bounds)
							}
							for _, other := range controls[index+1:] {
								otherBounds := hit(other).Bounds
								if control.Bounds.X < otherBounds.X+otherBounds.Width && otherBounds.X < control.Bounds.X+control.Bounds.Width && control.Bounds.Y < otherBounds.Y+otherBounds.Height && otherBounds.Y < control.Bounds.Y+control.Bounds.Height {
									t.Fatalf("native controls overlap: %s and %s", tree.Text(id), tree.Text(other))
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestHostCatalogControlsNativeCompactPanel(t *testing.T) {
	state := nestedHostGroupState(t)
	state.ai.open, state.ai.panelWidth = true, 5000
	view, err := headless.Open(app.Config{Width: 1262, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnResize: state.Resize}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	ids := []gfx.ID{hitHostSelectAll, hitHostFilterAll, hitHostFilterMore, hitHostOrder, hitHostSort, hitHostViewList, hitHostViewGrid, hitHostGroupsExpandAll, hitHostGroupsCollapseAll}
	for index, id := range ids {
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("compact panel omitted %s", tree.Text(id))
		}
		if hit.Bounds.X < state.hostsBounds.X || hit.Bounds.X+hit.Bounds.Width > state.hostsBounds.X+state.hostsBounds.Width {
			t.Fatalf("compact %s escapes panel width %.0f: %+v", tree.Text(id), state.hostsBounds.Width, hit.Bounds)
		}
		for _, other := range ids[index+1:] {
			otherHit, _ := hitByIDSnapshot(view.Snapshot().Hits, other)
			if hit.Bounds.X < otherHit.Bounds.X+otherHit.Bounds.Width && otherHit.Bounds.X < hit.Bounds.X+hit.Bounds.Width && hit.Bounds.Y < otherHit.Bounds.Y+otherHit.Bounds.Height && otherHit.Bounds.Y < hit.Bounds.Y+hit.Bounds.Height {
				t.Fatalf("compact controls overlap: %s and %s", tree.Text(id), tree.Text(other))
			}
		}
	}
	for _, id := range []gfx.ID{hitHostSelectAll, hitHostOrder, hitHostOrderName} {
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("compact interaction omitted %s", tree.Text(id))
		}
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	if len(state.hostSelections) != 3 || state.hostOrder != hostOrderName {
		t.Fatal("compact selection/order controls did not activate")
	}
	if directory := os.Getenv("MOTTY_HOST_CATALOG_SCREENSHOTS"); directory != "" {
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(directory, "compact-controls.png")); err != nil {
			t.Fatal(err)
		}
	}
}
