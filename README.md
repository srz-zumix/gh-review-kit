# gh-review-kit

A tool to manage GitHub reviews.

## Installation

```sh
gh extension install srz-zumix/gh-review-kit
```

## Commands

### List check runs for a pull request

```sh
gh review-kit checks <pull-request-number> [--repo REPO] [--status STATUS] [--conclusion CONCLUSION] [--headers HEADERS] [--all] [--required|--not-required] [--details] [--color COLOR]
```

List check runs for a pull request.

This command is similar to `gh pr checks` but allows filtering by status and conclusion.
You can customize the output columns and filter by required status.

**Aliases:** `cc`, `check-checks`

**Options:**

- `--all`: Show all check runs, including those without a conclusion (optional, default: false)
- `--color`: Color output: always, never, auto (optional, default: auto)
- `--conclusion, -c`: Filter by conclusion: success, failure, neutral, cancelled, skipped, timed_out, action_required (optional)
- `--details, -d`: Show detailed information (status icon, run ID, job ID, timestamps, URLs) (optional, default: false)
- `--headers, -H`: Columns to display (NAME, STATUS, CONCLUSION, RUN_ID, JOB_ID, STARTED_AT, ELAPSED, DETAILS_URL, etc.) (optional)
- `--not-required`: Show only non-required check runs (optional)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--required`: Show only required check runs (optional)
- `--status, -s`: Filter by status: queued, in_progress, completed (optional)

**Examples:**

```sh
# List all check runs for a pull request
gh review-kit cc 123

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
gh review-kit flush-failure <pull-request-number> [--repo REPO]
```

Display logs for failed check runs in a pull request.

This command retrieves all check runs with 'failure' conclusion and displays their logs.

**Aliases:** `ff`, `flush-fail`, `flush-failed`

**Options:**

- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)

**Examples:**

```sh
# Display logs for all failed check runs
gh review-kit ff 123

# Display logs for failed check runs in a different repository
gh review-kit ff 123 --repo owner-name/repo-name
```

### Re-request review for a pull request

```sh
gh review-kit rerequest <pull-request-number> [--repo REPO] [--reviewers REVIEWERS] [--exclude-approved] [--expand-team]
```

Re-request review for a pull request.

If reviewers are not specified, the command will re-request review from all reviewers who have already submitted a review.
If reviewers are specified, the command will re-request review from the specified reviewers only.

**Aliases:** `rr`

**Options:**

- `--exclude-approved`: Exclude reviewers who have already approved (optional)
- `--expand-team`: Expand team reviewers to individual team members (optional)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--reviewers, -r`: Reviewers to re-request (optional, users or teams, e.g., username or org/team)

**Examples:**

```sh
# Re-request review from all reviewers who have already reviewed
gh review-kit rr 123

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
