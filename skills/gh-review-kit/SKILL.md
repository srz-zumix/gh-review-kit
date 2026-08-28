---
name: gh-review-kit
description: GitHub CLI extension (gh review-kit) for managing GitHub pull request reviews — including listing check runs with advanced filtering, displaying logs for failed checks, re-requesting reviews, building/analyzing normalized datasets of PR review feedback for large-scale review-comment mining, and embedding Git provenance metadata into video files.
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

The `attestation set` command does not call the GitHub API or require GitHub authentication, but it does require a local Git repository plus `ffmpeg` and `ffprobe` on `PATH`. The `attestation view` command only requires `ffprobe` on `PATH`.

## CLI Structure

```
gh review-kit                       # Root command
├── attestation                     # Embed Git provenance metadata into a video file
├── checks                          # Manage check runs for a pull request
│   ├── list                        # List check runs for a pull request
│   └── failure                     # Display logs for failed check runs
├── comments                        # Build and analyze datasets of PR review feedback
│   ├── estimate                    # Preflight extract: PR count, comment volume, API budget
│   ├── extract                     # Extract PR review feedback into a dataset
│   ├── validate                    # Validate a comments dataset
│   ├── stats                       # Aggregate counts over a comments dataset
│   ├── sample                      # Pick representative comments from a dataset
│   ├── bundle                      # Split a dataset into Agent-sized JSONL bundles
│   ├── suggest-rules               # Rank candidate coding rules / review viewpoints
│   └── report                      # Generate a Markdown/JSON report from a dataset
├── rerequest                       # Re-request review for a pull request
├── reviewed                        # Mark files in a pull request as viewed
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

## Embed Git Provenance Metadata into a Video (attestation set)

Collect Git information (commit, branch, dirty state, commit date, and repository) from a local Git repository and embed it as global metadata tags into a copy of a video file. FFmpeg stream-copies all media without transcoding, preserving existing streams, metadata, and chapters on a best-effort basis. Embedded tags are verified with `ffprobe`; a container that cannot retain custom metadata keys produces warnings rather than a failure.

This embeds unsigned provenance metadata only — it is not a cryptographic signature, GitHub artifact attestation, or tamper-proof claim. It does not call the GitHub API.

```bash
gh review-kit attestation set <input-video> -o OUTPUT [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--force` | Overwrite the output file if it already exists (default: false) |
| `--format` | Output format: `text`, `json` (default: `text`) |
| `-o`, `--output` | Output video file path (required) |
| `-C`, `--repo-dir` | Git repository directory to collect provenance from (default: current directory) |

### Embedded Tags

| Tag | Description |
| --- | --- |
| `git.author` | Identity of the user running the attestation command, in `Name <email>` format (from `git config user.name`/`user.email`) |
| `git.branch` | Current branch name, or `detached` when HEAD is not on any branch |
| `git.commit` | Full HEAD commit SHA |
| `git.commit_date` | HEAD commit's committer date in RFC 3339 format |
| `git.dirty` | `true` or `false`, based on tracked and untracked working tree changes |
| `git.repository` | Credential-free `host/owner/repo`, or the top-level directory name if no `origin` remote is configured |

### Examples

```bash
# Embed provenance from the current directory's repository
gh review-kit attestation set input.mp4 --output output.mp4

# Collect provenance from a different repository directory
gh review-kit attestation set input.mp4 --output output.mp4 -C /path/to/repo

# Overwrite an existing output file
gh review-kit attestation set input.mp4 --output output.mp4 --force
```

## Display Git Provenance Metadata Embedded in a Video (attestation view)

Read the global metadata tags previously embedded by `attestation set` using `ffprobe`, without modifying the file.

```bash
gh review-kit attestation view <input-video> [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--format` | Output format: `text`, `json` (default: `text`) |

### Examples

```bash
# Display provenance metadata embedded in a video
gh review-kit attestation view output.mp4
```

## List Check Runs (checks list)

List check runs for a pull request.

This command is similar to 'gh pr checks' but allows filtering by status and conclusion.
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

## Estimate API Work (comments estimate)

Preflight a future `comments extract`. Lists matching PRs, samples a few for averages, and reports projected total comments, projected API calls, and current rate-limit headroom. Use it before large runs to avoid hitting secondary rate limits.

```bash
gh review-kit comments estimate [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--comment-types` | Comment types to estimate (default: all). Allowed: `review_body`, `review_comment`, `issue_comment` |
| `--format` | Output format: `text`, `json` (default: `text`) |
| `--labels` | Include only PRs that have at least one of the given labels |
| `--limit` | Cap PR count to consider (default: 0 = no cap) |
| `--merged` | Include only merged pull requests (default: false) |
| `--repo, -R` | Repository in the format 'owner/repo' (default: current repository) |
| `--sample-size` | Number of PRs to sample for averages (default: 5) |
| `--since` | Only include PRs updated at or after this RFC3339 timestamp |
| `--state` | PR state filter: `open`, `closed`, `all` (default: `all`) |
| `--until` | Only include PRs created at or before this RFC3339 timestamp |

### Examples

```bash
# Quick estimate for the current repository
gh review-kit comments estimate

# Estimate a merged-only corpus with a larger sample for accuracy
gh review-kit comments estimate --repo owner/repo --merged --sample-size 20

# JSON for downstream tooling
gh review-kit comments estimate --repo owner/repo --format json
```

## Extract PR Review Feedback (comments extract)

Extract pull request review feedback (review bodies, inline review comments, and PR issue comments) into a normalized JSONL dataset directory. Subsequent commands operate on this directory.

The dataset directory contains:

- `corpus.jsonl`: one JSON record per comment
- `prs.jsonl`: one JSON record per PR included
- `manifest.json`: filter parameters and counts
- `checkpoint.json`: completed PR numbers, used to safely resume

Re-running with the same `--dataset` resumes from the checkpoint. Pass `--update` to re-fetch PRs whose `updated_at` advanced; their existing records are atomically replaced. Conservative secret/token redaction is applied by default; pass `--no-redact` to opt out.

```bash
gh review-kit comments extract --dataset DIR [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--comment-types` | Comment types to extract (default: all). Allowed: `review_body`, `review_comment`, `issue_comment` |
| `--dataset` | Dataset directory (required) |
| `--include-bots` | Include comments authored by bot users (default: false) |
| `--labels` | Include only PRs that have at least one of the given labels |
| `--limit` | Maximum number of new PRs to process this run (default: 0 = no limit) |
| `--merged` | Include only merged pull requests (default: false) |
| `--min-length` | Skip comments whose trimmed body is shorter than this many bytes (default: 0) |
| `--no-redact` | Disable conservative secret/token redaction (default: false) |
| `--path` | Restrict inline review comments to these path prefixes (repeatable) |
| `--repo, -R` | Repository in the format 'owner/repo' (default: current repository) |
| `--since` | Only include PRs updated at or after this RFC3339 timestamp |
| `--state` | PR state filter: `open`, `closed`, `all` (default: `all`) |
| `--until` | Only include PRs created at or before this RFC3339 timestamp |
| `--update` | Re-fetch PRs whose `updated_at` advanced since the last run (default: false) |

### Examples

```bash
# Extract all PR review feedback for a repository into ./dataset
gh review-kit comments extract --repo owner/repo --dataset ./dataset

# Resume an interrupted extraction (same --dataset)
gh review-kit comments extract --repo owner/repo --dataset ./dataset

# Refresh PRs whose updated_at advanced since the last run
gh review-kit comments extract --repo owner/repo --dataset ./dataset --update

# Only merged PRs updated since 2024-01-01, excluding bots
gh review-kit comments extract --repo owner/repo --dataset ./dataset \
  --merged --since 2024-01-01T00:00:00Z

# Only inline review comments under src/ with at least 20 bytes of body
gh review-kit comments extract --repo owner/repo --dataset ./dataset \
  --comment-types review_comment --path src/ --min-length 20
```

## Validate a Comments Dataset (comments validate)

Validate the schema and integrity of a comments dataset directory.

```bash
gh review-kit comments validate --dataset DIR [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--dataset` | Dataset directory (required) |
| `--format` | Output format: `text`, `json` (default: `text`) |
| `--strict` | Exit non-zero when any issue is reported (default: false) |

### Examples

```bash
# Print a human-readable validation report
gh review-kit comments validate --dataset ./dataset

# Fail with a non-zero exit code on any issue
gh review-kit comments validate --dataset ./dataset --strict

# Emit JSON for downstream tooling
gh review-kit comments validate --dataset ./dataset --format json
```

## Aggregate a Comments Dataset (comments stats)

Aggregate counts over a comments dataset and rank rows by frequency. Useful before LLM/Agent analysis to pick high-value slices.

```bash
gh review-kit comments stats --dataset DIR [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--dataset` | Dataset directory (required) |
| `--format` | Output format: `text`, `json` (default: `text`) |
| `--group-by` | Grouping key: `comment_type`, `repo`, `author`, `review_state`, `path_prefix`, `label` (default: `comment_type`) |
| `--min-count` | Drop rows with fewer than this many records (default: 0) |
| `--top` | Keep only the top N rows after sorting (default: 0 = keep all) |

### Examples

```bash
# Count records by type
gh review-kit comments stats --dataset ./dataset

# Top 20 reviewers by comment volume
gh review-kit comments stats --dataset ./dataset --group-by author --top 20

# Top path prefixes among inline comments, JSON output
gh review-kit comments stats --dataset ./dataset --group-by path_prefix --top 30 --format json
```

## Pick Representative Comments (comments sample)

Pick representative comments from a comments dataset. Filters narrow the corpus, records are grouped by `--group-by`, and `--strategy` decides which `--per-group` records are kept per group.

```bash
gh review-kit comments sample --dataset DIR [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--authors` | Filter by authors |
| `--comment-types` | Filter by comment types (review_body, review_comment, issue_comment) |
| `--dataset` | Dataset directory (required) |
| `--format` | Output format: `jsonl`, `json` (default: `jsonl`) |
| `--group-by` | Grouping key: `comment_type`, `repo`, `author`, `review_state`, `path_prefix`, `label` (empty = single group) |
| `--include-bots` | Include bot-authored comments (default: false) |
| `--min-length` | Minimum trimmed body length in bytes (default: 0) |
| `--output` | Output file path (default: stdout) |
| `--path` | Path prefixes for inline review comments (repeatable) |
| `--per-group` | Records to keep per group (default: 5) |
| `--review-states` | Filter by review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED) |
| `--seed` | Random seed when `--strategy=random` (default: 0 = time-based) |
| `--since` | Created at or after this RFC3339 timestamp |
| `--strategy` | Selection strategy: `recent`, `diverse-authors`, `blocking`, `random` (default: `recent`) |
| `--until` | Created at or before this RFC3339 timestamp |

> **Note:** `blocking` / `--review-states` rely on `review_state`, which is populated for both `review_body` and `review_comment` records. Datasets extracted before inline `review_state` support may lack it on `review_comment` records; re-extract affected PRs for accurate results.

### Examples

```bash
# 5 most recent comments overall
gh review-kit comments sample --dataset ./dataset

# 3 representative comments per author, JSONL to stdout
gh review-kit comments sample --dataset ./dataset --group-by author --per-group 3

# Only blocking review feedback (CHANGES_REQUESTED), 10 per repo
gh review-kit comments sample --dataset ./dataset \
  --group-by repo --per-group 10 --strategy blocking

# Deterministic random sample written to a file
gh review-kit comments sample --dataset ./dataset \
  --strategy random --seed 42 --per-group 50 --output ./samples.jsonl
```

## Split a Dataset into Bundles (comments bundle)

Split a comments dataset into smaller JSONL bundles for parallel LLM/Agent analysis. At least one of `--max-records` or `--max-bytes` must be set. A `manifest.json` next to the bundles records each file's group, record count, and byte size.

```bash
gh review-kit comments bundle --dataset DIR --output-dir DIR [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--authors` | Filter by authors |
| `--comment-types` | Filter by comment types (review_body, review_comment, issue_comment) |
| `--dataset` | Dataset directory (required) |
| `--format` | Summary format: `text`, `json` (default: `text`) |
| `--group-by` | Grouping key: `comment_type`, `repo`, `author`, `review_state`, `path_prefix`, `label` (empty = single stream) |
| `--include-bots` | Include bot-authored comments (default: false) |
| `--max-bytes` | Maximum bytes per bundle (default: 0 = no byte cap) |
| `--max-records` | Maximum records per bundle (default: 0 = no record cap) |
| `--min-length` | Minimum trimmed body length in bytes (default: 0) |
| `--output-dir` | Directory to write bundle files (required) |
| `--path` | Path prefixes for inline review comments (repeatable) |
| `--review-states` | Filter by review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED) |
| `--since` | Created at or after this RFC3339 timestamp |
| `--until` | Created at or before this RFC3339 timestamp |

### Examples

```bash
# 1000 records per bundle, single stream
gh review-kit comments bundle --dataset ./dataset --output-dir ./bundles --max-records 1000

# 500KB per bundle, grouped by repo so each Agent sees one repo at a time
gh review-kit comments bundle --dataset ./dataset --output-dir ./bundles \
  --max-bytes 500000 --group-by repo

# Only blocking review feedback, grouped by path prefix
gh review-kit comments bundle --dataset ./dataset --output-dir ./bundles \
  --max-records 200 --group-by path_prefix --review-states CHANGES_REQUESTED
```

## Rank Candidate Rules (comments suggest-rules)

Rank deterministic candidate coding rules and review viewpoints. Topic detection is regex/keyword based, with built-in defaults and an optional JSON dictionary via `--topics-file`. Each candidate carries frequency, distinct reviewer/repo counts, blocking share, and evidence URLs.

```bash
gh review-kit comments suggest-rules --dataset DIR [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--comment-types` | Filter by comment types |
| `--dataset` | Dataset directory (required) |
| `--examples` | Number of evidence examples per topic (default: 3) |
| `--format` | Output format: `text`, `json`, `markdown` (default: `text`) |
| `--include-bots` | Include bot-authored comments (default: false) |
| `--min-count` | Drop topics matched fewer than this many times (default: 3) |
| `--min-length` | Minimum trimmed body length in bytes (default: 0) |
| `--min-reviewers` | Drop topics matched by fewer than this many distinct reviewers (default: 2) |
| `--output` | Output file path (default: stdout) |
| `--path` | Path prefixes for inline review comments (repeatable) |
| `--review-states` | Filter by review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED) |
| `--since` | Created at or after this RFC3339 timestamp |
| `--topics-file` | JSON dictionary of topics (default: built-in) |
| `--until` | Created at or before this RFC3339 timestamp |

### Examples

```bash
# Rank candidates with the built-in dictionary
gh review-kit comments suggest-rules --dataset ./dataset

# Use a custom topics file and emit JSON
gh review-kit comments suggest-rules --dataset ./dataset \
  --topics-file ./topics.json --format json --output ./candidates.json

# Only blocking review comments, require 3 distinct reviewers
gh review-kit comments suggest-rules --dataset ./dataset \
  --review-states CHANGES_REQUESTED --min-reviewers 3
```

## Generate a Report (comments report)

Generate a deterministic Markdown or JSON report combining aggregate stats and rule candidates with the dataset manifest summary.

```bash
gh review-kit comments report --dataset DIR [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--comment-types` | Filter by comment types |
| `--dataset` | Dataset directory (required) |
| `--examples` | Number of evidence examples per topic (default: 3) |
| `--format` | Output format: `markdown`, `json` (default: `markdown`) |
| `--include-bots` | Include bot-authored comments (default: false) |
| `--min-count` | Drop topics matched fewer than this many times (default: 3) |
| `--min-length` | Minimum trimmed body length in bytes (default: 0) |
| `--min-reviewers` | Drop topics matched by fewer than this many distinct reviewers (default: 2) |
| `--output` | Output file path (default: stdout) |
| `--path` | Path prefixes for inline review comments (repeatable) |
| `--review-states` | Filter by review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED) |
| `--since` | Created at or after this RFC3339 timestamp |
| `--stats-top` | Top N rows per stats slice (default: 20) |
| `--topics-file` | JSON dictionary of topics (default: built-in) |
| `--until` | Created at or before this RFC3339 timestamp |

### Examples

```bash
# Markdown report to stdout
gh review-kit comments report --dataset ./dataset

# Save Markdown and JSON reports
gh review-kit comments report --dataset ./dataset --output ./report.md
gh review-kit comments report --dataset ./dataset --format json --output ./report.json

# Focus on blocking review feedback from the last 6 months
gh review-kit comments report --dataset ./dataset \
  --review-states CHANGES_REQUESTED --since 2025-10-01T00:00:00Z
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

# 4. Extract small evidence sets for an Agent
gh review-kit comments sample --dataset ./review-corpus \
  --group-by path_prefix --per-group 5 --strategy blocking --output ./evidence.jsonl

# 5. Split the corpus into Agent-sized bundles for parallel analysis
gh review-kit comments bundle --dataset ./review-corpus --output-dir ./bundles \
  --max-records 1000 --group-by repo

# 6. Rank candidate coding rules / review viewpoints
gh review-kit comments suggest-rules --dataset ./review-corpus \
  --review-states CHANGES_REQUESTED --min-reviewers 3 --format json --output ./candidates.json

# 7. Produce a Markdown report for human review
gh review-kit comments report --dataset ./review-corpus --output ./review-report.md

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

## Mark Files as Viewed (reviewed)

Mark files in a pull request as viewed using the GitHub `markFileAsViewed` API.

If file paths are specified as arguments, only those files will be marked as viewed.
If no file paths are specified, all files marked as `linguist-generated` in the repository's `.gitattributes` will be marked as viewed.

```bash
gh review-kit reviewed [file...] [flags]
```

### Options

| Flag | Description |
| --- | --- |
| `--pr` | Pull request number, URL, or branch name (default: current branch) |
| `--repo, -R` | Repository in the format 'owner/repo' (default: current repository) |

### Examples

```bash
# Mark all linguist-generated files as viewed for current branch
gh review-kit reviewed

# Mark all linguist-generated files as viewed for a specific PR
gh review-kit reviewed --pr 123

# Mark specific files as viewed
gh review-kit reviewed path/to/generated_file.go another/file.go

# Mark specific files as viewed for a specific PR
gh review-kit reviewed --pr 123 path/to/generated_file.go

# Mark files as viewed in a different repository
gh review-kit reviewed --repo owner/repo --pr 123
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

### Skip Reviewing Auto-generated Files

```bash
# Mark all linguist-generated files as viewed (reads .gitattributes)
gh review-kit reviewed --pr 123

# Mark specific generated files as viewed
gh review-kit reviewed --pr 123 docs/generated/api.go
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

### Mine Review Comments at Scale

Build a reproducible dataset of historical review feedback, then summarize it before handing slices to an LLM/Agent. This pipeline is designed for repositories with thousands of PRs and supports safe resume after rate limits or interruptions.

```bash
# 0. Preflight: estimate API work and rate-limit headroom
gh review-kit comments estimate --repo owner/repo --merged

# 1. Extract review feedback into a dataset directory
gh review-kit comments extract --repo owner/repo --dataset ./review-corpus

# 2. Validate the dataset before analysis
gh review-kit comments validate --dataset ./review-corpus --strict

# 3. Pick high-value slices instead of reading every record
gh review-kit comments stats --dataset ./review-corpus --group-by review_state
gh review-kit comments stats --dataset ./review-corpus --group-by author --top 20
gh review-kit comments stats --dataset ./review-corpus --group-by path_prefix --top 30 --format json
```

## References

- Repository: https://github.com/srz-zumix/gh-review-kit
