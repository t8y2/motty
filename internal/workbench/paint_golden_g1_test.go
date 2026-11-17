package workbench

import (
	"testing"

	"motty/internal/ai/risk"
)

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("g1-ai-approval-confirm", func(t *testing.T, s *State) {
			withModel(t, s)
			approvalFixture(s, risk.Critical, true)
		}),
		plainScene("g1-ai-approval-confirm-typed", func(t *testing.T, s *State) {
			withModel(t, s)
			approvalFixture(s, risk.Critical, true)
			s.ai.confirm.Insert("era")
		}),
	)
}
