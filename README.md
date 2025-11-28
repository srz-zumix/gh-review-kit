# gh-review-kit

A tool to manage GitHub reviews.

## Installation

```sh
gh extension install srz-zumix/gh-review-kit
```

## Commands

### List check runs for a pull request

```sh
gh review-kit checks [pull-request-identifier] [--repo REPO] [--status STATUS] [--conclusion CONCLUSION] [--headers HEADERS] [--all] [--required|--no-required] [--details] [--color COLOR]
```

List check runs for a pull request.

This command is similar to `gh pr checks` but allows filtering by status and conclusion.
You can customize the output columns and filter by required status.

The pull request can be specified by:
- PR number (e.g., `123` or `#123`)
- PR URL (e.g., `https://github.com/owner/repo/pull/123`)
- Branch name (e.g., `feature/my-branch`)
- If omitted, uses the current branch

**Aliases:** `cc`, `check-checks`

**Options:**

- `--all`: Show all check runs, including those without a conclusion (optional, default: false)
- `--color`: Color output: always, never, auto (optional, default: auto)
- `--conclusion, -c`: Filter by conclusion: success, failure, neutral, cancelled, skipped, timed_out, action_required (optional)
- `--details, -d`: Show detailed information (status icon, run ID, job ID, timestamps, URLs) (optional, default: false)
- `--headers, -H`: Columns to display (NAME, STATUS, CONCLUSION, RUN_ID, JOB_ID, STARTED_AT, ELAPSED, DETAILS_URL, etc.) (optional)
- `--no-required`: Show only non-required check runs (optional)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--required`: Show only required check runs (optional)
- `--status, -s`: Filter by status: queued, in_progress, completed (optional)

**Examples:**

```sh
# List check runs for current branch
gh review-kit cc

# List check runs by PR number
gh review-kit cc 123

# List check runs by PR URL
gh review-kit cc https://github.com/owner/repo/pull/123

# List check runs by branch name
gh review-kit cc feature/my-branch

# List only completed check runs
gh review-kit cc 123 --status completed

# List only failed check runs
gh review-kit cc 123 --conclusion failure

# List with detailed information
gh review-kit cc 123 --details

# List with custom columns
gh review-kit cc 123 --headers NAME,STATUS,CONCLUSION,RUN_ID,JOB_ID

# List only required check runs
gh review-kit cc 123 --required

# List check runs in a different repository
gh review-kit cc 123 --repo owner-name/repo-name
```

### Display logs for failed check runs

```sh
gh review-kit flush-failure [pull-request-identifier] [--repo REPO] [--full] [--required|--no-required]
```

Display logs for failed check runs in a pull request.

This command retrieves all check runs with 'failure' conclusion and displays their logs.

The pull request can be specified by:
- PR number (e.g., `123` or `#123`)
- PR URL (e.g., `https://github.com/owner/repo/pull/123`)
- Branch name (e.g., `feature/my-branch`)
- If omitted, uses the current branch

**Aliases:** `ff`, `flush-fail`, `flush-failed`

**Options:**

- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)

**Examples:**

```sh
# Display logs for current branch
gh review-kit ff

# Display logs for failed check runs by PR number
gh review-kit ff 123

# Display logs by PR URL
gh review-kit ff https://github.com/owner/repo/pull/123

# Display logs by branch name
gh review-kit ff feature/my-branch

# Display logs for failed check runs in a different repository
gh review-kit ff 123 --repo owner-name/repo-name
```

### Re-request review for a pull request

```sh
gh review-kit rerequest [pull-request-identifier] [--repo REPO] [--reviewers REVIEWERS] [--exclude-approved] [--expand-team]
```

Re-request review for a pull request.

If reviewers are not specified, the command will re-request review from all reviewers who have already submitted a review.
If reviewers are specified, the command will re-request review from the specified reviewers only.

The pull request can be specified by:
- PR number (e.g., `123` or `#123`)
- PR URL (e.g., `https://github.com/owner/repo/pull/123`)
- Branch name (e.g., `feature/my-branch`)
- If omitted, uses the current branch

**Aliases:** `rr`

**Options:**

- `--exclude-approved`: Exclude reviewers who have already approved (optional)
- `--expand-team`: Expand team reviewers to individual team members (optional)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--reviewers, -r`: Reviewers to re-request (optional, users or teams, e.g., username or org/team)

**Examples:**

```sh
# Re-request review for current branch from all reviewers who have already reviewed
gh review-kit rr

# Re-request review by PR number
gh review-kit rr 123

# Re-request review by PR URL
gh review-kit rr https://github.com/owner/repo/pull/123

# Re-request review by branch name
gh review-kit rr feature/my-branch

# Re-request review from reviewers excluding those who approved
gh review-kit rr 123 --exclude-approved

# Re-request review from specific reviewers
gh review-kit rr 123 --reviewers user1,user2,@org/team

# Re-request review from specific reviewers, excluding those who approved
gh review-kit rr 123 --reviewers user1,user2,user3 --exclude-approved

# Re-request review from a team, expanding to individual members
gh review-kit rr 123 --reviewers @org/team --expand-team

# Re-request review in a different repository
gh review-kit rr 123 --repo owner-name/repo-name
```
