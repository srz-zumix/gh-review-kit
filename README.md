# gh-review-kit

Make GitHub pull request reviews easier to inspect, act on, and learn from with a `gh` extension.

- **Find what needs attention:** filter PR checks, read failed check logs, and gather review feedback in one place.
- **Keep reviews moving:** re-request reviews, mark generated files as viewed, and manage Copilot review comments and threads.
- **Learn from past reviews:** extract feedback into a reusable dataset, explore trends, and generate reports or candidate coding rules.
- **Trace media back to its source:** embed and inspect Git provenance in videos and images.

## Installation

```sh
gh extension install srz-zumix/gh-review-kit
```

## Shell Completion

Completion requires a workaround for `gh` extensions. Set up `gh` completion first, then follow the [shell completion guide](https://github.com/srz-zumix/go-gh-extension/blob/main/docs/shell-completion.md).

## Quick Start

```sh
gh review-kit checks list
gh review-kit checks failure
gh review-kit comments list --include-bots
gh review-kit copilot status
gh review-kit insights estimate
```

PR-related commands use the current branch's pull request by default. Use `--repo OWNER/REPO` to target another repository where supported. For every option, its default, and more examples, see the [command reference](docs/commands.md); `gh review-kit <command> --help` also shows the available flags.

## Commands

The following are usage summaries. Brackets denote optional arguments or flags; `--dataset DIR` and other unbracketed arguments are required. Omitted flags use their documented defaults in the [command reference](docs/commands.md).

### attestation

#### Display embedded Git provenance

```sh
gh review-kit attestation view [FILE | ASSET_URL] [--pr PR] [flags]
```

Read metadata from a local video or image, a GitHub asset, or all attachments on a PR with `--pr`. Video inspection requires `ffprobe`; `--format` selects text (default) or JSON.

#### Embed Git provenance

```sh
gh review-kit attestation set FILE --output OUTPUT [flags]
gh review-kit attestation set (--pr PR | --issue ISSUE) [ASSET_URL] [flags]
```

Add unsigned Git metadata to a copy of a video or image, or update PR/issue attachments and their links. Local files require `--output`; attachments use the current repository by default. Video processing requires `ffmpeg` and `ffprobe`.

### checks

#### Display failed check logs

```sh
gh review-kit checks failure [PR] [flags]
```

Read logs for failed checks, optionally restricting to required checks or requesting full logs. Omitting `PR` uses the current branch.

#### List PR check runs

```sh
gh review-kit checks list [PR] [flags]
```

Filter by status, conclusion, or required state; optionally show run and job IDs. Omitting `PR` uses the current branch.

### comments

#### List PR review feedback

```sh
gh review-kit comments list [PR] [flags]
```

Combine review bodies, inline comments, and issue comments; filter by type or path, include bots, or export JSON. Omitting `PR` uses the current branch.

### copilot

#### List and evaluate Copilot review comments

```sh
gh review-kit copilot comments [flags]
```

List Copilot feedback, including resolved or outdated threads when requested; optionally evaluate it using Copilot CLI or Claude Code. Without `--pr`, use the current branch's PR.

#### React to review comments

```sh
gh review-kit copilot feedback COMMENT_ID_OR_URL... [flags]
```

Leave a reaction on one or more review comments. `--content` selects the reaction; the default is `-1`.

#### Request a Copilot review

```sh
gh review-kit copilot review-request [PR_NUMBER] [flags]
```

Request a review of the latest commit, optionally forcing a repeat request. Omitting the number uses the current branch's PR.

#### Resolve review threads

```sh
gh review-kit copilot resolve COMMENT_ID_OR_URL... [flags]
```

Resolve threads by comment ID or URL; use `--unresolve` to reopen them (default: resolve).

#### Show Copilot review status

```sh
gh review-kit copilot status [PR_NUMBER] [flags]
```

Check whether Copilot has reviewed the latest commit or is still reviewing; optionally export JSON. Omitting the number uses the current branch's PR.

### insights

#### Bundle review data

```sh
gh review-kit insights bundle --dataset DIR --output-dir DIR [flags]
```

Split an extracted dataset into smaller JSONL bundles for parallel analysis; both directories are required.

#### Estimate extraction work

```sh
gh review-kit insights estimate [flags]
```

Preview PR volume, comment counts, and API budget before extracting; the repository defaults to the current repository.

#### Extract review data

```sh
gh review-kit insights extract --dataset DIR [flags]
```

Collect PR review feedback into a resumable dataset; `--update` refreshes changed PRs and the repository defaults to the current repository.

#### Generate a report

```sh
gh review-kit insights report --dataset DIR [flags]
```

Summarize feedback trends and candidate rules in Markdown (default) or JSON; output defaults to stdout.

#### Sample review comments

```sh
gh review-kit insights sample --dataset DIR [flags]
```

Select representative comments by strategy or group; optionally filter and write results to a file.

#### Summarize review data

```sh
gh review-kit insights stats --dataset DIR [flags]
```

Count feedback by author, type, repository, or other dimensions; choose grouping and top-N limits with flags.

#### Suggest coding rules

```sh
gh review-kit insights suggest-rules --dataset DIR [flags]
```

Rank recurring review topics using a built-in (default) or custom topic dictionary.

#### Validate review data

```sh
gh review-kit insights validate --dataset DIR [flags]
```

Check dataset integrity; use `--strict` to fail on issues or `--format json` for structured output.

### rerequest

#### Re-request PR reviews

```sh
gh review-kit rerequest [PR] [flags]
```

Request another review from previous reviewers (default) or selected users and teams; optionally exclude approvers. Omitting `PR` uses the current branch.

### reviewed

#### Mark PR files as viewed

```sh
gh review-kit reviewed [FILE...] [flags]
```

Mark specified files as viewed, or (by default) all linguist-generated files; `--pr` selects a PR instead of the current branch's PR.

## Agent Skills

Use `gh review-kit skills` to install and manage bundled agent skills. See [skillsmith](https://github.com/Songmu/skillsmith) for details.

## Global Options

All commands accept `--read-only` to prevent write operations (default: off) and `--log-level, -L` to select `debug`, `info`, `warn`, or `error` (default: `info`).
