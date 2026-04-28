---
name: gh-review-kit
description: GitHub CLI extension (gh review-kit) for managing GitHub pull request reviews — including listing check runs with advanced filtering, displaying logs for failed checks, and re-requesting reviews from specific or all reviewers.
---

# gh-review-kit

GitHub CLI extension for managing GitHub pull request reviews from the command line.

## Prerequisites

### Installation

```bash
# Install the extension
gh extension install srz-zumix/gh-review-kit

# Verify installation
gh review-kit --version
```

### Authentication

```bash
# Login to GitHub (if not already authenticated)
gh auth login
```

## CLI Structure

```
gh review-kit                       # Root command
├── checks                          # Manage check runs for a pull request
│   ├── list                        # List check runs for a pull request
│   └── failure                     # Display logs for failed check runs
├── rerequest                       # Re-request review for a pull request
├── skills                          # Manage agent skills
└── completion                      # Shell completion
```

## Global Options

| Flag | Description |
| --- | --- |
| `--read-only` | Run in read-only mode (prevent write operations) |
| `--log-level, -L` | Set log level: debug, info, warn, error (default: info) |
| `--help, -h` | Show help for command |
| `--version` | Show version |

## List Check Runs (checks list)

List check runs for a pull request.

This command is similar to 'gh pr checks' but allows filtering by status.
You can also output run IDs and job IDs for use with 'gh run view'.

**Aliases:** `ls`, `cc`, `check-checks`

```bash
gh review-kit checks list [pull-request-identifier] [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--all` | Show all check runs, including those without a conclusion (default: false) |
| `--color` | Color output: always, never, auto (default: auto) |
| `--conclusion, -c` | Filter by conclusion: success, failure, neutral, cancelled, skipped, timed_out, action_required |
| `--details, -d` | Show detailed information (status icon, run ID, job ID, timestamps, URLs) (default: false) |
| `--headers, -H` | Columns to display (NAME, STATUS, CONCLUSION, RUN_ID, JOB_ID, STARTED_AT, ELAPSED, DETAILS_URL, etc.) |
| `--no-required` | Show only non-required check runs |
| `--repo, -R` | Repository in the format 'owner/repo' (default: current repository) |
| `--required` | Show only required check runs |
| `--status, -s` | Filter by status: queued, in_progress, completed |

### Examples

```bash
# List check runs for current branch
gh review-kit checks list

# List check runs by PR number
gh review-kit checks list 123

# List check runs by PR URL
gh review-kit checks list https://github.com/owner/repo/pull/123

# List check runs by branch name
gh review-kit checks list feature/my-branch

# List only completed check runs
gh review-kit checks list 123 --status completed

# List only failed check runs
gh review-kit checks list 123 --conclusion failure

# List with detailed information
gh review-kit checks list 123 --details

# List with custom columns
gh review-kit checks list 123 --headers NAME,STATUS,CONCLUSION,RUN_ID,JOB_ID

# List only required check runs
gh review-kit checks list 123 --required

# List check runs in a different repository
gh review-kit checks list 123 --repo owner/repo

# Using alias
gh review-kit checks cc 123
```

## Display Logs for Failed Check Runs (checks failure)

Retrieve and display logs for all check runs with 'failure' conclusion in a pull request.

**Aliases:** `ff`, `fail`

```bash
gh review-kit checks failure [pull-request-identifier] [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--full` | Display full logs instead of only failed step logs (default: false) |
| `--no-required` | Show only non-required check runs |
| `--repo, -R` | Repository in the format 'owner/repo' (default: current repository) |
| `--required` | Show only required check runs |

### Examples

```bash
# Display logs for failed check runs on current branch
gh review-kit checks failure

# Display logs for failed check runs by PR number
gh review-kit checks failure 123

# Display logs by PR URL
gh review-kit checks failure https://github.com/owner/repo/pull/123

# Display logs by branch name
gh review-kit checks failure feature/my-branch

# Display full logs for failed check runs
gh review-kit checks failure 123 --full

# Display logs for only required failed check runs
gh review-kit checks failure 123 --required

# Display logs for only non-required failed check runs
gh review-kit checks failure 123 --no-required

# Display logs in a different repository
gh review-kit checks failure 123 --repo owner/repo

# Using alias
gh review-kit checks ff 123
```

## Re-request Review (rerequest)

Re-request review for a pull request.

If reviewers are not specified, the command will re-request review from all reviewers who have already submitted a review.
If reviewers are specified, the command will re-request review from the specified reviewers only.

Reviewers can be specified as:
- Individual users: username
- Team reviewers: org/team-slug
- With @ prefix: @username or @org/team-slug

When --expand-team is specified, team reviewers will be expanded to individual team members.

**Aliases:** `rr`

```bash
gh review-kit rerequest [pull-request-identifier] [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--exclude-approved` | Exclude reviewers who have already approved |
| `--expand-team` | Expand team reviewers to individual team members |
| `--repo, -R` | Repository in the format 'owner/repo' (default: current repository) |
| `--reviewers, -r` | Reviewers to re-request (users or teams, e.g., username or org/team) |

### Examples

```bash
# Re-request review for current branch from all reviewers who have already reviewed
gh review-kit rerequest

# Re-request review by PR number
gh review-kit rerequest 123

# Re-request review by PR URL
gh review-kit rerequest https://github.com/owner/repo/pull/123

# Re-request review by branch name
gh review-kit rerequest feature/my-branch

# Re-request review excluding approved reviewers
gh review-kit rerequest 123 --exclude-approved

# Re-request review from specific reviewers
gh review-kit rerequest 123 --reviewers user1,user2,@org/team

# Re-request review from specific reviewers, excluding approved
gh review-kit rerequest 123 --reviewers user1,user2,user3 --exclude-approved

# Expand team reviewers to individual members
gh review-kit rerequest 123 --reviewers @org/team --expand-team

# Re-request review in a different repository
gh review-kit rerequest 123 --repo owner/repo

# Using alias
gh review-kit rr 123
```

## Pull Request Identifier

All commands accept a pull request identifier in these formats:

- **PR number:** `123` or `#123`
- **PR URL:** `https://github.com/owner/repo/pull/123`
- **Branch name:** `feature/my-branch`
- **Omitted:** Uses the current branch

## Common Workflows

### Investigate CI Failures

```bash
# Check which runs failed
gh review-kit checks list 123 --conclusion failure

# View failed step logs
gh review-kit checks failure 123

# View full logs for deeper investigation
gh review-kit checks failure 123 --full
```

### Re-request Reviews After Fixing Issues

```bash
# Fix code issues, push changes, then re-request all reviewers
gh review-kit rr 123

# Re-request only reviewers who haven't approved yet
gh review-kit rr 123 --exclude-approved
```

### Monitor Required Checks

```bash
# List only required check runs
gh review-kit checks list 123 --required

# View detailed info for required checks
gh review-kit checks list 123 --required --details

# View logs for required failed checks only
gh review-kit checks failure 123 --required
```

## References

- Repository: https://github.com/srz-zumix/gh-review-kit
