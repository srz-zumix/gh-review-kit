---
name: gh-review-kit-comments
description: Build and analyze normalized datasets of GitHub pull request review feedback at scale using the `gh review-kit comments` subcommand family. Use when mining historical review comments (50k+ scale), preflighting API budgets, extracting/validating/refreshing JSONL corpora, slicing the corpus for LLM/Agent analysis, ranking candidate coding rules, or generating review-feedback reports.
---

# gh-review-kit-comments

`gh review-kit comments` is a self-contained pipeline for turning GitHub pull request review feedback (review bodies, inline review comments, and PR issue comments) into a normalized JSONL dataset and analyzing it at scale. This skill covers only the `comments` subcommand family. For check-run / re-request workflows see the `gh-review-kit` skill.

## Prerequisites

```bash
# Install the extension
gh extension install srz-zumix/gh-review-kit

# Verify installation
gh review-kit --version

# Login to GitHub (if not already authenticated)
gh auth login
```

## Subcommand Map

```
gh review-kit comments
├── estimate       # Preflight: PR count, projected comments, API budget, rate-limit headroom
├── extract        # Extract PR review feedback into a dataset directory
├── validate       # Validate dataset schema and integrity
├── stats          # Aggregate counts (by type, repo, author, review_state, path_prefix, label)
├── sample         # Pick representative comments (recent / diverse-authors / blocking / random)
├── bundle         # Split the corpus into Agent-sized JSONL bundles for parallel analysis
├── suggest-rules  # Rank deterministic candidate coding rules / review viewpoints
└── report         # Generate Markdown or JSON report combining stats + rule candidates
```

## Dataset Layout

Every subcommand except `estimate` operates on a single dataset directory created by `extract`:

- `corpus.jsonl` — one JSON record per comment (append-only)
- `prs.jsonl` — one JSON record per included PR (append-only)
- `manifest.json` — filter parameters and running counts
- `checkpoint.json` — completed PR numbers + per-PR `updated_at`, used to safely resume and detect updates

Treat the directory as the unit of work. Re-running `extract` with the same `--dataset` resumes from the checkpoint; pass `--update` to additionally re-fetch PRs whose `updated_at` advanced — their existing PR and comment records are atomically replaced. Conservative secret/token redaction is applied by default; pass `--no-redact` to opt out.

## How to Use This Command Group

The pipeline is deliberately split into small, composable steps so you can stop at the level of insight you need. Pick subcommands by intent, not by name.

### Pipeline at a Glance

```
estimate ─► extract ─► validate ─► stats ─┬─► sample        (evidence for an Agent / human review)
   ▲          ▲                           ├─► bundle        (parallel LLM/Agent analysis)
   │          │                           ├─► suggest-rules (ranked rule candidates)
   │          └── --update for refresh    └─► report        (one-shot Markdown / JSON deliverable)
   └── re-run before large jobs
```

- `estimate` is read-only and fast. Always run it first on an unfamiliar repo or after widening filters.
- `extract` is the only command that calls the GitHub API in bulk and writes the dataset directory. Everything downstream is local and deterministic.
- `validate` should be run once after each `extract` (especially before `report` / `suggest-rules`).
- `stats` / `sample` / `bundle` / `suggest-rules` / `report` all read the same dataset and accept the same filter flags, so you can iterate without re-extracting.

### API Access and `GH_HOST`

Only `estimate` and `extract` call the GitHub API. All other subcommands (`validate`, `stats`, `sample`, `bundle`, `suggest-rules`, `report`) operate entirely on local files in the dataset directory and **never make network requests** — no host specification is needed for them.

| Subcommand | Calls GitHub API? | Host specification needed for GHES? |
| --- | --- | --- |
| `estimate` | Yes | Yes (`--repo HOST/owner/repo`) |
| `extract` | Yes | Yes (`--repo HOST/owner/repo`) |
| `validate` | No | No |
| `stats` | No | No |
| `sample` | No | No |
| `bundle` | No | No |
| `suggest-rules` | No | No |
| `report` | No | No |

When targeting a GitHub Enterprise Server instance, specify the host as part of `--repo` (format: `HOST/owner/repo`). No `GH_HOST` environment variable is needed:

```bash
# Extract from GHES — host is part of the --repo value
gh review-kit comments estimate --repo ghes.example.com/owner/repo
gh review-kit comments extract --repo ghes.example.com/owner/repo --dataset ./dataset

# Analysis commands — no host needed, they read local files only
gh review-kit comments stats --dataset ./dataset --group-by author --top 20
gh review-kit comments suggest-rules --dataset ./dataset
gh review-kit comments report --dataset ./dataset --output ./report.md
```

### Choosing the Right Subcommand

| You want to … | Use |
| --- | --- |
| Know how many PRs / comments / API calls a future extract will cost | `estimate` |
| Build the corpus from scratch | `extract` |
| Resume an interrupted extract | `extract` with the same `--dataset` |
| Pick up new comments on PRs that were already extracted | `extract --update` |
| Confirm the dataset is well-formed before analysis | `validate --strict` |
| Get a quick distribution (top authors, repos, paths, labels) | `stats` |
| Hand a small, curated set of examples to a human or a single LLM call | `sample` |
| Run many parallel LLM/Agent jobs over the whole corpus | `bundle` |
| Discover recurring review topics and turn them into coding rules | `suggest-rules` |
| Produce a single deliverable summarizing everything | `report` |

### Filter Flags Are Shared

Most analysis subcommands (`sample`, `bundle`, `suggest-rules`, `report`) accept the same filter flags as `extract`: `--comment-types`, `--path`, `--review-states`, `--since`, `--until`, `--min-length`, `--include-bots`, `--authors` (where applicable). Prefer extracting a wide corpus once and slicing it later — re-extraction is the expensive step.

### Iteration Loop

1. **Estimate** with the filters you care about; if the projected API budget exceeds rate-limit headroom, narrow with `--merged`, `--since`, `--labels`, or `--limit`.
2. **Extract** into a stable directory (e.g. `./review-corpus`). Resume freely — interruptions are safe.
3. **Validate** the dataset; fix any issues (usually by re-running `extract --update`) before downstream steps.
4. **Explore** with `stats` to find high-value slices (noisy authors, hot paths, blocking review states).
5. **Drill in** with `sample` (handful of examples) or `suggest-rules` (ranked topics) using the slice you identified.
6. **Scale out** with `bundle` when you need parallel LLM/Agent runs, or skip straight to `report` for a single deliverable.
7. **Refresh** periodically by re-running `extract --update` against the same directory.

## Estimate API Work (comments estimate)

Preflight a future `extract`. Lists matching PRs, samples a few for averages, and reports projected total comments, projected API calls, and current rate-limit headroom. Run before large jobs to avoid hitting secondary rate limits.

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

Extract pull request review feedback into a dataset directory. Subsequent commands operate on this directory.

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

## Recipes by Use Case

Short, copy-pasteable recipes for the most common goals. All assume `--dataset ./review-corpus` for clarity.

### Recipe A — One-shot exploration of a small repo

```bash
gh review-kit comments estimate --repo owner/repo
gh review-kit comments extract  --repo owner/repo --dataset ./review-corpus
gh review-kit comments validate --dataset ./review-corpus --strict
gh review-kit comments report   --dataset ./review-corpus --output ./review-report.md
```

### Recipe B — Mining a large repo (thousands of PRs)

```bash
# 1. Budget check; narrow until the projection fits the rate-limit headroom
gh review-kit comments estimate --repo owner/repo --merged --since 2024-01-01T00:00:00Z

# 2. Extract in chunks; safe to interrupt and resume
gh review-kit comments extract  --repo owner/repo --dataset ./review-corpus \
  --merged --since 2024-01-01T00:00:00Z --limit 500

# 3. Re-run the same command until estimate reports 0 remaining PRs
gh review-kit comments extract  --repo owner/repo --dataset ./review-corpus \
  --merged --since 2024-01-01T00:00:00Z --limit 500

gh review-kit comments validate --dataset ./review-corpus --strict
```

### Recipe C — Incremental refresh (scheduled job)

```bash
# Pick up new PRs and updates to existing PRs since the last run
gh review-kit comments extract --repo owner/repo --dataset ./review-corpus --update
gh review-kit comments validate --dataset ./review-corpus --strict
gh review-kit comments report   --dataset ./review-corpus --output ./review-report.md
```

### Recipe D — Focus on blocking feedback in a specific area

```bash
gh review-kit comments stats   --dataset ./review-corpus --group-by path_prefix --top 30
gh review-kit comments sample  --dataset ./review-corpus \
  --path internal/ --review-states CHANGES_REQUESTED \
  --group-by author --per-group 5 --strategy blocking --output ./evidence.jsonl
gh review-kit comments suggest-rules --dataset ./review-corpus \
  --path internal/ --review-states CHANGES_REQUESTED --min-reviewers 3 \
  --format markdown --output ./rules.md
```

### Recipe E — Parallel LLM/Agent analysis

```bash
# Split into bundles small enough for an LLM context window, grouped by repo
gh review-kit comments bundle --dataset ./review-corpus --output-dir ./bundles \
  --max-bytes 500000 --group-by repo

# Then run your Agent over each ./bundles/*.jsonl in parallel
```

## End-to-End Workflow: Mine Review Comments at Scale

Build a reproducible dataset of historical review feedback, then summarize it before handing slices to an LLM/Agent. Designed for repositories with thousands of PRs and supports safe resume after rate limits or interruptions.

```bash
# 0. Preflight: estimate API work and rate-limit headroom
gh review-kit comments estimate --repo owner/repo --merged

# 1. Extract review feedback into a dataset directory
gh review-kit comments extract --repo owner/repo --dataset ./review-corpus

# 1b. Refresh PRs that were updated since the last extract run
gh review-kit comments extract --repo owner/repo --dataset ./review-corpus --update

# 2. Validate the dataset before analysis
gh review-kit comments validate --dataset ./review-corpus --strict

# 3. Pick high-value slices instead of reading every record
gh review-kit comments stats --dataset ./review-corpus --group-by review_state
gh review-kit comments stats --dataset ./review-corpus --group-by author --top 20
gh review-kit comments stats --dataset ./review-corpus --group-by path_prefix --top 30 --format json

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
```

## Using the Output with LLMs / Agents

Each subcommand produces a different shape of artifact. This section shows what to do with them and gives copy-pasteable prompt templates.

### Output Cheat Sheet

| Source | Artifact | Typical use |
| --- | --- | --- |
| `extract` | `corpus.jsonl`, `prs.jsonl` | Source of truth; do not feed directly to an LLM at scale |
| `stats --format json` | small JSON | Steer humans / agents to high-value slices |
| `sample --output X.jsonl` | small JSONL | Few-shot evidence for a single LLM call |
| `bundle --output-dir X` | many JSONL files + `manifest.json` | Parallel LLM/Agent jobs, one bundle per task |
| `suggest-rules --format json` | ranked candidates JSON | Seed for rule synthesis / classifier prompts |
| `suggest-rules --format markdown` | ranked candidates Markdown | Direct human review, ready to paste in a doc/PR |
| `report --format markdown` | full Markdown report | Standalone deliverable; can also be a system-prompt context |
| `report --format json` | full report JSON | Programmatic post-processing / dashboards |

A `Comment` JSONL record (corpus.jsonl, sample, bundle) has fields like:

```json
{"id":123,"type":"review_comment","repo":"owner/repo","pr_number":42,
 "author":"alice","body":"Please add a nil check before deref.",
 "review_state":"CHANGES_REQUESTED","path":"internal/foo.go","line":87,
 "created_at":"2025-03-01T10:00:00Z","url":"https://github.com/owner/repo/pull/42#discussion_r1"}
```

Tell the LLM about these fields once; afterwards it can cite `url` and `path` precisely.

### Prompt Template 1 \u2014 Extract recurring rules from `sample` output

Use after `gh review-kit comments sample --strategy blocking --output ./evidence.jsonl`.

```text
You are reviewing a JSONL file of GitHub pull-request review comments. Each line is one
comment with fields: id, type, repo, pr_number, author, body, review_state, path, line, url.

Task:
1. Read every comment in <evidence>.
2. Cluster them into 3\u201310 recurring review themes.
3. For each theme, output:
   - title (imperative, <= 8 words, e.g. "Validate inputs at boundaries")
   - rule (1\u20132 sentences, actionable; written for a contributor, not a reviewer)
   - 3 representative quotes, each with the source url
   - rough frequency ("seen in N comments out of M")

Constraints:
- Stay strictly grounded in the provided comments. If a theme appears <3 times, drop it.
- Quotes must be verbatim, trimmed to <= 200 chars.
- Output Markdown.

<evidence>
{{paste contents of ./evidence.jsonl}}
</evidence>
```

### Prompt Template 2 \u2014 Promote `suggest-rules` output into a coding guideline

Use after `gh review-kit comments suggest-rules --format json --output ./candidates.json`.

```text
You are drafting a coding-guidelines document for the {{language/framework}} codebase
of {{repo or org}}. Below is JSON of candidate rules ranked by frequency, distinct
reviewer count, and blocking share. Each candidate has: topic, count, reviewers, repos,
blocking_ratio, examples (with url + body).

Produce a Markdown guideline with sections:
- "Must" (blocking_ratio >= 0.5 and reviewers >= 3)
- "Should" (count >= 10 and reviewers >= 2, otherwise)
- "Consider" (everything else above the cutoff)

Each rule entry contains:
- short imperative title
- the rule itself in 1\u20133 sentences
- a 1-line rationale citing how often it was raised in review
- one short example link (url) as evidence

Skip topics that are clearly project-specific noise. Do not invent rules not present
in the input.

<candidates>
{{paste contents of ./candidates.json}}
</candidates>
```

### Prompt Template 3 \u2014 Review a new PR using a `bundle` shard as context

Use after `gh review-kit comments bundle --group-by repo --max-bytes 500000 --output-dir ./bundles`.
Pick the bundle whose `repo` matches the PR under review and pass it as historical context.

```text
SYSTEM:
You are a senior code reviewer for {{repo}}. The <history> block contains JSONL of past
review comments on this repository, one per line, with fields: author, body, review_state,
path, line, url. Use them to mirror the team's actual review style and standards. Cite
relevant past comments by url when they justify a finding.

USER:
Review the diff below. For each finding output: severity (blocker/nit), file:line,
1-sentence problem, recommended fix, and (if applicable) a citation url from <history>.
Do not invent issues that aren't supported by either the diff or <history>.

<history>
{{paste contents of ./bundles/<repo>.jsonl}}
</history>

<diff>
{{paste git diff}}
</diff>
```

### Prompt Template 4 \u2014 Summarize the Markdown report for a stakeholder

Use after `gh review-kit comments report --output ./review-report.md`.

```text
Below is an automatically generated review-feedback report for {{repo or org}}. Produce
a 10-bullet executive summary aimed at an engineering manager. Each bullet must be
fact-grounded in the report and reference the section it came from. Highlight:
- top 3 recurring blocking themes
- 2 hotspots (paths or authors)
- 2 trends over time if visible
- 3 concrete next actions (e.g. lint rule, doc, training)

<report>
{{paste contents of ./review-report.md}}
</report>
```

### Tips for Feeding Output to LLMs

- **Always slice first.** Feeding raw `corpus.jsonl` (tens of thousands of lines) wastes context. Use `sample`, `bundle`, or `suggest-rules` to compress.
- **Keep the schema in the prompt.** A one-line description of the JSONL fields lets the LLM cite `url` / `path` correctly.
- **Prefer JSON for machines, Markdown for humans.** `--format json` outputs are stable and easy to post-process; Markdown outputs are ready to drop into docs/PRs.
- **Stay grounded.** Tell the model to drop themes that aren't supported by the data and to cite `url` for every claim. The `examples[].url` fields exist for exactly this.
- **Combine artifacts.** A common pattern is `suggest-rules` (themes) + a per-theme `sample --strategy blocking` (evidence) feeding the same prompt.

## Operational Notes

- **Resume vs. update.** Plain re-runs of `extract` skip PRs already in `checkpoint.json`. To pick up new comments on existing PRs, add `--update`; the command rewrites `corpus.jsonl` and `prs.jsonl` (tmp + atomic rename) to drop the stale records, then re-fetches them through the normal extraction loop.
- **Filter consistency.** All analysis subcommands accept the same filter flags (`--comment-types`, `--path`, `--review-states`, `--since`, `--until`, `--min-length`, `--include-bots`, `--authors`). Use them at analysis time rather than re-extracting.
- **Redaction.** Conservative secret/token redaction is applied during `extract` unless `--no-redact` is passed. Validation and downstream commands operate on the redacted text.
- **Determinism.** `stats`, `suggest-rules`, and `report` are deterministic for the same dataset and flags. `sample --strategy random` is deterministic when `--seed` is set.
- **Rate limits.** Always run `estimate` before large jobs. If the projected API budget approaches the headroom, narrow the corpus with `--merged`, `--since`, `--labels`, or `--limit` before running `extract`.
- **GHES.** Only `estimate` and `extract` talk to the GitHub API. Specify the GHES host as part of `--repo` (e.g. `--repo ghes.example.com/owner/repo`); no `GH_HOST` environment variable is needed. All other subcommands are local-only and do not require any host specification.

## References

- Repository: https://github.com/srz-zumix/gh-review-kit
- Companion skill: `gh-review-kit` (covers `checks` and `rerequest`)
