# gh-review-kit

A tool to manage GitHub reviews.

## Installation

```sh
gh extension install srz-zumix/gh-review-kit
```

## Shell Completion

**Workaround Available!** While gh CLI doesn't natively support extension completion, we provide a patch script that enables it.

**Prerequisites:** Before setting up gh-review-kit completion, ensure gh CLI completion is configured for your shell. See [gh completion documentation](https://cli.github.com/manual/gh_completion) for setup instructions.

For detailed installation instructions and setup for each shell, see the [Shell Completion Guide](https://github.com/srz-zumix/go-gh-extension/blob/main/docs/shell-completion.md).

## Agent Skills

gh-review-kit bundles agent skills for AI. Use the `skills` subcommand to install and manage them.

```sh
gh review-kit skills [subcommand] [args...]
```

For details, see [Songmu/skillsmith](https://github.com/Songmu/skillsmith).

## Global Options

The following options are available for all commands:

- `--read-only`: Run in read-only mode (prevent write operations). This flag is useful for AI agents or CI/CD environments to ensure no modifications are made to GitHub resources.
- `--log-level, -L`: Set log level: debug, info, warn, error (optional, default: info)

**Example:**

```sh
# Run in read-only mode
gh review-kit rerequest 123 --read-only
```

## Commands

### attestation

#### Embed Git provenance metadata into a video or image

```sh
# Local file mode
gh review-kit attestation set <input-file> -o OUTPUT [-C DIR | --repo-dir DIR] [--comment TEXT] [--force] [--format FORMAT]
# Pull request / issue attachment mode
gh review-kit attestation set (--pr PR | --issue ISSUE) [<asset-url> [-o OUTPUT]] [-R REPO] [--max-asset-size N] [-C DIR | --repo-dir DIR] [--comment TEXT] [--force] [--format FORMAT]
```

Collect Git information (commit, branch, dirty state, commit date, and repository) from a local Git repository and embed it as metadata tags into a copy of the input file, together with an optional freeform comment (`--comment`). For video files, FFmpeg stream-copies all media without transcoding, preserving existing streams, metadata, and chapters on a best-effort basis, and the embedded tags are verified with `ffprobe` before the output file is written; a container that cannot retain custom metadata keys produces warnings rather than a failure. For PNG and JPEG files, tags are embedded natively (PNG `iTXt` chunks (UTF-8 text) or JPEG COM segments) without invoking FFmpeg.

Two kinds of input are supported:

- `<input-file>`: embed metadata into a local file and write the result to `--output`, which is required in this mode.
- `--pr` or `--issue`: re-embed metadata into files already attached to a pull request or issue. Each attachment is downloaded, re-embedded, uploaded again through GitHub's user-attachments endpoint, and every link to it in the target's body and comments is rewritten to the new URL. Attachments that already carry provenance metadata are left untouched, so re-running the command does not replace working links. Passing an `<asset-url>` argument as well limits the run to that single attachment. `--output` is optional in this mode and, when given, also keeps a local copy of the single re-embedded attachment. GitHub offers no API to delete the originals, so they remain reachable at their old URLs, and uploading is unavailable on GitHub Enterprise Server. Attachments whose type or size the upload endpoint does not accept are skipped rather than causing an error.

This embeds unsigned provenance metadata only. It is not a cryptographic signature, GitHub artifact attestation, or tamper-proof claim.

Requires `ffmpeg` and `ffprobe` to be available on `PATH` for video files; PNG and JPEG files have no external tool dependency.

**Embedded tags:**

| Tag | Description |
| --- | --- |
| `attestation.comment` | Freeform comment supplied via `--comment` (only present when `--comment` is given) |
| `git.author` | Identity of the user running the attestation command, in `Name <email>` format (from `git config user.name`/`user.email`) |
| `git.branch` | Current branch name, or `detached` when HEAD is not on any branch |
| `git.commit` | Full HEAD commit SHA |
| `git.commit_date` | HEAD commit's committer date in RFC 3339 format |
| `git.dirty` | `true` or `false`, based on tracked and untracked working tree changes |
| `git.repository` | Credential-free `host/owner/repo`, or the top-level directory name if no `origin` remote is configured |

**Options:**

- `--comment`: Freeform comment to embed alongside the Git provenance tags (optional, default: none)
- `--force`: Overwrite the output file if it already exists (optional, default: false)
- `--format`: Output format: `text`, `json` (optional, default: `text`); in `--pr`/`--issue` mode each asset is a block starting with a `<filename> (<location>)` header, followed by `old_url=`/`new_url=` and its tags, or `skipped=<reason>` / `error=<message>`
- `--issue`: Re-embed and re-upload the attachments of an issue, rewriting its links (number or URL; optional, mutually exclusive with `--pr`)
- `--max-asset-size`: In `--pr`/`--issue` mode, skip attachments whose server-reported size exceeds this many bytes instead of downloading them (optional, default: `0` = no limit)
- `-o`, `--output`: Output file path (required for `<input-file>`, optional for a single `<asset-url>`)
- `--pr`: Re-embed and re-upload the attachments of a pull request, rewriting its links (number, URL, or branch name; optional, mutually exclusive with `--issue`)
- `-R`, `--repo`: Repository for GitHub authentication and asset uploads, `[HOST/]OWNER/REPO` (optional, default: current repository, or derived from `--pr`/`--issue`)
- `-C`, `--repo-dir`: Git repository directory to collect provenance from (optional, default: current directory)

**Examples:**

```sh
# Embed provenance from the current directory's repository
gh review-kit attestation set input.mp4 --output output.mp4

# Collect provenance from a different repository directory
gh review-kit attestation set input.mp4 --output output.mp4 -C /path/to/repo

# Overwrite an existing output file
gh review-kit attestation set input.mp4 --output output.mp4 --force

# Embed provenance into a PNG or JPEG image (no ffmpeg required)
gh review-kit attestation set input.png --output output.png

# Embed provenance together with a freeform comment
gh review-kit attestation set input.mp4 --output output.mp4 --comment "pre-release build"

# Re-embed every attachment of a pull request and rewrite its links
gh review-kit attestation set --pr 123

# Re-embed a single attachment of an issue and keep a local copy
gh review-kit attestation set https://github.com/user-attachments/assets/00000000-0000-0000-0000-000000000000 --issue 456 --output local.png
```

#### Display Git provenance metadata embedded in a video or image

```sh
gh review-kit attestation view [<input-file> | <asset-url>] [--pr PR] [-R REPO] [--max-asset-size N] [--format FORMAT]
```

Read the metadata tags previously embedded by `attestation set`, without modifying the file. Video files are probed with `ffprobe`; PNG and JPEG files are read natively. Supports three mutually exclusive modes: a local file path, a GitHub-hosted asset URL (e.g. a file pasted into a pull request), or `--pr` to scan a pull request's body, issue comments, and review comments for GitHub-hosted asset URLs and read metadata from each one found. In `--pr` mode, assets with no embedded attestation are listed with a "no attestation found" note rather than causing an error.

Requires `ffprobe` to be available on `PATH` for video files; PNG and JPEG files have no external tool dependency.

**Options:**

- `--format`: Output format: `text`, `json` (optional, default: `text`); `text` renders `key=value` lines. In `--pr` mode each asset is a block starting with a `<filename> (<location>)` header, followed by `location_url=<url>` linking to the comment (or the pull request itself for the body), and then its tags, `no attestation found`, or `error=<message>`
- `--max-asset-size`: In `--pr` mode, skip assets whose server-reported size exceeds this many bytes instead of downloading them (optional, default: `0` = no limit)
- `--pr`: Scan a pull request's attachments for Git provenance metadata (number, URL, or branch name; optional, mutually exclusive with `<input-file>`/`<asset-url>`)
- `-R`, `--repo`: Repository for GitHub authentication (`--pr` API access and asset downloads), `[HOST/]OWNER/REPO` (optional, default: current repository, or derived from `--pr`/the asset URL)

**Examples:**

```sh
# Display provenance metadata embedded in a video
gh review-kit attestation view output.mp4

# Display provenance metadata for a file pasted into a pull request
gh review-kit attestation view https://github.com/user-attachments/assets/00000000-0000-0000-0000-000000000000

# Scan all attachments in a pull request for provenance metadata
gh review-kit attestation view --pr 123

# Scan a pull request in a different repository, as JSON
gh review-kit attestation view --pr 123 -R owner/repo --format json
```

### checks

#### List check runs for a pull request

```sh
gh review-kit checks list [pull-request-identifier] [--repo REPO] [--status STATUS] [--conclusion CONCLUSION] [--headers HEADERS] [--all] [--required|--no-required] [--details] [--color COLOR]
```

List check runs for a pull request.

This command is similar to `gh pr checks` but also supports filtering by status, conclusion, and required check state.
It can also control output with options such as `--all`, `--headers`, `--details`, and `--color`, and can show run IDs and job IDs for use with `gh run view`.

The pull request can be specified by:

- PR number (e.g., `123` or `#123`)
- PR URL (e.g., `https://github.com/owner/repo/pull/123`)
- Branch name (e.g., `feature/my-branch`)
- If omitted, uses the current branch

**Aliases:** `ls`, `cc`, `check-checks`

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
gh review-kit checks list 123 --repo owner-name/repo-name
```

#### Display logs for failed check runs

```sh
gh review-kit checks failure [pull-request-identifier] [--repo REPO] [--full] [--required|--no-required]
```

Display logs for failed check runs in a pull request.

This command retrieves all check runs with 'failure' conclusion and displays their logs.

The pull request can be specified by:

- PR number (e.g., `123` or `#123`)
- PR URL (e.g., `https://github.com/owner/repo/pull/123`)
- Branch name (e.g., `feature/my-branch`)
- If omitted, uses the current branch

**Aliases:** `ff`, `fail`

**Options:**

- `--full`: Display full logs instead of only failed step logs (optional, default: false)
- `--no-required`: Show only non-required check runs (optional)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--required`: Show only required check runs (optional)

**Examples:**

```sh
# Display logs for current branch
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

# Display logs for failed check runs in a different repository
gh review-kit checks failure 123 --repo owner-name/repo-name
```

### comments

#### Preflight a comments extract: PR count, comment volume, API budget

```sh
gh review-kit comments estimate [--repo REPO] [--state STATE] [--merged] [--since RFC3339] [--until RFC3339] [--labels LABELS] [--comment-types TYPES] [--limit N] [--sample-size N] [--format FORMAT]
```

Estimate how much GitHub API work a future `comments extract` run with the same filters would consume. The command lists matching pull requests (cheap REST pagination), samples a small number of them to measure average comment volume per PR, and reports the projected total comments, projected API calls, and current rate-limit headroom. Use it before kicking off a large extraction to avoid hitting secondary rate limits or running out of REST quota mid-run.

**Options:**

- `--comment-types`: Comment types to estimate (optional, default: all). Allowed: `review_body`, `review_comment`, `issue_comment`
- `--format`: Output format: `text`, `json` (optional, default: `text`)
- `--labels`: Include only PRs that have at least one of the given labels (optional)
- `--limit`: Cap PR count to consider (optional, default: 0 = no cap)
- `--merged`: Include only merged pull requests (optional, default: false)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--sample-size`: Number of PRs to sample for averages (optional, default: 5)
- `--since`: Only include PRs updated at or after this RFC3339 timestamp (optional)
- `--state`: PR state filter: `open`, `closed`, `all` (optional, default: `all`)
- `--until`: Only include PRs created at or before this RFC3339 timestamp (optional)

**Examples:**

```sh
# Quick estimate for the current repository
gh review-kit comments estimate

# Estimate a large repo's merged-only corpus, sampling 20 PRs for accuracy
gh review-kit comments estimate --repo owner/repo --merged --sample-size 20

# JSON for downstream tooling
gh review-kit comments estimate --repo owner/repo --format json
```

#### Extract PR review feedback into a dataset

```sh
gh review-kit comments extract --dataset DIR [--repo REPO] [--state STATE] [--merged] [--since RFC3339] [--until RFC3339] [--labels LABELS] [--comment-types TYPES] [--include-bots] [--min-length N] [--path PREFIX] [--limit N] [--no-redact] [--update]
```

Extract pull request review feedback (review bodies, inline review comments, and PR issue comments) into a normalized JSONL dataset directory.

The dataset directory is the unit shared by every `comments` subcommand and contains:

- `corpus.jsonl`: one JSON record per comment
- `prs.jsonl`: one JSON record per PR included in the dataset
- `manifest.json`: filter parameters and running counts
- `checkpoint.json`: completed PR numbers used to resume safely

Re-running with the same `--dataset` resumes from the checkpoint and skips PRs already recorded. Pass `--update` to additionally re-fetch PRs whose `updated_at` advanced since the last run; their existing PR and comment records are atomically replaced. Conservative secret/token redaction is applied by default; pass `--no-redact` to opt out.

**Options:**

- `--comment-types`: Comment types to extract (optional, default: all). Allowed: `review_body`, `review_comment`, `issue_comment`
- `--dataset`: Dataset directory (required)
- `--include-bots`: Include comments authored by bot users (optional, default: false)
- `--labels`: Include only PRs that have at least one of the given labels (optional)
- `--limit`: Maximum number of new PRs to process this run (optional, default: 0 = no limit)
- `--merged`: Include only merged pull requests (optional, default: false)
- `--min-length`: Skip comments whose trimmed body is shorter than this many bytes (optional, default: 0)
- `--no-redact`: Disable conservative secret/token redaction (optional, default: false)
- `--path`: Restrict inline review comments to these path prefixes, repeatable (optional)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--since`: Only include PRs updated at or after this RFC3339 timestamp (optional)
- `--state`: PR state filter: `open`, `closed`, `all` (optional, default: `all`)
- `--until`: Only include PRs created at or before this RFC3339 timestamp (optional)
- `--update`: Re-fetch PRs whose `updated_at` advanced since the last run (optional, default: false)

**Examples:**

```sh
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

#### Validate a comments dataset

```sh
gh review-kit comments validate --dataset DIR [--strict] [--format FORMAT]
```

Validate the schema and integrity of a comments dataset directory. Checks include schema version, required fields, duplicate IDs, and PR/comment linkage.

**Options:**

- `--dataset`: Dataset directory (required)
- `--format`: Output format: `text`, `json` (optional, default: `text`)
- `--strict`: Exit non-zero when any issue is reported (optional, default: false)

**Examples:**

```sh
# Print a human-readable validation report
gh review-kit comments validate --dataset ./dataset

# Fail with a non-zero exit code on any issue
gh review-kit comments validate --dataset ./dataset --strict

# Emit JSON for downstream tooling
gh review-kit comments validate --dataset ./dataset --format json
```

#### Aggregate counts over a comments dataset

```sh
gh review-kit comments stats --dataset DIR [--group-by KEY] [--top N] [--min-count N] [--format FORMAT]
```

Aggregate counts over a comments dataset and rank rows by frequency. Useful before LLM/Agent analysis to pick high-value slices instead of reading every record.

**Options:**

- `--dataset`: Dataset directory (required)
- `--format`: Output format: `text`, `json` (optional, default: `text`)
- `--group-by`: Grouping key: `comment_type`, `repo`, `author`, `review_state`, `path_prefix`, `label` (optional, default: `comment_type`)
- `--min-count`: Drop rows with fewer than this many records (optional, default: 0)
- `--top`: Keep only the top N rows after sorting (optional, default: 0 = keep all)

**Examples:**

```sh
# Count records by type
gh review-kit comments stats --dataset ./dataset

# Top 20 reviewers by comment volume
gh review-kit comments stats --dataset ./dataset --group-by author --top 20

# Top path prefixes among inline comments, JSON output
gh review-kit comments stats --dataset ./dataset --group-by path_prefix --top 30 --format json
```

#### Pick representative comments from a dataset

```sh
gh review-kit comments sample --dataset DIR [--group-by KEY] [--per-group N] [--strategy STRATEGY] [--seed N] [--output FILE] [--format FORMAT] [--comment-types TYPES] [--review-states STATES] [--authors AUTHORS] [--path PREFIX] [--since RFC3339] [--until RFC3339] [--min-length N] [--include-bots]
```

Pick representative comments from a comments dataset. Filters narrow the corpus, records are grouped by `--group-by`, and `--strategy` decides which `--per-group` records are kept per group. Useful for handing a small evidence set to an LLM/Agent.

Strategies:

- `recent`: newest first by `created_at` (default)
- `diverse-authors`: newest record per distinct author until N
- `blocking`: only `review_state=CHANGES_REQUESTED`, then recent
- `random`: random with `--seed` (deterministic when seeded)

> **Note:** `review_state` is populated for `review_body` and `review_comment` records. Datasets extracted before inline `review_state` support may lack it on `review_comment` records; re-extract (recreate or purge affected PRs) for accurate `blocking` / `--review-states` results.

**Options:**

- `--authors`: Filter by authors (optional)
- `--comment-types`: Filter by comment types (optional). Allowed: `review_body`, `review_comment`, `issue_comment`
- `--dataset`: Dataset directory (required)
- `--format`: Output format: `jsonl`, `json` (optional, default: `jsonl`)
- `--group-by`: Grouping key (optional, default: empty = single group). Allowed: `comment_type`, `repo`, `author`, `review_state`, `path_prefix`, `label`
- `--include-bots`: Include bot-authored comments (optional, default: false)
- `--min-length`: Minimum trimmed body length in bytes (optional, default: 0)
- `--output`: Output file path (optional, default: stdout)
- `--path`: Path prefixes for inline review comments, repeatable (optional)
- `--per-group`: Records to keep per group (optional, default: 5)
- `--review-states`: Filter by review states (optional). Allowed: `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED`
- `--seed`: Random seed when `--strategy=random` (optional, default: 0 = time-based)
- `--since`: Created at or after this RFC3339 timestamp (optional)
- `--strategy`: Selection strategy (optional, default: `recent`). Allowed: `recent`, `diverse-authors`, `blocking`, `random`
- `--until`: Created at or before this RFC3339 timestamp (optional)

**Examples:**

```sh
# 5 most recent comments overall
gh review-kit comments sample --dataset ./dataset

# 3 representative comments per author, JSONL to stdout
gh review-kit comments sample --dataset ./dataset --group-by author --per-group 3

# Only blocking review feedback (CHANGES_REQUESTED), 10 per repo
gh review-kit comments sample --dataset ./dataset \
  --group-by repo --per-group 10 --strategy blocking

# Diverse authors per path prefix under src/
gh review-kit comments sample --dataset ./dataset \
  --group-by path_prefix --per-group 5 --strategy diverse-authors --path src/

# Deterministic random sample written to a file
gh review-kit comments sample --dataset ./dataset \
  --strategy random --seed 42 --per-group 50 --output ./samples.jsonl
```

#### Split a dataset into Agent-sized JSONL bundles

```sh
gh review-kit comments bundle --dataset DIR --output-dir DIR [--group-by KEY] [--max-records N] [--max-bytes N] [--comment-types TYPES] [--review-states STATES] [--authors AUTHORS] [--path PREFIX] [--since RFC3339] [--until RFC3339] [--min-length N] [--include-bots] [--format FORMAT]
```

Split a comments dataset into smaller JSONL bundles for parallel LLM/Agent analysis. Bundles are capped by `--max-records` and/or `--max-bytes`. A `manifest.json` next to the bundles records each file's group, record count, and byte size.

**Options:**

- `--authors`: Filter by authors (optional)
- `--comment-types`: Filter by comment types (optional). Allowed: `review_body`, `review_comment`, `issue_comment`
- `--dataset`: Dataset directory (required)
- `--format`: Summary format: `text`, `json` (optional, default: `text`)
- `--group-by`: Grouping key (optional, default: empty = single stream). Allowed: `comment_type`, `repo`, `author`, `review_state`, `path_prefix`, `label`
- `--include-bots`: Include bot-authored comments (optional, default: false)
- `--max-bytes`: Maximum bytes per bundle (optional, default: 0 = no byte cap)
- `--max-records`: Maximum records per bundle (optional, default: 0 = no record cap)
- `--min-length`: Minimum trimmed body length in bytes (optional, default: 0)
- `--output-dir`: Directory to write bundle files (required)
- `--path`: Path prefixes for inline review comments, repeatable (optional)
- `--review-states`: Filter by review states (optional). Allowed: `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED`
- `--since`: Created at or after this RFC3339 timestamp (optional)
- `--until`: Created at or before this RFC3339 timestamp (optional)

At least one of `--max-records` or `--max-bytes` must be set.

**Examples:**

```sh
# 1000 records per bundle, single stream
gh review-kit comments bundle --dataset ./dataset --output-dir ./bundles --max-records 1000

# 500KB per bundle, grouped by repo so each Agent sees one repo at a time
gh review-kit comments bundle --dataset ./dataset --output-dir ./bundles \
  --max-bytes 500000 --group-by repo

# Only blocking review feedback, grouped by path prefix
gh review-kit comments bundle --dataset ./dataset --output-dir ./bundles \
  --max-records 200 --group-by path_prefix --review-states CHANGES_REQUESTED
```

#### Rank candidate coding rules and review viewpoints

```sh
gh review-kit comments suggest-rules --dataset DIR [--topics-file FILE] [--min-count N] [--min-reviewers N] [--examples N] [--output FILE] [--format FORMAT] [--comment-types TYPES] [--review-states STATES] [--path PREFIX] [--since RFC3339] [--until RFC3339] [--min-length N] [--include-bots]
```

Rank deterministic candidate coding rules and review viewpoints inferred from the dataset.

Topic detection is regex/keyword based and case-insensitive. Use the built-in dictionary that covers common review areas (naming, error handling, tests, security, performance, concurrency, style, logging, API design, comments and docs), or supply your own JSON dictionary via `--topics-file`. Each candidate is reported with frequency, distinct reviewers, distinct repos, blocking (`CHANGES_REQUESTED`) share, latest occurrence, and evidence URLs. No fuzzy clustering or embeddings are used; the output is reproducible.

A topics file looks like:

```json
{
  "topics": [
    {
      "name": "logging",
      "description": "Structured logging conventions",
      "patterns": ["\\bstructured log\\b", "\\blog level\\b"]
    }
  ]
}
```

**Options:**

- `--comment-types`: Filter by comment types (optional)
- `--dataset`: Dataset directory (required)
- `--examples`: Number of evidence examples to include per topic (optional, default: 3)
- `--format`: Output format: `text`, `json`, `markdown` (optional, default: `text`)
- `--include-bots`: Include bot-authored comments (optional, default: false)
- `--min-count`: Drop topics matched fewer than this many times (optional, default: 3)
- `--min-length`: Minimum trimmed body length in bytes (optional, default: 0)
- `--min-reviewers`: Drop topics matched by fewer than this many distinct reviewers (optional, default: 2)
- `--output`: Output file path (optional, default: stdout)
- `--path`: Path prefixes for inline review comments, repeatable (optional)
- `--review-states`: Filter by review states (optional)
- `--since`: Created at or after this RFC3339 timestamp (optional)
- `--topics-file`: JSON dictionary of topics (optional, default: built-in)
- `--until`: Created at or before this RFC3339 timestamp (optional)

**Examples:**

```sh
# Rank candidates with the built-in dictionary
gh review-kit comments suggest-rules --dataset ./dataset

# Use a custom topics file and emit JSON for downstream tooling
gh review-kit comments suggest-rules --dataset ./dataset \
  --topics-file ./topics.json --format json --output ./candidates.json

# Only consider blocking review comments and require 3 distinct reviewers
gh review-kit comments suggest-rules --dataset ./dataset \
  --review-states CHANGES_REQUESTED --min-reviewers 3
```

#### Generate a Markdown/JSON report from a comments dataset

```sh
gh review-kit comments report --dataset DIR [--topics-file FILE] [--format FORMAT] [--output FILE] [--stats-top N] [--min-count N] [--min-reviewers N] [--examples N] [--comment-types TYPES] [--review-states STATES] [--path PREFIX] [--since RFC3339] [--until RFC3339] [--min-length N] [--include-bots]
```

Generate a deterministic Markdown or JSON report from a comments dataset. The report combines aggregate stats (by `comment_type`, `review_state`, `author`, `path_prefix`, `repo`) with rule candidates from `suggest-rules` and a manifest summary, so humans can review review-comment trends and sign off on which topics should become coding rules.

**Options:**

- `--comment-types`: Filter by comment types (optional)
- `--dataset`: Dataset directory (required)
- `--examples`: Number of evidence examples to include per topic (optional, default: 3)
- `--format`: Output format: `markdown`, `json` (optional, default: `markdown`)
- `--include-bots`: Include bot-authored comments (optional, default: false)
- `--min-count`: Drop topics matched fewer than this many times (optional, default: 3)
- `--min-length`: Minimum trimmed body length in bytes (optional, default: 0)
- `--min-reviewers`: Drop topics matched by fewer than this many distinct reviewers (optional, default: 2)
- `--output`: Output file path (optional, default: stdout)
- `--path`: Path prefixes for inline review comments, repeatable (optional)
- `--review-states`: Filter by review states (optional)
- `--since`: Created at or after this RFC3339 timestamp (optional)
- `--stats-top`: Top N rows per stats slice (optional, default: 20)
- `--topics-file`: JSON dictionary of topics (optional, default: built-in)
- `--until`: Created at or before this RFC3339 timestamp (optional)

**Examples:**

```sh
# Markdown report to stdout
gh review-kit comments report --dataset ./dataset

# Save a Markdown report and a JSON report side by side
gh review-kit comments report --dataset ./dataset --output ./report.md
gh review-kit comments report --dataset ./dataset --format json --output ./report.json

# Focus on blocking review feedback from the last 6 months
gh review-kit comments report --dataset ./dataset \
  --review-states CHANGES_REQUESTED --since 2025-10-01T00:00:00Z
```

### copilot

#### List Copilot code review comments on a pull request

```sh
gh review-kit copilot comments [--repo REPO] [--pr PR] [--author AUTHORS] [--include-resolved] [--include-outdated] [--evaluate] [--batch] [--prompt PROMPT | --prompt-file FILE] [--copilot-bin BIN] [--agent AGENT] [--allow-all-tools] [--model MODEL] [--evaluate-timeout DURATION] [--sandbox] [--session-id ID] [--rubber-duck] [--language LANGUAGE] [--dryrun] [--json FIELDS] [-- COPILOT_CLI_ARG...]
```

List GitHub Copilot code review comments on a pull request.

By default, resolved and outdated review threads are excluded. Use `--evaluate` to additionally judge each comment with the [Copilot CLI](https://github.com/github/copilot-cli): comments judged invalid receive a thumbs-down reaction and have their review thread resolved with resolution reason `INVALID`; comments judged valid have their review thread resolved with resolution reason `ADDRESSED`. The prompt used to judge comments must be supplied with `--prompt` or `--prompt-file`; gh-review-kit does not ship a built-in evaluation prompt.

The Copilot CLI runs in non-interactive mode (`-p`) for evaluation, so it can never prompt for tool permissions and denies anything not pre-authorized, regardless of whether the command is run from a terminal or in CI. Use `--allow-all-tools` to allow every tool, or pass arguments after a `--` separator to forward them to the Copilot CLI, e.g. `-- --allow-tool=... --deny-tool=...` to scope permissions more tightly (an organization's Copilot policy may disable these bypass options entirely). Without either, a warning is printed before evaluation starts, and any tool calls the Copilot CLI denies are reported as a warning afterward, since they may leave its judgement based on incomplete information.

The Copilot CLI keeps tool, path, and URL permissions in separate categories, so `--allow-all-tools` authorizes tool execution but not file access outside the working directory. Denied tool calls are therefore also reported with the options that would have allowed them, as a `--` passthrough list to paste onto a re-run, e.g. `-- --allow-tool='shell(go:*)' --add-dir=/path/to/module/cache`. Paths are always narrowed to `--add-dir`; `--allow-all-paths` is never recommended. Note that `--sandbox` enforces an OS-level filesystem policy on top of these permissions, so denials on paths no `--add-dir` can reach, such as symlinks out of the working directory or network access, persist until `--sandbox` is dropped.

Use `--model` to select the Copilot CLI model used for evaluation, and `--rubber-duck` to additionally ask the Copilot CLI's built-in [rubber duck agent](https://docs.github.com/en/copilot/concepts/agents/copilot-cli/rubber-duck) for a second opinion before it decides; the rubber duck agent runs on a different model, so this adds latency and model usage.

Every comment is judged with a single Copilot CLI invocation, which avoids repeated context and keeps AI credits down at the cost of a single combined judgement pass. Use `--batch=false` to run one invocation per comment instead.

Each run starts a new Copilot CLI session, so runs never inherit each other's context. The session ID is written to stderr both before and after the evaluation, along with the Copilot CLI output and the AI credits the session consumed. Pass that ID back with `--session-id` to resume the session, for example to keep the context of a previous run.

Use `--sandbox` to enable the Copilot CLI's OS-level shell sandbox for the evaluation. This also passes `--experimental`, since the Copilot CLI otherwise ignores `--sandbox`, and `--add-dir` for the directory the command runs in, since the sandbox otherwise blocks reading the checked-out repository. When paths are recommended, a `~/.copilot/settings.json` fragment granting them under `sandbox.userPolicy.filesystem.readonlyPaths` is printed as well, so the grant can be made permanent instead of repeated on every run; the Copilot CLI reads repository settings (`.github/copilot/settings.json` and `settings.local.json`) only in interactive mode, so they have no effect here.

**Options:**

- `--agent`: Copilot CLI custom agent to use for evaluation (optional, default: none)
- `--allow-all-tools`: Allow the Copilot CLI to use any tool without approval during evaluation (optional, default: false)
- `--author`: Comment author logins to match, repeatable (optional, default: `copilot-pull-request-reviewer`)
- `--batch`: Judge every comment with a single Copilot CLI invocation instead of one per comment (optional, default: true)
- `--copilot-bin`: Copilot CLI executable name or path (optional, default: `copilot`)
- `--dryrun, -n`: Report the action that would be taken without performing it (optional, default: false)
- `--evaluate`: Judge each comment with the Copilot CLI and act on the verdict (optional, default: false)
- `--evaluate-timeout`: Timeout for a single Copilot CLI evaluation, per comment; a batch run is given this much for every comment it covers (optional, default: `15m`)
- `--include-outdated`: Include comments whose review thread is outdated (optional, default: false)
- `--include-resolved`: Include comments whose review thread is already resolved (optional, default: false)
- `--language`: Language for the Copilot CLI's evaluation reason (optional, default: the Copilot CLI's default language)
- `--model`: Copilot CLI model to use for evaluation (optional, default: the Copilot CLI's default model)
- `--pr`: Pull request number, URL, or branch name (optional, default: current branch)
- `--prompt, -p`: Prompt used to judge comments with the Copilot CLI (required with `--evaluate`, mutually exclusive with `--prompt-file`)
- `--prompt-file`: File containing the prompt used to judge comments with the Copilot CLI (required with `--evaluate`, mutually exclusive with `--prompt`)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--rubber-duck`: Ask the Copilot CLI's built-in rubber duck agent for a second opinion before deciding (optional, default: false)
- `--sandbox`: Enable the Copilot CLI's OS-level shell sandbox for the evaluation (optional, default: false; also passes `--experimental` and `--add-dir` for the current directory)
- `--session-id`: Copilot CLI session to resume (optional, default: a new session)
- `-- COPILOT_CLI_ARG...`: Arguments forwarded verbatim to the Copilot CLI after a `--` separator, repeatable (optional)

**Examples:**

```sh
# List open Copilot review comments for the current branch
gh review-kit copilot comments

# List Copilot review comments including resolved and outdated threads
gh review-kit copilot comments --pr 123 --include-resolved --include-outdated

# Judge each comment with the Copilot CLI, allowing every tool without prompting
gh review-kit copilot comments --pr 123 --evaluate --prompt-file ./judge-prompt.md --allow-all-tools

# Judge each comment, scoping permissions to only what's needed instead of allowing everything
gh review-kit copilot comments --pr 123 --evaluate --prompt-file ./judge-prompt.md -- --allow-tool=read

# Judge each comment with its own Copilot CLI invocation instead of a single combined pass
gh review-kit copilot comments --pr 123 --evaluate --batch=false --prompt-file ./judge-prompt.md --allow-all-tools

# Judge each comment with a specific model, and get a rubber duck second opinion
gh review-kit copilot comments --pr 123 --evaluate --prompt-file ./judge-prompt.md --allow-all-tools --model gpt-5 --rubber-duck

# Judge each comment and have the evaluation reason written in Japanese
gh review-kit copilot comments --pr 123 --evaluate --prompt-file ./judge-prompt.md --language Japanese --allow-all-tools

# Preview the actions an evaluation run would take, without performing them
gh review-kit copilot comments --pr 123 --evaluate --prompt-file ./judge-prompt.md --dryrun --allow-all-tools

# Judge each comment with the Copilot CLI's OS-level shell sandbox enabled
gh review-kit copilot comments --pr 123 --evaluate --prompt-file ./judge-prompt.md --sandbox --allow-all-tools

# Resume a previous Copilot CLI session to keep its context
gh review-kit copilot comments --pr 123 --evaluate --prompt-file ./judge-prompt.md --session-id 0b2d6f5e-... --allow-all-tools
```

#### Leave a reaction on pull request review comments

```sh
gh review-kit copilot feedback <comment-id-or-url>... [--repo REPO] [--pr PR] [--content CONTENT]
```

Leave a reaction on one or more pull request review comments, typically to report that a GitHub Copilot code review comment was incorrect. Defaults to a thumbs-down (`-1`) reaction; use `--content` to send a different reaction.

Each comment can be given as a bare comment ID or as a review comment URL (`https://github.com/owner/repo/pull/123#discussion_r456789`); URLs also determine the target repository when `--repo` is omitted.

**Options:**

- `--content`: Reaction content: `+1`, `-1`, `laugh`, `confused`, `heart`, `hooray`, `rocket`, `eyes` (optional, default: `-1`)
- `--pr`: Pull request number, URL, or branch name, used to resolve the repository when `--repo` is omitted (optional, default: none)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)

**Examples:**

```sh
# Give negative feedback on a single Copilot review comment
gh review-kit copilot feedback 123456789 --repo owner/repo

# Give negative feedback on several comments at once
gh review-kit copilot feedback 123456789 123456790 --pr 123

# Give positive feedback on a comment
gh review-kit copilot feedback 123456789 --pr 123 --content +1

# Give negative feedback using the comment's URL, without --repo/--pr
gh review-kit copilot feedback https://github.com/owner/repo/pull/123#discussion_r456789
```

#### Resolve pull request review threads by comment ID

```sh
gh review-kit copilot resolve <comment-id-or-url>... [--repo REPO] [--pr PR] [--reason REASON] [--unresolve]
```

Resolve the review thread containing each of the given pull request review comment IDs. Use `--unresolve` to reopen the threads instead.

Each comment can be given as a bare comment ID or as a review comment URL (`https://github.com/owner/repo/pull/123#discussion_r456789`); URLs also determine the target repository and pull request when `--repo`/`--pr` are omitted.

**Options:**

- `--pr`: Pull request number, URL, or branch name (optional, default: current branch)
- `--reason`: Resolution reason: `addressed`, `wont-fix`, or `invalid` (optional, default: none, mutually exclusive with `--unresolve`)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--unresolve`: Reopen the threads instead of resolving them (optional, default: false)

**Examples:**

```sh
# Resolve the thread for a fixed Copilot review comment
gh review-kit copilot resolve 123456789 --pr 123

# Resolve several comments' threads at once
gh review-kit copilot resolve 123456789 123456790 --pr 123

# Resolve the thread for an incorrect Copilot review comment
gh review-kit copilot resolve 123456789 --pr 123 --reason invalid

# Reopen a previously resolved thread
gh review-kit copilot resolve 123456789 --pr 123 --unresolve

# Resolve using the comment's URL, without --repo/--pr
gh review-kit copilot resolve https://github.com/owner/repo/pull/123#discussion_r456789
```

#### Request a GitHub Copilot code review on a pull request

```sh
gh review-kit copilot review-request [pull-request-number] [--repo REPO] [--force]
```

Request a code review from GitHub Copilot on a pull request, the same as requesting a review from a human reviewer. The request is only sent when the latest commit has not been reviewed yet:

| State | Action |
| --- | --- |
| Copilot has never been requested | request a review |
| Copilot reviewed an earlier commit only | request a review again |
| Copilot is a requested reviewer already | nothing to do |
| Copilot has reviewed the latest commit | nothing to do |

Use `--force` to request a review regardless of that state.

Copilot's [review effort level](https://docs.github.com/en/copilot/concepts/agents/code-review#review-effort-level) (Lite or Balanced) cannot be selected through this command: neither the REST `requested_reviewers` endpoint nor the GraphQL `requestReviews` mutation exposes a parameter for it, so the pull request's or organization's configured default effort level is always used. Change the default in the repository or organization settings, or select it manually in the pull request's Reviewers section on GitHub.com, if a specific effort level is needed.

**Aliases:** `rr`

**Options:**

- `--force`: Request a review even when the latest commit has already been reviewed (optional, default: false)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)

**Examples:**

```sh
# Request a Copilot review on the current branch's pull request
gh review-kit copilot review-request

# Request a Copilot review on a specific pull request
gh review-kit copilot rr 123 --repo owner/repo

# Request a review even though the latest commit has already been reviewed
gh review-kit copilot review-request --force
```

#### Show the GitHub Copilot code review status of a pull request

```sh
gh review-kit copilot status [pull-request-number] [--repo REPO] [--format json]
```

Show where GitHub Copilot's code review of a pull request stands, together with the number of Copilot review comments and how many of them are still unresolved. If `pull-request-number` is omitted, the pull request for the current branch is used.

The reported status is one of:

| Status | Meaning |
| --- | --- |
| `not_requested` | Copilot has not been requested and has not reviewed yet |
| `in_progress` | Copilot is a requested reviewer and has not answered yet |
| `commented` | Copilot's latest review left comments |
| `approved` | Copilot's latest review approved the pull request |
| `changes_requested` | Copilot's latest review requested changes |
| `dismissed` | Copilot's latest review was dismissed |

A pending review request takes precedence, so a pull request that Copilot has already reviewed and is reviewing again is reported as `in_progress`.

`head_reviewed` tells whether Copilot reviewed the latest commit of the pull request. A `false` means every review it submitted predates the newest push, so the current code has not been reviewed yet.

**Options:**

- `--format`: Output format: `text`, `json` (optional, default: `text`)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)

**Examples:**

```sh
# Show the Copilot review status of the current branch's pull request
gh review-kit copilot status

# Show the Copilot review status of a specific pull request
gh review-kit copilot status 123 --repo owner/repo

# Check whether Copilot is still reviewing
gh review-kit copilot status 123 --format json --jq .status

# Check whether the latest commit has been reviewed
gh review-kit copilot status 123 --format json --jq .head_reviewed
```

### Re-request review for a pull request

```sh
gh review-kit rerequest [pull-request-identifier] [--repo REPO] [--reviewers REVIEWERS] [--exclude-approved] [--expand-team]
```

Re-request review for a pull request.

If reviewers are not specified, the command will re-request review from all reviewers who have already submitted a review.
If reviewers are specified, the command will re-request review from the specified reviewers only.

Reviewers can be specified as:

- Individual users: `username`
- Team reviewers: `org/team-slug`
- With @ prefix: `@username` or `@org/team-slug`

When `--expand-team` is specified, team reviewers will be expanded to individual team members.

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

### Mark files as viewed in a pull request

```sh
gh review-kit reviewed [file...] [--repo REPO] [--pr PR]
```

Mark files in a pull request as viewed using the GitHub `markFileAsViewed` API.

If file paths are specified as arguments, only those files will be marked as viewed.
If no file paths are specified, all files marked as `linguist-generated` in the repository's `.gitattributes` will be marked as viewed.

The pull request can be specified by:

- PR number (e.g., `123` or `#123`)
- PR URL (e.g., `https://github.com/owner/repo/pull/123`)
- Branch name (e.g., `feature/my-branch`)
- If omitted, uses the pull request associated with the current branch

**Options:**

- `--pr`: Pull request number, URL, or branch name (optional, default: current branch)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)

**Examples:**

```sh
# Mark all linguist-generated files as viewed for current branch
gh review-kit reviewed

# Mark all linguist-generated files as viewed for a specific PR
gh review-kit reviewed --pr 123

# Mark specific files as viewed
gh review-kit reviewed path/to/generated_file.go another/file.go

# Mark specific files as viewed for a specific PR
gh review-kit reviewed --pr 123 path/to/generated_file.go

# Mark files as viewed in a different repository
gh review-kit reviewed --repo owner-name/repo-name --pr 123
```
