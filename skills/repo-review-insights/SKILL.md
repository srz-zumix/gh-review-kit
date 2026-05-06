---
name: repo-review-insights
description: End-to-end workflow that profiles a GitHub repository's review culture and code-quality concerns by combining `gh review-kit comments` with the standard `gh` CLI (`gh pr view`, `gh pr list`, `gh api`, `gh issue list`). Invoking this skill runs the full pipeline (estimate → extract → validate → stats → suggest-rules → report) against a target repo and produces a finished Markdown deliverable. Also handles follow-up instructions such as "summarize review viewpoints", "profile reviewer @alice", "compare two periods", or "focus on path X".
---

# repo-review-insights

A complete, one-shot workflow for understanding a repository's review tendencies. When invoked, you (the agent) execute every step below until a Markdown deliverable is produced; you do not stop and ask the user mid-pipeline unless something fails. After the initial run, the user may issue follow-up instructions — handle them by re-using the already-built dataset whenever possible.

## When to Invoke

Use this skill when the user asks anything like:
- "Investigate the review trends of `owner/repo`."
- "What kind of feedback do reviewers give on this codebase?"
- "Summarize review viewpoints for `owner/repo`."
- "What are `@alice`'s review patterns?"
- "Find recurring blockers in `internal/` over the last 6 months."

If the user only wants a single artifact (e.g. "just give me the stats by author"), still run the minimum prerequisite steps but stop at the requested artifact.

## Inputs to Resolve First

Before running anything, determine these from the user's prompt; ask **once** in a single round if essential values are missing.

| Input | Default if omitted |
| --- | --- |
| Target repo (`owner/repo`) | The current repository inferred via `gh repo view --json nameWithOwner -q .nameWithOwner` |
| Time window | Last 12 months (`--since` = 1y ago, RFC3339) |
| Merged-only? | Yes (`--merged`) for "trend / quality" questions; No when the user asks about open-PR review behavior |
| Dataset directory | `./.review-insights/<owner>__<repo>` (create if missing) |
| Output report path | `./.review-insights/<owner>__<repo>/report.md` |
| Specific focus (path / author / label / review state) | None |

Do not invent values for `--repo` if it is ambiguous; ask the user.

## Pipeline (Execute Top-to-Bottom)

> Every command below is local and idempotent except `extract`. Re-running the skill on the same dataset directory will resume safely.

### Step 1. Sanity check the target with plain `gh`

```bash
# Confirm repo exists, capture default branch, primary language, recent activity
gh repo view {{owner/repo}} --json nameWithOwner,defaultBranchRef,primaryLanguage,pushedAt,description \
  | tee ./.review-insights/{{owner__repo}}/repo.json

# Recent PR throughput (last 50 merged PRs) — gives a feel for activity
gh pr list --repo {{owner/repo}} --state merged --limit 50 \
  --json number,title,author,mergedAt,additions,deletions,changedFiles,labels \
  > ./.review-insights/{{owner__repo}}/recent-prs.json

# Top reviewers by recent reviews (lightweight signal before extraction)
gh api -X GET "repos/{{owner/repo}}/pulls?state=closed&per_page=30" --paginate \
  --jq '.[] | {n:.number, t:.title, merged:.merged_at, user:.user.login}' \
  > ./.review-insights/{{owner__repo}}/recent-pulls.jsonl
```

If `gh repo view` fails, stop and surface the error to the user.

### Step 2. Preflight the API budget

```bash
gh review-kit comments estimate \
  --repo {{owner/repo}} --merged --since {{since}} \
  --format json > ./.review-insights/{{owner__repo}}/estimate.json
```

Read `estimate.json`. If `projected_api_calls` exceeds `rate_limit.remaining * 0.7`, narrow with one of:
- shorter `--since`
- `--limit 500` (and plan to re-run)
- a label filter (`--labels`)

State the chosen budget plan in one sentence before continuing.

### Step 3. Extract the dataset

```bash
gh review-kit comments extract \
  --repo {{owner/repo}} \
  --dataset ./.review-insights/{{owner__repo}} \
  --merged --since {{since}}
```

If the extract is interrupted, re-run the same command (it resumes from `checkpoint.json`). If the user runs the skill again later, add `--update` to refresh PRs whose `updated_at` advanced.

### Step 4. Validate

```bash
gh review-kit comments validate \
  --dataset ./.review-insights/{{owner__repo}} --strict \
  --format json > ./.review-insights/{{owner__repo}}/validation.json
```

If validation fails, fix-forward by re-running `extract --update` once. If it still fails, stop and report the issues.

### Step 5. Aggregate signals

Run all four in parallel; they are read-only:

```bash
gh review-kit comments stats --dataset ./.review-insights/{{owner__repo}} \
  --group-by review_state --format json > ./.review-insights/{{owner__repo}}/stats-state.json

gh review-kit comments stats --dataset ./.review-insights/{{owner__repo}} \
  --group-by author --top 20 --format json > ./.review-insights/{{owner__repo}}/stats-author.json

gh review-kit comments stats --dataset ./.review-insights/{{owner__repo}} \
  --group-by path_prefix --top 30 --format json > ./.review-insights/{{owner__repo}}/stats-path.json

gh review-kit comments stats --dataset ./.review-insights/{{owner__repo}} \
  --group-by label --top 20 --format json > ./.review-insights/{{owner__repo}}/stats-label.json
```

### Step 6. Pull evidence and rank topics

```bash
# Blocking review feedback evidence, grouped by path prefix
gh review-kit comments sample \
  --dataset ./.review-insights/{{owner__repo}} \
  --strategy blocking --group-by path_prefix --per-group 5 \
  --output ./.review-insights/{{owner__repo}}/evidence-blocking.jsonl

# Ranked candidate review viewpoints / coding rules
gh review-kit comments suggest-rules \
  --dataset ./.review-insights/{{owner__repo}} \
  --review-states CHANGES_REQUESTED --min-reviewers 2 \
  --format json --output ./.review-insights/{{owner__repo}}/candidates.json

gh review-kit comments suggest-rules \
  --dataset ./.review-insights/{{owner__repo}} \
  --review-states CHANGES_REQUESTED --min-reviewers 2 \
  --format markdown --output ./.review-insights/{{owner__repo}}/candidates.md
```

### Step 7. Enrich the top PRs with `gh pr view`

For the 5 PRs cited most in `evidence-blocking.jsonl` (count distinct `pr_number`), pull a richer view to back the narrative:

```bash
gh pr view {{number}} --repo {{owner/repo}} \
  --json number,title,author,labels,body,reviewDecision,reviewRequests,reviews,files,mergedAt
```

Save each as `./.review-insights/{{owner__repo}}/pr-{{number}}.json`. If a PR cannot be fetched, skip it silently.

### Step 8. Generate the deliverable

```bash
gh review-kit comments report \
  --dataset ./.review-insights/{{owner__repo}} \
  --review-states CHANGES_REQUESTED \
  --output ./.review-insights/{{owner__repo}}/report.md
```

Then synthesize a final Markdown deliverable that combines the auto-generated `report.md` with the prose summary you write yourself. Required sections:

1. **Repository at a glance** — facts from `repo.json` + `recent-prs.json` (activity, language, top contributors, default branch).
2. **Review culture** — derived from `stats-state.json`, `stats-author.json` (who reviews, blocking ratio, churn).
3. **Where reviewers focus** — `stats-path.json` + 2–3 quotes from `evidence-blocking.jsonl` per hotspot.
4. **Recurring review viewpoints** — pull from `candidates.md`; rewrite into Must / Should / Consider.
5. **Illustrative PRs** — 3–5 PR cards using `pr-*.json` (title, link, why it's representative, 1 quote).
6. **Open questions / next actions** — 3 concrete items (lint rule, doc, training, automation idea).

Cite every claim with a `url` from the dataset records when possible. Do not fabricate themes that are not present in the JSON outputs.

Write the synthesized version to `./.review-insights/{{owner__repo}}/insights.md` and tell the user where both `report.md` (auto) and `insights.md` (synthesized) live.

## Handling Follow-Up Instructions

After the initial pipeline, the dataset directory is reusable. Map common follow-ups to commands without re-extracting unless the request requires fresh data.

### "Summarize review viewpoints" / "What do reviewers care about?"

```bash
gh review-kit comments suggest-rules \
  --dataset ./.review-insights/{{owner__repo}} \
  --review-states CHANGES_REQUESTED --min-reviewers 2 \
  --format markdown --output ./.review-insights/{{owner__repo}}/viewpoints.md
```

Then rewrite the Markdown into "Must / Should / Consider" buckets, each item with: imperative title, 1–2 sentence rule, 1 representative quote with `url`, frequency. Drop topics under `min_reviewers` or those that are project noise.

### "Profile reviewer @alice" / "What are @alice's review tendencies?"

```bash
gh review-kit comments stats  --dataset ./.review-insights/{{owner__repo}} \
  --group-by review_state --format json
gh review-kit comments sample --dataset ./.review-insights/{{owner__repo}} \
  --authors alice --per-group 10 --strategy recent \
  --output ./.review-insights/{{owner__repo}}/alice-recent.jsonl
gh review-kit comments sample --dataset ./.review-insights/{{owner__repo}} \
  --authors alice --review-states CHANGES_REQUESTED --per-group 10 --strategy blocking \
  --output ./.review-insights/{{owner__repo}}/alice-blocking.jsonl
gh review-kit comments suggest-rules --dataset ./.review-insights/{{owner__repo}} \
  --review-states CHANGES_REQUESTED --min-reviewers 1 --format json \
  --output ./.review-insights/{{owner__repo}}/alice-candidates.json
```

When ranking topics for one reviewer, set `--min-reviewers 1` (the default of 2 will drop everything). Optionally cross-check with `gh api users/alice` for tenure/team context.

Produce a profile with: review volume, blocking share, favorite paths, signature themes (3–7), and 3 verbatim quotes with `url`.

### "Compare period A vs B" / "Has review style changed?"

Run `extract` twice into separate dataset directories with disjoint `--since` / `--until` ranges, then run `stats` and `suggest-rules` on each. Diff the candidate lists by topic name; highlight topics that appeared, vanished, or changed blocking ratio.

### "Focus on path `internal/` (or any prefix)"

```bash
gh review-kit comments stats   --dataset ./.review-insights/{{owner__repo}} \
  --group-by author --top 20 --format json   # whole repo for context

gh review-kit comments suggest-rules --dataset ./.review-insights/{{owner__repo}} \
  --path internal/ --review-states CHANGES_REQUESTED --min-reviewers 2 \
  --format markdown --output ./.review-insights/{{owner__repo}}/internal-rules.md

gh review-kit comments sample --dataset ./.review-insights/{{owner__repo}} \
  --path internal/ --review-states CHANGES_REQUESTED \
  --group-by author --per-group 5 --strategy blocking \
  --output ./.review-insights/{{owner__repo}}/internal-evidence.jsonl
```

### "Why is PR #123 noisy?"

```bash
gh pr view 123 --repo {{owner/repo}} --json number,title,author,reviews,reviewRequests,files,labels
gh review-kit comments sample --dataset ./.review-insights/{{owner__repo}} \
  --per-group 100 --group-by review_state --format json \
  | jq '[.[] | select(.pr_number==123)]'
```

Combine the diff/files info from `gh pr view` with the per-PR comment slice to narrate the cause.

### "Refresh / re-run with the latest data"

```bash
gh review-kit comments extract --repo {{owner/repo}} \
  --dataset ./.review-insights/{{owner__repo}} --update
```

Then re-run only the steps whose output the user needs — `stats`, `suggest-rules`, `report`, etc. The dataset directory carries over.

## Prompt Templates for the Synthesis Step

Use these when you turn raw artifacts into prose. Keep them grounded in the JSON / JSONL fields and cite `url` for every concrete claim.

### Template A — Repository Trend Narrative (default deliverable)

```text
You are summarizing the review culture of {{owner/repo}}. Use only the data inside <inputs>.
Inputs include: repo metadata, stats by review_state / author / path_prefix / label, ranked
review-topic candidates, blocking-comment evidence (JSONL), and selected gh pr view JSON.

Produce Markdown with these sections (in order):
1. Repository at a glance
2. Review culture (volume, blocking share, top reviewers)
3. Where reviewers focus (paths/labels, with 2–3 quoted comments per hotspot)
4. Recurring review viewpoints (Must / Should / Consider; cite url for every quote)
5. Illustrative PRs (3–5, each with title, link, 1 quote, why it is representative)
6. Open questions and next actions (3 bullets)

Rules:
- Every concrete claim must cite a comment url or a PR url from <inputs>.
- Drop themes not supported by the data; do not invent.
- Keep quotes verbatim, ≤ 200 chars.

<inputs>
{{paste relevant JSON / JSONL artifacts}}
</inputs>
```

### Template B — Reviewer Profile

```text
Profile reviewer @{{author}} on {{owner/repo}}, grounded only in <inputs> (their stats, recent
comments, blocking comments, and per-author rule candidates).

Output Markdown with:
- One-line summary
- Volume and blocking share (numbers from stats JSON)
- Top 3 paths they review
- 3–7 signature review themes (imperative title + 1-sentence rule + 1 verbatim quote with url)
- Notable strengths and possible blind spots (only if supported by data)
- 3 verbatim quotes (≤ 200 chars) showcasing their style

Do not infer personality traits not supported by the comments.

<inputs>
{{paste relevant JSON / JSONL artifacts}}
</inputs>
```

### Template C — Period-over-Period Comparison

```text
Compare review trends in {{owner/repo}} between period A ({{A_since}}..{{A_until}}) and
period B ({{B_since}}..{{B_until}}). Use the two stats / candidate sets in <inputs>.

Output Markdown with:
- Volume delta (PRs and comments)
- Top 5 themes that grew, top 5 that shrank, top 5 new in B
- Blocking-ratio shifts (top 3 increases, top 3 decreases)
- One short narrative paragraph hypothesizing the driver (only if grounded in data)

Cite urls for each concrete example. Do not invent themes that don't appear in candidates.

<inputs>
{{paste both periods' JSON artifacts}}
</inputs>
```

### Template D — Path-Focused Deep Dive

```text
Analyze review feedback for the path prefix `{{path}}` in {{owner/repo}}. Use only <inputs>
(path-filtered candidates, evidence JSONL, and overall stats for context).

Output Markdown:
- Volume and blocking share for the path vs repo overall
- Top 5 reviewers active on this path
- 3–7 recurring viewpoints with 1 verbatim quote + url each
- 3 concrete recommendations (lint rule, refactor, doc, automation)

<inputs>
{{paste artifacts}}
</inputs>
```

## Operational Rules

- **Run, don't ask.** After resolving inputs in the first round, execute every step without further confirmation. Surface errors only when a step fails.
- **Reuse the dataset.** All follow-ups should reuse `./.review-insights/{{owner__repo}}/` unless the user asks for a different repo or window.
- **Be grounded.** Every claim in synthesized Markdown must cite a `url` from the dataset or a PR/issue link from `gh`. If you cannot cite, drop the claim.
- **Stay deterministic.** Prefer `--format json` for machine post-processing and pin `--seed` when sampling randomly.
- **Mind the rate limit.** If `gh api` calls in steps 1 and 7 risk depleting the budget, drop step 7 (`gh pr view`) first; it is the only optional step.
- **GHES.** Steps 1, 2, 3, and 7 call the GitHub API. When the target is a GitHub Enterprise Server instance, specify the host as part of `--repo` (e.g. `--repo ghes.example.com/owner/repo`); no `GH_HOST` environment variable is needed. Steps 4, 5, 6, and 8 operate only on local dataset files — they never make network requests and do not require any host specification. For follow-up instructions that only run analysis subcommands (`stats`, `sample`, `bundle`, `suggest-rules`, `report`), host specification is irrelevant.

## References

- Companion skill: `gh-review-kit-comments` (full reference for the `comments` subcommands)
- Companion skill: `gh-review-kit` (`checks`, `rerequest`)
- gh CLI manual: <https://cli.github.com/manual/>
