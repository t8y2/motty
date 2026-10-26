package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestDetectCommand(t *testing.T) {
	tests := []struct {
		name string
		body string
		want command
	}{
		{"claim alone", "/claim", commandClaim},
		{"claim in a sentence", "I would like to /claim this one", commandClaim},
		{"claim on its own line", "Looks good.\n/claim", commandClaim},
		{"unclaim", "/unclaim", commandUnclaim},
		{"unclaimed", "/unclaimed", commandUnclaim},
		{"close", "/close", commandClose},
		{"plain word claim", "claim", ""},
		{"prefix of a word", "/claims", ""},
		{"unclaim with a reason", "/unclaim because I ran out of time", ""},
		{"close with a reason", "/close done", ""},
		{"empty", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := detectCommand(test.body); got != test.want {
				t.Fatalf("detectCommand(%q) = %q, want %q", test.body, got, test.want)
			}
		})
	}
}

type fake struct {
	calls   [][]string
	respond func(args []string) (string, error)
}

func newFake(t *testing.T, respond func(args []string) (string, error)) (*github, *fake) {
	t.Helper()
	f := &fake{respond: respond}
	gh := newGitHub("t8y2/motty", func(args ...string) (string, error) {
		f.calls = append(f.calls, args)
		return f.respond(args)
	})
	return gh, f
}

func (f *fake) called(want ...string) bool {
	for _, call := range f.calls {
		matched := true
		for _, part := range want {
			if !slices.Contains(call, part) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func (f *fake) commented() bool {
	return f.called("issue", "comment")
}

// mentions reports whether an argument of any call contains text, for the parts
// of a comment that are interpolated rather than passed whole.
func (f *fake) mentions(text string) bool {
	for _, call := range f.calls {
		for _, arg := range call {
			if strings.Contains(arg, text) {
				return true
			}
		}
	}
	return false
}

func issueEnv(body, user string) environment {
	return environment{number: 7, body: body, user: user, userType: "User", repo: "t8y2/motty"}
}

func issueJSON(state string, logins ...string) string {
	assignees := make([]map[string]string, 0, len(logins))
	for _, login := range logins {
		assignees = append(assignees, map[string]string{"login": login})
	}
	encoded, err := json.Marshal(map[string]any{"state": state, "assignees": assignees})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func issueResponder(state string, logins ...string) func(args []string) (string, error) {
	return func(args []string) (string, error) {
		if args[0] == "issue" && args[1] == "view" {
			return issueJSON(state, logins...), nil
		}
		return "", nil
	}
}

func TestClaim(t *testing.T) {
	t.Run("assigns an unclaimed issue", func(t *testing.T) {
		gh, f := newFake(t, issueResponder("OPEN"))

		status, err := handle(issueEnv("/claim", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "claimed" {
			t.Fatalf("status = %q, want claimed", status)
		}
		if !f.called("issue", "edit", "7", "--add-assignee", "alice") {
			t.Fatalf("the claim was not assigned: %v", f.calls)
		}
		if !f.commented() {
			t.Fatalf("the claim was not confirmed: %v", f.calls)
		}
	})

	t.Run("keeps the first claim", func(t *testing.T) {
		gh, f := newFake(t, issueResponder("OPEN", "bob"))

		status, err := handle(issueEnv("/claim", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "already-claimed" {
			t.Fatalf("status = %q, want already-claimed", status)
		}
		if f.called("--add-assignee") {
			t.Fatalf("a second assignee was added: %v", f.calls)
		}
		if !f.commented() {
			t.Fatalf("the conflict was not reported: %v", f.calls)
		}
	})

	t.Run("repeating your own claim changes nothing", func(t *testing.T) {
		gh, f := newFake(t, issueResponder("OPEN", "alice"))

		status, err := handle(issueEnv("/claim", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "already-claimed" {
			t.Fatalf("status = %q, want already-claimed", status)
		}
		if f.called("--add-assignee") {
			t.Fatalf("the assignment was written twice: %v", f.calls)
		}
	})

	t.Run("does not claim a closed issue", func(t *testing.T) {
		gh, f := newFake(t, issueResponder("CLOSED"))

		if _, err := handle(issueEnv("/claim", "alice"), gh); err != nil {
			t.Fatal(err)
		}
		if f.called("--add-assignee") {
			t.Fatalf("a closed issue was assigned: %v", f.calls)
		}
	})

	t.Run("reports an assignment that did not take effect", func(t *testing.T) {
		gh, f := newFake(t, func(args []string) (string, error) {
			if args[0] == "issue" && args[1] == "view" {
				return issueJSON("OPEN"), nil
			}
			if args[0] == "issue" && args[1] == "edit" {
				return "", fmt.Errorf("gh: Not Found (HTTP 404)")
			}
			return "", nil
		})

		status, err := handle(issueEnv("/claim", "alice"), gh)
		if err == nil {
			t.Fatal("a failed assignment was reported as a claim")
		}
		if status != "" {
			t.Fatalf("status = %q, want no status", status)
		}
		if !f.commented() {
			t.Fatalf("the failed claim was not reported on the issue: %v", f.calls)
		}
	})
}

func TestUnclaim(t *testing.T) {
	t.Run("releases the assignee's claim", func(t *testing.T) {
		gh, f := newFake(t, issueResponder("OPEN", "alice"))

		status, err := handle(issueEnv("/unclaim", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "unclaimed" {
			t.Fatalf("status = %q, want unclaimed", status)
		}
		if !f.called("issue", "edit", "7", "--remove-assignee", "alice") {
			t.Fatalf("the claim was not released: %v", f.calls)
		}
	})

	t.Run("does not release anyone else's claim", func(t *testing.T) {
		gh, f := newFake(t, issueResponder("OPEN", "bob"))

		status, err := handle(issueEnv("/unclaim", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "not-assignee" {
			t.Fatalf("status = %q, want not-assignee", status)
		}
		if f.called("--remove-assignee") {
			t.Fatalf("another contributor's claim was released: %v", f.calls)
		}
	})

	t.Run("does nothing on a closed issue", func(t *testing.T) {
		gh, f := newFake(t, issueResponder("CLOSED", "alice"))

		status, err := handle(issueEnv("/unclaim", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "already-closed" {
			t.Fatalf("status = %q, want already-closed", status)
		}
		if f.called("--remove-assignee") || f.commented() {
			t.Fatalf("a closed issue was touched: %v", f.calls)
		}
	})
}

func pullRequestSource(number int, state, baseBranch, mergedAt, author, repository string) string {
	return fmt.Sprintf(
		`{"__typename":"PullRequest","number":%d,"state":%q,"baseRefName":%q,"mergedAt":%q,"author":{"login":%q},"repository":{"nameWithOwner":%q}}`,
		number, state, baseBranch, mergedAt, author, repository)
}

func closePage(state, defaultBranch string, assignees, sources []string, hasNext bool, cursor string) string {
	assigneeNodes := make([]string, 0, len(assignees))
	for _, login := range assignees {
		assigneeNodes = append(assigneeNodes, fmt.Sprintf(`{"login":%q}`, login))
	}
	timelineNodes := make([]string, 0, len(sources))
	for _, source := range sources {
		timelineNodes = append(timelineNodes, `{"source":`+source+`}`)
	}
	return fmt.Sprintf(
		`{"data":{"repository":{"defaultBranchRef":{"name":%q},"issue":{"state":%q,"assignees":{"nodes":[%s]},"timelineItems":{"pageInfo":{"hasNextPage":%t,"endCursor":%q},"nodes":[%s]}}}}}`,
		defaultBranch, state, strings.Join(assigneeNodes, ","), hasNext, cursor, strings.Join(timelineNodes, ","))
}

func closeResponder(page func(args []string) string) func(args []string) (string, error) {
	return func(args []string) (string, error) {
		if args[0] == "api" && args[1] == "graphql" {
			return page(args), nil
		}
		return "", nil
	}
}

func TestClose(t *testing.T) {
	t.Run("closes for the assignee with a merged pull request", func(t *testing.T) {
		gh, f := newFake(t, closeResponder(func(args []string) string {
			return closePage("OPEN", "main", []string{"alice"},
				[]string{pullRequestSource(42, "MERGED", "main", "2026-10-26T03:00:00Z", "alice", "t8y2/motty")}, false, "")
		}))

		status, err := handle(issueEnv("/close", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "closed" {
			t.Fatalf("status = %q, want closed", status)
		}
		if !f.called("issue", "close", "7") {
			t.Fatalf("the issue was not closed: %v", f.calls)
		}
		if !f.mentions("#42") {
			t.Fatalf("the close did not name the merged pull request: %v", f.calls)
		}
	})

	t.Run("only the assignee can close", func(t *testing.T) {
		gh, f := newFake(t, closeResponder(func(args []string) string {
			return closePage("OPEN", "main", []string{"bob"},
				[]string{pullRequestSource(42, "MERGED", "main", "2026-10-26T03:00:00Z", "alice", "t8y2/motty")}, false, "")
		}))

		status, err := handle(issueEnv("/close", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "not-assignee" {
			t.Fatalf("status = %q, want not-assignee", status)
		}
		if f.called("issue", "close") {
			t.Fatalf("a non-assignee closed the issue: %v", f.calls)
		}
	})

	t.Run("requires a merged pull request from the same author", func(t *testing.T) {
		tests := []struct {
			name   string
			source string
		}{
			{"still open", pullRequestSource(42, "OPEN", "main", "", "alice", "t8y2/motty")},
			{"another branch", pullRequestSource(42, "MERGED", "dev", "2026-10-26T03:00:00Z", "alice", "t8y2/motty")},
			{"a fork", pullRequestSource(42, "MERGED", "main", "2026-10-26T03:00:00Z", "alice", "someone/motty")},
			{"another author", pullRequestSource(42, "MERGED", "main", "2026-10-26T03:00:00Z", "bob", "t8y2/motty")},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				gh, f := newFake(t, closeResponder(func(args []string) string {
					return closePage("OPEN", "main", []string{"alice"}, []string{test.source}, false, "")
				}))

				status, err := handle(issueEnv("/close", "alice"), gh)
				if err != nil {
					t.Fatal(err)
				}
				if status != "no-merged-pull-request" {
					t.Fatalf("status = %q, want no-merged-pull-request", status)
				}
				if f.called("issue", "close") {
					t.Fatalf("the issue was closed without an eligible pull request: %v", f.calls)
				}
			})
		}
	})

	t.Run("follows the timeline past the first page", func(t *testing.T) {
		gh, f := newFake(t, closeResponder(func(args []string) string {
			if slices.Contains(args, "endCursor=PAGE2") {
				return closePage("OPEN", "main", []string{"alice"},
					[]string{pullRequestSource(42, "MERGED", "main", "2026-10-26T03:00:00Z", "alice", "t8y2/motty")}, false, "")
			}
			return closePage("OPEN", "main", []string{"alice"}, nil, true, "PAGE2")
		}))

		status, err := handle(issueEnv("/close", "alice"), gh)
		if err != nil {
			t.Fatal(err)
		}
		if status != "closed" {
			t.Fatalf("status = %q, want closed", status)
		}
		if !f.called("issue", "close", "7") {
			t.Fatalf("the issue was not closed: %v", f.calls)
		}
	})
}

func TestBotsAreIgnored(t *testing.T) {
	gh, f := newFake(t, issueResponder("OPEN"))
	env := issueEnv("/claim", "alice")
	env.userType = "Bot"

	status, err := handle(env, gh)
	if err != nil {
		t.Fatal(err)
	}
	if status != "ignored-bot" {
		t.Fatalf("status = %q, want ignored-bot", status)
	}
	if len(f.calls) != 0 {
		t.Fatalf("a bot comment reached the API: %v", f.calls)
	}
}
