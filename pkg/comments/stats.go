package comments

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ValidationReport summarizes issues found by Validate.
type ValidationReport struct {
	Comments     int      `json:"comments"`
	PRs          int      `json:"prs"`
	DuplicateIDs int      `json:"duplicate_ids"`
	Issues       []string `json:"issues,omitempty"`
}

// Validate walks the dataset's JSONL files and reports schema issues. It does
// not mutate the dataset.
func Validate(dir string) (*ValidationReport, error) {
	if _, err := LoadManifest(dir); err != nil {
		return nil, err
	}
	r := &ValidationReport{}
	seen := map[string]struct{}{}
	prSeen := map[string]struct{}{}
	// Referenced PR keys collected from comments so linkage can be verified
	// after prSeen is fully populated by the PR pass below.
	refCount := map[string]int{}
	refComment := map[string]int64{}

	if err := IterateComments(dir, func(c *Comment) error {
		r.Comments++
		if c.SchemaVersion != SchemaVersion {
			r.Issues = append(r.Issues, fmt.Sprintf("comment id=%d unsupported schema_version=%d", c.ID, c.SchemaVersion))
		}
		if c.ID == 0 {
			r.Issues = append(r.Issues, "comment with id=0 (missing identifier)")
		}
		if c.Type == "" {
			r.Issues = append(r.Issues, fmt.Sprintf("comment id=%d missing type", c.ID))
		}
		if c.Repo == "" || c.PRNumber == 0 {
			r.Issues = append(r.Issues, fmt.Sprintf("comment id=%d missing repo/pr linkage", c.ID))
		} else {
			key := prKey(c.Repo, c.PRNumber)
			if _, ok := refComment[key]; !ok {
				refComment[key] = c.ID
			}
			refCount[key]++
		}
		if c.CreatedAt.IsZero() {
			r.Issues = append(r.Issues, fmt.Sprintf("comment id=%d missing created_at", c.ID))
		}
		key := fmt.Sprintf("%s|%d", c.Type, c.ID)
		if _, dup := seen[key]; dup {
			r.DuplicateIDs++
			r.Issues = append(r.Issues, fmt.Sprintf("duplicate comment %s", key))
		}
		seen[key] = struct{}{}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := IteratePRs(dir, func(p *PR) error {
		r.PRs++
		if p.SchemaVersion != SchemaVersion {
			r.Issues = append(r.Issues, fmt.Sprintf("pr %s#%d unsupported schema_version=%d", p.Repo, p.Number, p.SchemaVersion))
		}
		if p.Repo == "" || p.Number == 0 {
			r.Issues = append(r.Issues, "pr record missing repo/number")
		}
		key := prKey(p.Repo, p.Number)
		if _, dup := prSeen[key]; dup {
			r.Issues = append(r.Issues, fmt.Sprintf("duplicate pr %s", key))
		}
		prSeen[key] = struct{}{}
		return nil
	}); err != nil {
		return nil, err
	}

	// Verify each comment references a PR record present in prs.jsonl. One issue
	// is reported per missing PR (deduped), sorted for deterministic output.
	missing := make([]string, 0)
	for key := range refCount {
		if _, ok := prSeen[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		r.Issues = append(r.Issues, fmt.Sprintf("comment id=%d references missing pr %s (%d comment(s))", refComment[key], key, refCount[key]))
	}
	return r, nil
}

// prKey builds the canonical "repo|number" key used to link comments to PR
// records. Callers must use the exact repo string (no case normalization) so it
// matches how other commands join comments and PRs.
func prKey(repo string, number int) string {
	return fmt.Sprintf("%s|%d", repo, number)
}

// StatsOptions configures Stats.
type StatsOptions struct {
	GroupBy  string // repo|author|review_state|comment_type|path_prefix|label
	Top      int
	MinCount int
	Filters  SampleFilters
}

// StatRow is one aggregated row.
type StatRow struct {
	Key        string  `json:"key"`
	Count      int     `json:"count"`
	Reviewers  int     `json:"reviewers,omitempty"`
	Repos      int     `json:"repos,omitempty"`
	Blocking   int     `json:"changes_requested,omitempty"`
	BotShare   float64 `json:"bot_share,omitempty"`
	ExampleURL string  `json:"example_url,omitempty"`
}

// StatsResult is the result of Stats.
type StatsResult struct {
	Dataset string    `json:"dataset"`
	GroupBy string    `json:"group_by"`
	Rows    []StatRow `json:"rows"`
}

type statAcc struct {
	count      int
	repos      map[string]struct{}
	reviewers  map[string]struct{}
	blocking   int
	bot        int
	exampleURL string
}

// Stats aggregates the dataset by the given group key.
func Stats(dir string, opts StatsOptions) (*StatsResult, error) {
	if opts.GroupBy == "" {
		opts.GroupBy = "comment_type"
	}
	keyFn, err := groupKeyFn(opts.GroupBy)
	if err != nil {
		return nil, err
	}
	// Pre-load PR labels so the "label" group can fan out per label.
	prLabels := map[string][]string{}
	if opts.GroupBy == "label" {
		if err := IteratePRs(dir, func(p *PR) error {
			prLabels[fmt.Sprintf("%s#%d", p.Repo, p.Number)] = p.Labels
			return nil
		}); err != nil {
			return nil, err
		}
	}

	groups := map[string]*statAcc{}
	if err := IterateComments(dir, func(c *Comment) error {
		if !sampleMatches(c, opts.Filters) {
			return nil
		}
		keys := keyFn(c, prLabels)
		for _, k := range keys {
			if k == "" {
				// Bucket records with empty metadata (e.g. missing author) as
				// "(none)" instead of dropping them, matching how review_state
				// and label groupings already handle empties.
				k = "(none)"
			}
			acc, ok := groups[k]
			if !ok {
				acc = &statAcc{repos: map[string]struct{}{}, reviewers: map[string]struct{}{}}
				groups[k] = acc
			}
			acc.count++
			acc.repos[c.Repo] = struct{}{}
			if c.Author != "" {
				acc.reviewers[c.Author] = struct{}{}
			}
			if c.ReviewState == "CHANGES_REQUESTED" {
				acc.blocking++
			}
			if c.AuthorIsBot {
				acc.bot++
			}
			if acc.exampleURL == "" {
				acc.exampleURL = c.URL
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	rows := make([]StatRow, 0, len(groups))
	for k, acc := range groups {
		if acc.count < opts.MinCount {
			continue
		}
		row := StatRow{
			Key:        k,
			Count:      acc.count,
			Reviewers:  len(acc.reviewers),
			Repos:      len(acc.repos),
			Blocking:   acc.blocking,
			ExampleURL: acc.exampleURL,
		}
		if acc.count > 0 {
			row.BotShare = float64(acc.bot) / float64(acc.count)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	if opts.Top > 0 && len(rows) > opts.Top {
		rows = rows[:opts.Top]
	}
	return &StatsResult{Dataset: dir, GroupBy: opts.GroupBy, Rows: rows}, nil
}

func groupKeyFn(group string) (func(c *Comment, prLabels map[string][]string) []string, error) {
	switch group {
	case "comment_type":
		return func(c *Comment, _ map[string][]string) []string { return []string{string(c.Type)} }, nil
	case "repo":
		return func(c *Comment, _ map[string][]string) []string { return []string{c.Repo} }, nil
	case "author":
		return func(c *Comment, _ map[string][]string) []string { return []string{c.Author} }, nil
	case "review_state":
		return func(c *Comment, _ map[string][]string) []string {
			if c.ReviewState == "" {
				return []string{"(none)"}
			}
			return []string{c.ReviewState}
		}, nil
	case "path_prefix":
		return func(c *Comment, _ map[string][]string) []string {
			if c.Path == "" {
				return nil
			}
			parts := strings.SplitN(c.Path, "/", 2)
			return []string{parts[0]}
		}, nil
	case "label":
		return func(c *Comment, prLabels map[string][]string) []string {
			labels := prLabels[fmt.Sprintf("%s#%d", c.Repo, c.PRNumber)]
			if len(labels) == 0 {
				return []string{"(none)"}
			}
			return labels
		}, nil
	default:
		return nil, fmt.Errorf("unknown group-by %q (allowed: comment_type, repo, author, review_state, path_prefix, label)", group)
	}
}

// AbsDataset returns an absolute path for display, falling back to the input
// when filepath.Abs fails.
func AbsDataset(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}
