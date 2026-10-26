// Command issuecommands runs the /claim, /unclaim and /close commands that
// contributors write as comments on GitHub issues. The workflow in
// .github/workflows/issue-commands.yml starts it with the issue and the comment
// in the environment; there is no other place it runs.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// command is what a comment asks for. A comment that names none is ignored.
type command string

const (
	commandClaim   command = "claim"
	commandUnclaim command = "unclaim"
	commandClose   command = "close"
)

var (
	// /unclaim releases a claim; /unclaimed is the wording contributors reach
	// for often enough that it is accepted, as in dbx.
	unclaimPattern = regexp.MustCompile(`^\s*/unclaim(?:ed)?\s*$`)
	closePattern   = regexp.MustCompile(`^\s*/close\s*$`)
	// /claim may open a sentence ("I would like to /claim this"), so it only
	// needs to stand as a word. The other two commands act on an existing
	// claim, so they must be the whole comment.
	claimPattern = regexp.MustCompile(`(?:^|\s)/claim(?:\s|$)`)
)

func detectCommand(body string) command {
	switch {
	case unclaimPattern.MatchString(body):
		return commandUnclaim
	case closePattern.MatchString(body):
		return commandClose
	case claimPattern.MatchString(body):
		return commandClaim
	default:
		return ""
	}
}

// runner runs gh and returns its standard output. Tests replace it.
type runner func(args ...string) (string, error)

func runGh(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), message)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// github is the slice of the GitHub API these commands need. It goes through
// gh so the workflow passes one token in GH_TOKEN and no HTTP client or API
// version lives here.
type github struct {
	repo string
	run  runner
}

func newGitHub(repo string, run runner) *github {
	if run == nil {
		run = runGh
	}
	return &github{repo: repo, run: run}
}

type assignee struct {
	Login string `json:"login"`
}

// issueView is `gh issue view --json state,assignees`.
type issueView struct {
	State     string     `json:"state"`
	Assignees []assignee `json:"assignees"`
}

func (g *github) issue(number int) (issueView, error) {
	output, err := g.run("issue", "view", strconv.Itoa(number), "--repo", g.repo, "--json", "state,assignees")
	if err != nil {
		return issueView{}, err
	}
	var view issueView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		return issueView{}, fmt.Errorf("decode issue #%d: %w", number, err)
	}
	return view, nil
}

func (g *github) comment(number int, body string) error {
	_, err := g.run("issue", "comment", strconv.Itoa(number), "--repo", g.repo, "--body", body)
	return err
}

func (g *github) addAssignee(number int, login string) error {
	_, err := g.run("issue", "edit", strconv.Itoa(number), "--repo", g.repo, "--add-assignee", login)
	return err
}

func (g *github) removeAssignee(number int, login string) error {
	_, err := g.run("issue", "edit", strconv.Itoa(number), "--repo", g.repo, "--remove-assignee", login)
	return err
}

func (g *github) close(number int, body string) error {
	_, err := g.run("issue", "close", strconv.Itoa(number), "--repo", g.repo, "--reason", "completed", "--comment", body)
	return err
}

// closeContext is what /close needs: the issue state, the branch a pull request
// had to merge into, the people who claimed the issue, and every pull request
// that cross-references it.
type closeContext struct {
	State         string
	DefaultBranch string
	Assignees     []assignee
	PullRequests  []pullRequestView
}

type pullRequestView struct {
	Number     int
	State      string
	BaseBranch string
	MergedAt   string
	Author     string
	Repository string
}

// closeContextQuery walks the cross-referenced pull requests of one issue. The
// timeline is paginated; the loop below follows the cursor.
const closeContextQuery = `
  query($owner: String!, $name: String!, $number: Int!, $endCursor: String) {
    repository(owner: $owner, name: $name) {
      defaultBranchRef {
        name
      }
      issue(number: $number) {
        state
        assignees(first: 20) {
          nodes {
            login
          }
        }
        timelineItems(first: 100, after: $endCursor, itemTypes: [CROSS_REFERENCED_EVENT]) {
          pageInfo {
            hasNextPage
            endCursor
          }
          nodes {
            ... on CrossReferencedEvent {
              source {
                __typename
                ... on PullRequest {
                  number
                  state
                  baseRefName
                  mergedAt
                  author {
                    login
                  }
                  repository {
                    nameWithOwner
                  }
                }
              }
            }
          }
        }
      }
    }
  }
`

type graphQLPage struct {
	Data struct {
		Repository *struct {
			DefaultBranchRef *struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
			Issue *struct {
				State     string `json:"state"`
				Assignees struct {
					Nodes []assignee `json:"nodes"`
				} `json:"assignees"`
				TimelineItems struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Source *struct {
							TypeName    string `json:"__typename"`
							Number      int    `json:"number"`
							State       string `json:"state"`
							BaseRefName string `json:"baseRefName"`
							MergedAt    string `json:"mergedAt"`
							Author      *struct {
								Login string `json:"login"`
							} `json:"author"`
							Repository *struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
						} `json:"source"`
					} `json:"nodes"`
				} `json:"timelineItems"`
			} `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
}

func (g *github) closeContext(number int) (closeContext, error) {
	owner, name, ok := strings.Cut(g.repo, "/")
	if !ok || owner == "" || name == "" {
		return closeContext{}, fmt.Errorf("invalid repository %q", g.repo)
	}

	var context closeContext
	var cursor string
	for {
		args := []string{
			"api", "graphql",
			"-f", "query=" + closeContextQuery,
			"-f", "owner=" + owner,
			"-f", "name=" + name,
			"-F", "number=" + strconv.Itoa(number),
		}
		if cursor != "" {
			args = append(args, "-f", "endCursor="+cursor)
		}

		output, err := g.run(args...)
		if err != nil {
			return closeContext{}, err
		}
		var page graphQLPage
		if err := json.Unmarshal([]byte(output), &page); err != nil {
			return closeContext{}, fmt.Errorf("decode the close context of #%d: %w", number, err)
		}

		repository := page.Data.Repository
		if repository == nil || repository.Issue == nil {
			return closeContext{}, fmt.Errorf("issue #%d was not found in %s", number, g.repo)
		}
		if context.DefaultBranch == "" {
			if repository.DefaultBranchRef == nil || repository.DefaultBranchRef.Name == "" {
				return closeContext{}, fmt.Errorf("%s has no default branch", g.repo)
			}
			context = closeContext{
				State:         repository.Issue.State,
				DefaultBranch: repository.DefaultBranchRef.Name,
				Assignees:     repository.Issue.Assignees.Nodes,
			}
		}

		for _, node := range repository.Issue.TimelineItems.Nodes {
			source := node.Source
			if source == nil || source.TypeName != "PullRequest" {
				continue
			}
			view := pullRequestView{
				Number:     source.Number,
				State:      source.State,
				BaseBranch: source.BaseRefName,
				MergedAt:   source.MergedAt,
			}
			if source.Author != nil {
				view.Author = source.Author.Login
			}
			if source.Repository != nil {
				view.Repository = source.Repository.NameWithOwner
			}
			context.PullRequests = append(context.PullRequests, view)
		}

		pageInfo := repository.Issue.TimelineItems.PageInfo
		if !pageInfo.HasNextPage {
			return context, nil
		}
		if pageInfo.EndCursor == "" {
			return closeContext{}, fmt.Errorf("the timeline of #%d has another page but no cursor", number)
		}
		cursor = pageInfo.EndCursor
	}
}

// environment is one comment. It comes from the workflow.
type environment struct {
	number   int
	body     string
	user     string
	userType string
	repo     string
}

func environmentFromOS() (environment, error) {
	number, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ISSUE_NUMBER")))
	if err != nil || number <= 0 {
		return environment{}, fmt.Errorf("ISSUE_NUMBER %q is not an issue number", os.Getenv("ISSUE_NUMBER"))
	}
	env := environment{
		number:   number,
		body:     os.Getenv("COMMENT_BODY"),
		user:     os.Getenv("COMMENT_USER"),
		userType: os.Getenv("COMMENT_USER_TYPE"),
		repo:     os.Getenv("GITHUB_REPOSITORY"),
	}
	if env.user == "" {
		return environment{}, fmt.Errorf("COMMENT_USER is empty")
	}
	owner, name, ok := strings.Cut(env.repo, "/")
	if !ok || owner == "" || name == "" {
		return environment{}, fmt.Errorf("GITHUB_REPOSITORY %q is not owner/name", env.repo)
	}
	return env, nil
}

// handle runs the command in env and returns a status for the workflow log.
func handle(env environment, gh *github) (string, error) {
	name := detectCommand(env.body)
	if name == "" {
		return "ignored", nil
	}
	// A bot must not answer itself, and an App's own comments arrive as Bot.
	if strings.EqualFold(env.userType, "Bot") {
		return "ignored-bot", nil
	}

	switch name {
	case commandClaim:
		return claim(env, gh)
	case commandUnclaim:
		return unclaim(env, gh)
	default:
		return closeIssue(env, gh)
	}
}

func claim(env environment, gh *github) (string, error) {
	view, err := gh.issue(env.number)
	if err != nil {
		return "", err
	}
	if view.State != "OPEN" {
		return "closed", gh.comment(env.number, fmt.Sprintf("❌ @%s，#%d 已经关闭，无法认领。", env.user, env.number))
	}

	if len(view.Assignees) > 0 {
		if len(view.Assignees) == 1 && sameLogin(view.Assignees[0].Login, env.user) {
			return "already-claimed", gh.comment(env.number, fmt.Sprintf("ℹ️ @%s 你已经认领了 #%d，无需重复认领。", env.user, env.number))
		}
		names := make([]string, 0, len(view.Assignees))
		for _, who := range view.Assignees {
			names = append(names, "@"+who.Login)
		}
		return "already-claimed", gh.comment(env.number, fmt.Sprintf("❌ @%s 这个 issue 已经有人认领了：%s", env.user, strings.Join(names, ", ")))
	}

	if err := gh.addAssignee(env.number, env.user); err != nil {
		// The claim did not take effect, so say that on the issue rather than
		// reporting success and leaving the issue looking free.
		if commentErr := gh.comment(env.number, fmt.Sprintf("❌ @%s 无法认领 #%d：机器人没能把该账号设为 assignee。请维护者手动指派。", env.user, env.number)); commentErr != nil {
			return "", commentErr
		}
		return "", fmt.Errorf("assign #%d to %s: %w", env.number, env.user, err)
	}
	return "claimed", gh.comment(env.number, fmt.Sprintf("✅ @%s 已认领 #%d，开始处理吧！", env.user, env.number))
}

func unclaim(env environment, gh *github) (string, error) {
	view, err := gh.issue(env.number)
	if err != nil {
		return "", err
	}
	if view.State != "OPEN" {
		return "already-closed", nil
	}

	var claimed bool
	for _, who := range view.Assignees {
		if sameLogin(who.Login, env.user) {
			claimed = true
			break
		}
	}
	if !claimed {
		return "not-assignee", gh.comment(env.number, fmt.Sprintf("❌ @%s 你当前没有认领 #%d，无法使用 `/unclaim`。", env.user, env.number))
	}

	if err := gh.removeAssignee(env.number, env.user); err != nil {
		return "", err
	}
	return "unclaimed", gh.comment(env.number, fmt.Sprintf("✅ @%s 已取消认领 #%d，其他贡献者可以继续认领。", env.user, env.number))
}

func closeIssue(env environment, gh *github) (string, error) {
	context, err := gh.closeContext(env.number)
	if err != nil {
		return "", err
	}
	if context.State != "OPEN" {
		return "already-closed", nil
	}

	var claimed bool
	for _, who := range context.Assignees {
		if sameLogin(who.Login, env.user) {
			claimed = true
			break
		}
	}
	if !claimed {
		return "not-assignee", gh.comment(env.number, fmt.Sprintf("❌ @%s 只有当前认领人可以使用 `/close`。", env.user))
	}

	pullRequest, ok := mergedPullRequest(context.PullRequests, env.user, env.repo, context.DefaultBranch)
	if !ok {
		return "no-merged-pull-request", gh.comment(env.number, fmt.Sprintf(
			"❌ @%s 暂不能关闭 #%d：未找到由你提交、已合并到 `%s` 且关联此 Issue 的 PR。",
			env.user, env.number, context.DefaultBranch))
	}

	return "closed", gh.close(env.number, fmt.Sprintf("✅ @%s 的关联 PR #%d 已合并，关闭 #%d。", env.user, pullRequest.Number, env.number))
}

// mergedPullRequest picks the newest merged pull request that the commenter
// wrote against a branch in this repository, so a fork's copy or a stale pull
// request into another branch cannot close an issue.
func mergedPullRequest(pullRequests []pullRequestView, user, repo, defaultBranch string) (pullRequestView, bool) {
	var candidates []pullRequestView
	for _, pullRequest := range pullRequests {
		if pullRequest.State != "MERGED" ||
			pullRequest.Repository != repo ||
			pullRequest.BaseBranch != defaultBranch ||
			!sameLogin(pullRequest.Author, user) {
			continue
		}
		candidates = append(candidates, pullRequest)
	}
	if len(candidates) == 0 {
		return pullRequestView{}, false
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].MergedAt > candidates[right].MergedAt
	})
	return candidates[0], true
}

func sameLogin(left, right string) bool {
	return left != "" && strings.EqualFold(left, right)
}

func main() {
	env, err := environmentFromOS()
	if err != nil {
		fmt.Fprintln(os.Stderr, "issuecommands:", err)
		os.Exit(1)
	}
	status, err := handle(env, newGitHub(env.repo, nil))
	if err != nil {
		fmt.Fprintln(os.Stderr, "issuecommands:", err)
		os.Exit(1)
	}
	fmt.Println("issuecommands:", status)
}
