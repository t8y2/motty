package workbench

import (
	"motty/internal/i18n"
	"strings"
	"testing"
)

func TestCLICompatibilityErrorsExplainFailureInBothLanguages(t *testing.T) {
	for _, tc := range []struct {
		reason, detail, command, zh, en string
	}{
		{"missing_capability", "--isolated", "usage", "缺少必要能力", "Missing required capability"},
		{"not_applied", "permission=deny", "policy", "隔离设置未生效", "Isolation setting was not applied"},
		{"invalid_output", "", "version", "无法解析检查结果", "Cannot parse check output"},
		{"unavailable", "", "policy", "无法执行检查命令", "Cannot run check command"},
	} {
		for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
			s := State{messages: i18n.For(language)}
			message := `cli: compatibility: {"program":"custom-agent","version":"9.8.7","reason":"` + tc.reason + `","detail":"` + tc.detail + `","command":"` + tc.command + `"}`
			got := s.aiCLIError(message)
			want := tc.en
			if language == i18n.Chinese {
				want = tc.zh
			}
			for _, part := range []string{"custom-agent", "9.8.7", want, tc.command, tc.detail} {
				if !strings.Contains(got, part) {
					t.Errorf("missing %q in %q", part, got)
				}
			}
			if strings.Contains(got, "compatibility:") || strings.Contains(got, `{"`) {
				t.Errorf("internal error envelope displayed: %s", got)
			}
		}
	}
}
