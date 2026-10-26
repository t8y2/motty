# Issue commands

Contributors claim an issue by commenting `/claim`. The workflow in
`.github/workflows/issue-commands.yml` catches that comment and runs
`tools/issuecommands`, which assigns, comments and closes through `gh` and the
GitHub API.

| Comment | Who may use it | Effect |
| --- | --- | --- |
| `/claim` | anyone | assigns the commenter when the issue is open and unassigned; it may appear inside a sentence ("I would like to /claim this") |
| `/unclaim`, `/unclaimed` | the current assignee | drops the assignee, leaving the issue open for someone else |
| `/close` | the current assignee | closes the issue once a merged pull request by the same person into the default branch cross-references it |

Rules:

- A closed issue cannot be claimed. Claiming an issue that is already assigned
  reports who holds it, and claiming your own assignment is a message, not a
  second assignment.
- `/close` never guesses. Without a merged pull request by the commenter it says
  why and leaves the issue open, so a pull request that lands without
  `Closes #123` does not strand the issue. A pull request with that line still
  closes the issue through GitHub itself.
- Comments by bots are ignored, so the bot cannot answer itself.
- Except for `/claim`, a command must be the whole comment; trailing text such
  as `/unclaim I ran out of time` is ignored rather than half-applied.

## Bot identity

By default the workflow uses `GITHUB_TOKEN`, which comments as
`github-actions[bot]`. To comment as a Motty App instead, create a GitHub App
with *Issues: read and write* and *Pull requests: read*, install it on this
repository, and set the variable and secret the workflow reads:

```sh
gh variable set MOTTY_BOT_APP_ID --body <app id>
gh secret set MOTTY_BOT_PRIVATE_KEY < private-key.pem
```

The workflow mints an installation token for the App and uses it for the
assignment and the comments. When the variable is unset it falls back to
`GITHUB_TOKEN`. If GitHub refuses an assignment the job says so on the issue
instead of reporting a claim that did not happen.

## Tests

`go test ./tools/issuecommands` covers command detection, claim conflicts,
unclaim, the merged-pull-request check and timeline pagination.
