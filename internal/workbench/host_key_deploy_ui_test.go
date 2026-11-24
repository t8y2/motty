package workbench

import (
	"context"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

func TestHostKeyManagementReviewDoesNotWriteBeforeConfirmation(t *testing.T) {
	state := displayOrderState(t)
	state.setLanguage(i18n.Chinese)
	state.hostSelections = []uint32{state.hosts[0].ID, state.hosts[1].ID}
	state.identities = []identityRecord{{ID: 51, Name: "Stored test key", Path: "test", Stored: true, PublicKey: "intentionally-invalid"}, {ID: 52, Name: "Unsupported external key", Path: "external", PublicKey: "test"}}
	state.Click(hitHostKeyManage)
	view := &state.hostKeyDeployView
	if state.modal != modalHostKeyDeploy || len(view.targets) != 2 || view.switchAuth {
		t.Fatalf("initial selection or switch state is wrong: %+v", view)
	}
	state.Click(hitHostKeyDeployApply) // Not reachable via normal UI: exercise the defensive click gate.
	if view.started || len(state.hostKeyDeploy.results) > 0 {
		t.Fatal("confirmation required before any attempt")
	}
	state.Click(hitHostKeyDeployKeys.At(52))
	state.Click(hitHostKeyDeployNext)
	if view.confirm {
		t.Fatal("unsupported external keys cannot be selected")
	}
	state.Click(hitHostKeyDeployKeys.At(51))
	state.Click(hitHostKeyDeploySwitch)
	state.Click(hitHostKeyDeployNext)
	if !view.confirm || !view.switchAuth || view.started {
		t.Fatal("review should freeze the chosen key and authentication switch without deploying")
	}
	state.Click(hitHostKeyDeploySwitch)
	if !view.switchAuth {
		t.Fatal("review must not let switch choice change")
	}
	state.Click(hitHostKeyDeployApply) // Synthetic invalid key: backend exits before creating a client.
	if !view.started || state.hostKeyDeploy.running || len(state.hostKeyDeploy.results) != 2 {
		t.Fatalf("expected only local validation results after explicit confirm: %+v", state.hostKeyDeploy)
	}
	for _, r := range state.hostKeyDeploy.results {
		if r.Imported || r.Switched || r.Error == "" {
			t.Fatalf("unexpected success: %+v", r)
		}
	}
	state.Click(hitHostKeyDeployClose)
	state.Click(hitHostKeyManage)
	if state.hostKeyDeployView.switchAuth {
		t.Fatal("a new batch must default to public-key-only")
	}
}

func TestHostKeyManagementChangedTargetCannotCarryOldConfirmation(t *testing.T) {
	state := displayOrderState(t)
	state.hostSelections = []uint32{state.hosts[0].ID}
	state.identities = []identityRecord{{ID: 51, Name: "Stored test key", Path: "test", Stored: true, PublicKey: "bad"}}
	state.Click(hitHostKeyManage)
	state.Click(hitHostKeyDeployKeys.At(51))
	state.Click(hitHostKeyDeployNext)
	if !state.hostKeyDeployView.confirm {
		t.Fatal("expected preview")
	}
	state.hosts[0].Address = "127.0.0.99"
	state.Click(hitHostKeyDeployApply)
	if !state.hostKeyDeployView.changed || state.hostKeyDeployView.started || len(state.hostKeyDeploy.results) != 0 {
		t.Fatal("a different target cannot be dispatched using a previous confirmation")
	}
}

func TestHostKeyManagementNativeToolbarAndReviewControls(t *testing.T) {
	for _, w := range []float32{960, 1400} {
		state := displayOrderState(t)
		state.setLanguage(i18n.Chinese)
		state.hostSelections = []uint32{state.hosts[0].ID}
		state.identities = []identityRecord{{ID: 51, Name: "Stored test key", Path: "test", Stored: true, PublicKey: "bad"}}
		var canvas gfx.Canvas
		size := gfx.Size{Width: w, Height: 600}
		state.Paint(&canvas, size)
		if _, ok := hitByIDSnapshot(canvas.Hits(), hitHostKeyManage); !ok {
			t.Fatalf("toolbar entry unavailable at %v", w)
		}
		state.Click(hitHostKeyManage)
		canvas.Reset()
		state.Paint(&canvas, size)
		for _, id := range []gfx.ID{hitHostKeyDeployClose, hitHostKeyDeployKeys.At(51), hitHostKeyDeploySwitch} {
			if hit, ok := hitByIDSnapshot(canvas.Hits(), id); !ok || hit.Disabled {
				t.Fatalf("dialog control unavailable: %v", id)
			}
		}
		if hit, ok := hitByIDSnapshot(canvas.Hits(), hitHostKeyDeployNext); !ok || !hit.Disabled {
			t.Fatal("review must be disabled without a selected key")
		}
		state.Click(hitHostKeyDeployKeys.At(51))
		state.Click(hitHostKeyDeployNext)
		canvas.Reset()
		state.Paint(&canvas, size)
		if hit, ok := hitByIDSnapshot(canvas.Hits(), hitHostKeyDeployApply); !ok || hit.Disabled {
			t.Fatal("explicit confirm not mounted")
		}
		if _, ok := hitByIDSnapshot(canvas.Hits(), hitHostKeyDeploySwitch); ok {
			t.Fatal("review still exposes mutable switch")
		}
	}
}

func TestHostKeyManagementStopIsExplicitAndDoesNotCloseRunningBatch(t *testing.T) {
	state := displayOrderState(t)
	state.hostSelections = []uint32{state.hosts[0].ID}
	state.Click(hitHostKeyManage)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state.hostKeyDeploy.running = true
	state.hostKeyDeploy.cancel = cancel
	state.hostKeyDeployView.started = true
	state.Click(hitHostKeyDeployClose)
	if state.modal != modalHostKeyDeploy || ctx.Err() != nil {
		t.Fatal("running batch cannot silently close or cancel")
	}
	state.Click(hitHostKeyDeployCancel)
	if state.modal != modalHostKeyDeploy || ctx.Err() == nil || !state.hostKeyDeploy.canceled {
		t.Fatal("explicit stop must cancel remaining work without hiding results")
	}
}

func TestHostKeyManagementToolbarLabel(t *testing.T) {
	state := displayOrderState(t)
	for _, entry := range []struct {
		language i18n.Language
		label    string
	}{
		{i18n.English, "Key management"},
		{i18n.Chinese, "密钥管理"},
		{i18n.TraditionalChinese, "金鑰管理"},
	} {
		state.setLanguage(entry.language)
		if got := state.hostKeyManagementLabel(); got != entry.label {
			t.Fatalf("label in %v = %q, want %q", entry.language, got, entry.label)
		}
	}
}
