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
