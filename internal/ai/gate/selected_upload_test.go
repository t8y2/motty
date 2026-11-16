package gate

import (
	"context"
	"strings"
	"testing"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

type selectedUploadBackend struct {
	*fakeBackend
	called int
}

func (backend *selectedUploadBackend) SelectedFileName(id string) (string, bool) {
	return "build.tar.gz", id == "1"
}

func (backend *selectedUploadBackend) UploadSelected(_ context.Context, args tools.SelectedUploadArgs) (Transfer, error) {
	backend.called++
	return Transfer{State: "completed", RemotePath: args.RemotePath, BytesDone: 1024}, nil
}

func TestSelectedUploadRequiresThisRequestsFileTargetAndExplicitApproval(t *testing.T) {
	backend := &selectedUploadBackend{fakeBackend: &fakeBackend{}}
	approved := false
	gate := Gate{
		Backend: backend, Target: &SessionInfo{ID: "1", Title: "chosen host"},
		Policy: risk.Policy{Enabled: true, AutoUpTo: risk.High},
		Approver: func(_ context.Context, request Request) (bool, string, error) {
			if request.Tool != tools.NameUploadSelected || request.SessionLabel != "chosen host" ||
				!strings.Contains(request.Summary, "build.tar.gz -> /tmp/artifact") {
				t.Fatalf("approval did not identify file and destination: %+v", request)
			}
			return approved, "", nil
		},
	}
	for _, raw := range []string{
		`{"file_id":"2","remote_path":"/tmp/artifact"}`,
		`{"file_id":"1","session":"not-selected","remote_path":"/tmp/artifact"}`,
		`{"file_id":"1","remote_path":"/tmp/artifact","local_path":"/etc/passwd"}`,
	} {
		if result := gate.Run(context.Background(), tools.NameUploadSelected, []byte(raw)); !result.IsError || backend.called != 0 {
			t.Fatalf("unauthorized upload %s: %+v", raw, result)
		}
	}
	args := []byte(`{"file_id":"1","remote_path":"/tmp/artifact"}`)
	if result := gate.Run(context.Background(), tools.NameUploadSelected, args); !result.Denied || backend.called != 0 {
		t.Fatalf("approval bypassed: %+v", result)
	}
	approved = true
	if result := gate.Run(context.Background(), tools.NameUploadSelected, args); result.IsError || backend.called != 1 || !strings.Contains(result.Text, `"state":"completed"`) {
		t.Fatalf("approved upload: %+v", result)
	}
}
