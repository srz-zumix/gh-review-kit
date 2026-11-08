package copilot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// SourceFilterNone matches comments that report no Copilot code review source.
const SourceFilterNone = "none"

// sourceKindAliases maps the accepted --source kind spellings to API kinds.
var sourceKindAliases = map[string]gh.CopilotCodeReviewSourceKind{
	"skill":               gh.CopilotCodeReviewSourceKindSkill,
	"mcp":                 gh.CopilotCodeReviewSourceKindMCP,
	"custom_instructions": gh.CopilotCodeReviewSourceKindCustomInstructions,
	"custom-instructions": gh.CopilotCodeReviewSourceKindCustomInstructions,
	"instructions":        gh.CopilotCodeReviewSourceKindCustomInstructions,
}

// SourceFilterKinds lists the kinds accepted by ParseSourceFilters, for completion.
var SourceFilterKinds = []string{"skill", "mcp", "custom_instructions", SourceFilterNone}

// SourceFilter matches Copilot code review sources by kind and, optionally,
// by name or repository-relative path. None matches comments without sources.
type SourceFilter struct {
	Kind gh.CopilotCodeReviewSourceKind
	Name string
	None bool
}

// ParseSourceFilters parses --source values of the form KIND, KIND:NAME, or
// "none". KIND is skill, mcp, or custom_instructions (case-insensitive); NAME
// matches the source name or path case-insensitively.
func ParseSourceFilters(values []string) ([]SourceFilter, error) {
	var filters []SourceFilter
	for _, v := range values {
		v = strings.TrimSpace(v)
		if strings.EqualFold(v, SourceFilterNone) {
			filters = append(filters, SourceFilter{None: true})
			continue
		}
		kindText, name, _ := strings.Cut(v, ":")
		kind, ok := sourceKindAliases[strings.ToLower(strings.TrimSpace(kindText))]
		if !ok {
			return nil, fmt.Errorf("invalid source filter %q: kind must be one of %s", v, strings.Join(SourceFilterKinds, ", "))
		}
		filters = append(filters, SourceFilter{Kind: kind, Name: strings.TrimSpace(name)})
	}
	return filters, nil
}

// Matches reports whether sources satisfy the filter.
func (f SourceFilter) Matches(sources []gh.CopilotCodeReviewSource) bool {
	if f.None {
		return len(sources) == 0
	}
	for _, s := range sources {
		if s.Kind != f.Kind {
			continue
		}
		if f.Name == "" || strings.EqualFold(s.Name, f.Name) || strings.EqualFold(s.Path, f.Name) {
			return true
		}
	}
	return false
}

// FilterCommentsBySource keeps comments matching at least one filter. With no
// filters every comment is kept.
func FilterCommentsBySource(comments []*Comment, filters []SourceFilter) []*Comment {
	if len(filters) == 0 {
		return comments
	}
	var kept []*Comment
	for _, c := range comments {
		for _, f := range filters {
			if f.Matches(c.Sources) {
				kept = append(kept, c)
				break
			}
		}
	}
	return kept
}

// attachAttribution copies the attribution fields of REST review comments onto
// the matching comments by comment ID.
func attachAttribution(comments []*Comment, attributed []*gh.CopilotAttributedPullRequestComment) {
	byID := make(map[int64]*gh.CopilotAttributedPullRequestComment, len(attributed))
	for _, a := range attributed {
		byID[a.GetID()] = a
	}
	for _, c := range comments {
		a, ok := byID[c.CommentID]
		if !ok {
			continue
		}
		c.IsCopilotCodeReview = a.IsCopilotCodeReview
		c.Sources = a.CopilotCodeReviewSources
	}
}

// FormatSource renders a source as KIND:NAME. Custom instructions all share
// the name "custom instructions", so their path is used when reported.
func FormatSource(s gh.CopilotCodeReviewSource) string {
	return s.String()
}

// SourceVerdictSummary counts evaluation verdicts for comments that report a
// given Copilot code review source.
type SourceVerdictSummary struct {
	// Source is KIND:NAME, or "none" for comments without a reported source.
	Source  string `json:"source"`
	Total   int    `json:"total"`
	Valid   int    `json:"valid"`
	Invalid int    `json:"invalid"`
	Unclear int    `json:"unclear"`
	// Failed counts comments without a verdict, e.g. after an evaluation error.
	Failed int `json:"failed"`
}

// ValidRate is the share of judged comments that were valid, or 0 when none were judged.
func (s SourceVerdictSummary) ValidRate() float64 {
	judged := s.Valid + s.Invalid + s.Unclear
	if judged == 0 {
		return 0
	}
	return float64(s.Valid) / float64(judged)
}

// SummarizeVerdictsBySource counts verdicts per source. A comment with several
// sources counts toward each of them, so totals may exceed the result count.
// Rows are ordered by total descending, then by source.
func SummarizeVerdictsBySource(results []*EvaluationResult) []SourceVerdictSummary {
	index := map[string]int{}
	var rows []SourceVerdictSummary
	add := func(key string, res *EvaluationResult) {
		i, ok := index[key]
		if !ok {
			i = len(rows)
			index[key] = i
			rows = append(rows, SourceVerdictSummary{Source: key})
		}
		row := &rows[i]
		row.Total++
		if res.Evaluation == nil {
			row.Failed++
			return
		}
		switch res.Evaluation.Verdict {
		case VerdictValid:
			row.Valid++
		case VerdictInvalid:
			row.Invalid++
		case VerdictUnclear:
			row.Unclear++
		default:
			row.Failed++
		}
	}
	for _, res := range results {
		if res == nil || res.Comment == nil {
			continue
		}
		if len(res.Comment.Sources) == 0 {
			add(SourceFilterNone, res)
			continue
		}
		seen := map[string]bool{}
		for _, s := range res.Comment.Sources {
			key := FormatSource(s)
			if seen[key] {
				continue
			}
			seen[key] = true
			add(key, res)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Total != rows[j].Total {
			return rows[i].Total > rows[j].Total
		}
		return rows[i].Source < rows[j].Source
	})
	return rows
}

// hasAttributedSource reports whether any row is for an actual source rather than "none".
func hasAttributedSource(rows []SourceVerdictSummary) bool {
	for _, r := range rows {
		if r.Source != SourceFilterNone {
			return true
		}
	}
	return false
}
