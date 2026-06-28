package comments

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Topic represents a candidate review-rule topic to detect via regex matching
// against comment bodies.
type Topic struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Patterns    []string `json:"patterns" yaml:"patterns"`
	compiled    []*regexp.Regexp
}

// TopicSet is a deterministic ordered collection of topics.
type TopicSet struct {
	Topics []*Topic `json:"topics" yaml:"topics"`
}

func (t *TopicSet) compile() error {
	for _, topic := range t.Topics {
		topic.compiled = topic.compiled[:0]
		for _, p := range topic.Patterns {
			re, err := regexp.Compile("(?i)" + p)
			if err != nil {
				return fmt.Errorf("topic %q: invalid pattern %q: %w", topic.Name, p, err)
			}
			topic.compiled = append(topic.compiled, re)
		}
	}
	return nil
}

// LoadTopicSet reads a topic dictionary from disk. Only JSON (.json) is
// supported. To keep dependencies minimal, YAML files (.yaml/.yml) are not
// parsed and are rejected with a clear message.
func LoadTopicSet(path string) (*TopicSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read topics file: %w", err)
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return nil, fmt.Errorf("YAML topics files are not yet supported; please convert %s to JSON", path)
	}
	var ts TopicSet
	if err := json.Unmarshal(b, &ts); err != nil {
		return nil, fmt.Errorf("failed to parse topics file: %w", err)
	}
	if len(ts.Topics) == 0 {
		return nil, fmt.Errorf("topics file %s contains no topics", path)
	}
	if err := ts.compile(); err != nil {
		return nil, err
	}
	return &ts, nil
}

// DefaultTopics returns a small built-in dictionary covering common review
// patterns. Patterns are deliberately broad keyword matches because the
// command's job is to surface candidates for human review, not to be a
// classifier.
func DefaultTopics() *TopicSet {
	ts := &TopicSet{
		Topics: []*Topic{
			{
				Name:        "naming",
				Description: "Naming conventions for variables, functions, files",
				Patterns:    []string{`\bnaming\b`, `\brename\b`, `\bbetter name\b`, `\bvariable name\b`, `\bfunction name\b`, `\btypo\b`},
			},
			{
				Name:        "error_handling",
				Description: "Error wrapping, propagation, and messages",
				Patterns:    []string{`\berror handling\b`, `\bwrap\b.*\berror\b`, `\bfmt\.Errorf\b`, `\bswallow(ed)? error\b`, `\bignore(d|s)? error\b`, `\bpanic\b`},
			},
			{
				Name:        "tests",
				Description: "Missing or insufficient tests",
				Patterns:    []string{`\badd(ing)? (a )?test\b`, `\btest case\b`, `\bunit test\b`, `\bcoverage\b`, `\bmissing test\b`},
			},
			{
				Name:        "comments_and_docs",
				Description: "Code comments, godoc, and documentation",
				Patterns:    []string{`\bgodoc\b`, `\bcomment\b.*\bplease\b`, `\bdocument(ation)?\b`, `\bdocstring\b`},
			},
			{
				Name:        "security",
				Description: "Potential security or secret-handling issues",
				Patterns:    []string{`\bsecret\b`, `\btoken\b`, `\bcredential\b`, `\bpassword\b`, `\binjection\b`, `\bSQL\b.*\binjection\b`, `\bauth(entication|orization)?\b`, `\bsanitiz(e|ation)\b`},
			},
			{
				Name:        "performance",
				Description: "Performance, complexity, allocation",
				Patterns:    []string{`\bperformance\b`, `\bO\([0-9a-z]+\)`, `\bN\+1\b`, `\ballocat(e|ion)\b`, `\bslow\b`, `\binefficient\b`},
			},
			{
				Name:        "concurrency",
				Description: "Concurrency, race conditions, contexts",
				Patterns:    []string{`\brace condition\b`, `\bdata race\b`, `\bgoroutine\b`, `\bcontext\.Context\b`, `\bdead ?lock\b`, `\bmutex\b`},
			},
			{
				Name:        "style_and_formatting",
				Description: "Code style, formatting, and lint findings",
				Patterns:    []string{`\bgofmt\b`, `\bgo vet\b`, `\bstaticcheck\b`, `\blint\b`, `\bformatting\b`, `\bstyle\b`},
			},
			{
				Name:        "logging",
				Description: "Logging level, message quality, structured fields",
				Patterns:    []string{`\blog(ging|ger)?\b.*\b(level|message|field)\b`, `\bstructured log\b`, `\bDEBUG\b`, `\bINFO\b`, `\bERROR\b`},
			},
			{
				Name:        "api_design",
				Description: "Public API design, exported names, breaking changes",
				Patterns:    []string{`\bAPI\b`, `\bbreaking change\b`, `\bexported\b`, `\bbackwards? compatib(le|ility)\b`, `\binterface\b`},
			},
		},
	}
	if err := ts.compile(); err != nil {
		// Patterns are constants; compile errors here are programming errors.
		panic(err)
	}
	return ts
}

// SuggestRulesOptions configures SuggestRules.
type SuggestRulesOptions struct {
	Topics       *TopicSet
	Filters      SampleFilters
	MinCount     int
	MinReviewers int
	Examples     int
}

// RuleExample is one source comment cited as evidence.
type RuleExample struct {
	Repo        string `json:"repo"`
	PRNumber    int    `json:"pr_number"`
	URL         string `json:"url,omitempty"`
	Author      string `json:"author,omitempty"`
	ReviewState string `json:"review_state,omitempty"`
	Body        string `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
}

// RuleCandidate is one ranked rule candidate.
type RuleCandidate struct {
	Topic             string        `json:"topic"`
	Description       string        `json:"description,omitempty"`
	Count             int           `json:"count"`
	DistinctReviewers int           `json:"distinct_reviewers"`
	DistinctRepos     int           `json:"distinct_repos"`
	BlockingCount     int           `json:"blocking_count"`
	BlockingShare     float64       `json:"blocking_share"`
	LatestAt          time.Time     `json:"latest_at"`
	Examples          []RuleExample `json:"examples,omitempty"`
}

// SuggestRulesResult is the result of SuggestRules.
type SuggestRulesResult struct {
	Dataset    string           `json:"dataset"`
	Topics     int              `json:"topics"`
	Candidates []RuleCandidate  `json:"candidates"`
}

type ruleAcc struct {
	count     int
	reviewers map[string]struct{}
	repos     map[string]struct{}
	blocking  int
	latest    time.Time
	examples  []RuleExample
}

// SuggestRules ranks topics by frequency and other deterministic signals,
// emitting a list of candidate rules with evidence URLs. No clustering or
// embedding is performed; matches are based on the supplied or default
// regex/keyword dictionary.
func SuggestRules(dir string, opts SuggestRulesOptions) (*SuggestRulesResult, error) {
	topics := opts.Topics
	if topics == nil {
		topics = DefaultTopics()
	} else if err := topics.compile(); err != nil {
		return nil, err
	}
	if opts.Examples <= 0 {
		opts.Examples = 3
	}

	acc := make(map[string]*ruleAcc, len(topics.Topics))
	for _, t := range topics.Topics {
		acc[t.Name] = &ruleAcc{
			reviewers: map[string]struct{}{},
			repos:     map[string]struct{}{},
		}
	}

	if err := IterateComments(dir, func(c *Comment) error {
		if !sampleMatches(c, opts.Filters) {
			return nil
		}
		body := c.Body
		for _, t := range topics.Topics {
			if !topicMatches(t, body) {
				continue
			}
			a := acc[t.Name]
			a.count++
			if c.Author != "" {
				a.reviewers[c.Author] = struct{}{}
			}
			a.repos[c.Repo] = struct{}{}
			if c.ReviewState == "CHANGES_REQUESTED" {
				a.blocking++
			}
			if c.CreatedAt.After(a.latest) {
				a.latest = c.CreatedAt
			}
			ex := RuleExample{
				Repo:        c.Repo,
				PRNumber:    c.PRNumber,
				URL:         c.URL,
				Author:      c.Author,
				ReviewState: c.ReviewState,
				Body:        c.Body,
				CreatedAt:   c.CreatedAt,
			}
			// Keep the most recent opts.Examples evidence items. Examples are
			// stored sorted ascending by CreatedAt so a[0] is always the oldest
			// kept item and can be replaced when a newer match arrives.
			if len(a.examples) < opts.Examples {
				a.examples = append(a.examples, ex)
				sort.SliceStable(a.examples, func(i, j int) bool {
					return a.examples[i].CreatedAt.Before(a.examples[j].CreatedAt)
				})
			} else if ex.CreatedAt.After(a.examples[0].CreatedAt) {
				a.examples[0] = ex
				sort.SliceStable(a.examples, func(i, j int) bool {
					return a.examples[i].CreatedAt.Before(a.examples[j].CreatedAt)
				})
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	candidates := make([]RuleCandidate, 0, len(topics.Topics))
	for _, t := range topics.Topics {
		a := acc[t.Name]
		if a.count < opts.MinCount {
			continue
		}
		if len(a.reviewers) < opts.MinReviewers {
			continue
		}
		share := 0.0
		if a.count > 0 {
			share = float64(a.blocking) / float64(a.count)
		}
		// Examples ordered by recency for predictability.
		sort.SliceStable(a.examples, func(i, j int) bool { return a.examples[i].CreatedAt.After(a.examples[j].CreatedAt) })
		candidates = append(candidates, RuleCandidate{
			Topic:             t.Name,
			Description:       t.Description,
			Count:             a.count,
			DistinctReviewers: len(a.reviewers),
			DistinctRepos:     len(a.repos),
			BlockingCount:     a.blocking,
			BlockingShare:     share,
			LatestAt:          a.latest,
			Examples:          a.examples,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		// Score: count weighted by distinct reviewers and blocking share.
		si := scoreCandidate(candidates[i])
		sj := scoreCandidate(candidates[j])
		if si != sj {
			return si > sj
		}
		return candidates[i].Topic < candidates[j].Topic
	})

	return &SuggestRulesResult{
		Dataset:    AbsDataset(dir),
		Topics:     len(topics.Topics),
		Candidates: candidates,
	}, nil
}

func scoreCandidate(c RuleCandidate) float64 {
	// Deterministic ranking signal: frequency, distinct reviewers, blocking share.
	return float64(c.Count) + float64(c.DistinctReviewers)*2 + float64(c.DistinctRepos) + c.BlockingShare*float64(c.Count)
}

func topicMatches(t *Topic, body string) bool {
	for _, re := range t.compiled {
		if re.MatchString(body) {
			return true
		}
	}
	return false
}
