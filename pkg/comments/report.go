package comments

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ReportOptions configures Report.
type ReportOptions struct {
	Topics       *TopicSet
	Filters      SampleFilters
	MinCount     int
	MinReviewers int
	Examples     int
	StatsTop     int // top-N rows to include per stats slice
}

// Report is a combined human-readable summary of the dataset.
type Report struct {
	Dataset    string             `json:"dataset"`
	GeneratedAt time.Time         `json:"generated_at"`
	Manifest   *Manifest          `json:"manifest,omitempty"`
	Stats      map[string]*StatsResult `json:"stats"`
	Rules      *SuggestRulesResult `json:"rules"`
}

// BuildReport assembles a report bundle (stats slices + rule candidates) from a
// dataset. The stats rows and rule candidates are produced in a deterministic
// order; only the GeneratedAt timestamp varies between otherwise identical
// runs.
func BuildReport(dir string, opts ReportOptions) (*Report, error) {
	if opts.StatsTop <= 0 {
		opts.StatsTop = 20
	}
	manifest, err := LoadManifest(dir)
	if err != nil {
		return nil, err
	}
	statsKeys := []string{"comment_type", "review_state", "author", "path_prefix", "repo"}
	statsOut := map[string]*StatsResult{}
	for _, k := range statsKeys {
		s, err := Stats(dir, StatsOptions{GroupBy: k, Top: opts.StatsTop, MinCount: opts.MinCount, Filters: opts.Filters})
		if err != nil {
			return nil, fmt.Errorf("stats(%s): %w", k, err)
		}
		statsOut[k] = s
	}
	rules, err := SuggestRules(dir, SuggestRulesOptions{
		Topics:       opts.Topics,
		Filters:      opts.Filters,
		MinCount:     opts.MinCount,
		MinReviewers: opts.MinReviewers,
		Examples:     opts.Examples,
	})
	if err != nil {
		return nil, fmt.Errorf("suggest-rules: %w", err)
	}
	return &Report{
		Dataset:     AbsDataset(dir),
		GeneratedAt: time.Now().UTC(),
		Manifest:    manifest,
		Stats:       statsOut,
		Rules:       rules,
	}, nil
}

// WriteReportJSON encodes the report as indented JSON.
func WriteReportJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteReportMarkdown writes a human-readable Markdown report.
func WriteReportMarkdown(w io.Writer, r *Report) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Comments Report\n\n")
	fmt.Fprintf(&b, "- **Dataset**: `%s`\n", r.Dataset)
	if !r.GeneratedAt.IsZero() {
		fmt.Fprintf(&b, "- **Generated**: %s\n", r.GeneratedAt.Format(time.RFC3339))
	}
	if r.Manifest != nil {
		c := r.Manifest.Counts
		fmt.Fprintf(&b, "- **Counts**: %d PRs, %d review bodies, %d review comments, %d issue comments\n",
			c.PRs, c.ReviewBodies, c.ReviewComments, c.IssueComments)
		if len(r.Manifest.Filters.Repos) > 0 {
			fmt.Fprintf(&b, "- **Repos**: %s\n", strings.Join(r.Manifest.Filters.Repos, ", "))
		}
	}
	b.WriteString("\n## Top Rule Candidates\n\n")
	if r.Rules == nil || len(r.Rules.Candidates) == 0 {
		b.WriteString("_No candidates met the configured thresholds._\n")
	} else {
		b.WriteString("| Topic | Count | Reviewers | Repos | Blocking | Blocking% | Latest |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | --- |\n")
		for _, c := range r.Rules.Candidates {
			latest := ""
			if !c.LatestAt.IsZero() {
				latest = c.LatestAt.Format("2006-01-02")
			}
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %.1f%% | %s |\n",
				tableCell(c.Topic), c.Count, c.DistinctReviewers, c.DistinctRepos, c.BlockingCount, c.BlockingShare*100, latest)
		}
		b.WriteString("\n### Evidence\n\n")
		for _, c := range r.Rules.Candidates {
			fmt.Fprintf(&b, "#### %s", c.Topic)
			if c.Description != "" {
				fmt.Fprintf(&b, " — %s", c.Description)
			}
			b.WriteString("\n\n")
			for _, ex := range c.Examples {
				ref := fmt.Sprintf("%s#%d", ex.Repo, ex.PRNumber)
				if ex.URL != "" {
					ref = fmt.Sprintf("[%s#%d](%s)", ex.Repo, ex.PRNumber, ex.URL)
				}
				fmt.Fprintf(&b, "- %s by `%s`", ref, ex.Author)
				if ex.ReviewState != "" {
					fmt.Fprintf(&b, " (%s)", ex.ReviewState)
				}
				fmt.Fprintf(&b, ": %s\n", oneLine(ex.Body))
			}
			b.WriteString("\n")
		}
	}

	statsKeys := []string{"comment_type", "review_state", "author", "path_prefix", "repo"}
	hasStats := false
	for _, key := range statsKeys {
		if s, ok := r.Stats[key]; ok && s != nil && len(s.Rows) > 0 {
			hasStats = true
			break
		}
	}
	if hasStats {
		b.WriteString("## Stats\n\n")
		for _, key := range statsKeys {
			s, ok := r.Stats[key]
			if !ok || s == nil || len(s.Rows) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### %s\n\n", key)
			b.WriteString("| Key | Count | Reviewers | Repos | Blocking |\n")
			b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
			for _, row := range s.Rows {
				fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", tableCell(row.Key), row.Count, row.Reviewers, row.Repos, row.Blocking)
			}
			b.WriteString("\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// tableCell escapes a value so it is safe to embed in a Markdown table cell.
func tableCell(s string) string { return oneLine(s) }

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.TrimSpace(s)
	return s
}
