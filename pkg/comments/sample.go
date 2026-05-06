package comments

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// SampleStrategy controls how Sample selects records inside each group.
type SampleStrategy string

const (
	SampleStrategyRecent         SampleStrategy = "recent"
	SampleStrategyDiverseAuthors SampleStrategy = "diverse-authors"
	SampleStrategyBlocking       SampleStrategy = "blocking"
	SampleStrategyRandom         SampleStrategy = "random"
)

// SampleFilters narrows the corpus before grouping/sampling.
type SampleFilters struct {
	CommentTypes []CommentType
	ReviewStates []string
	Authors      []string
	PathPrefixes []string
	Since        *time.Time
	Until        *time.Time
	MinLength    int
	IncludeBots  bool
}

// SampleOptions configures Sample.
type SampleOptions struct {
	Filters  SampleFilters
	GroupBy  string // empty = single global group ""
	PerGroup int    // default 5
	Strategy SampleStrategy
	Seed     int64
}

// Sample picks representative comments from the dataset.
func Sample(dir string, opts SampleOptions) ([]*Comment, error) {
	if opts.PerGroup <= 0 {
		opts.PerGroup = 5
	}
	if opts.Strategy == "" {
		opts.Strategy = SampleStrategyRecent
	}
	groupKey, err := sampleGroupFn(opts.GroupBy)
	if err != nil {
		return nil, err
	}

	prLabels := map[string][]string{}
	if opts.GroupBy == "label" {
		if err := IteratePRs(dir, func(p *PR) error {
			prLabels[fmt.Sprintf("%s#%d", p.Repo, p.Number)] = p.Labels
			return nil
		}); err != nil {
			return nil, err
		}
	}

	groups := map[string][]*Comment{}
	if err := IterateComments(dir, func(c *Comment) error {
		if !sampleMatches(c, opts.Filters) {
			return nil
		}
		for _, k := range groupKey(c, prLabels) {
			cp := *c
			groups[k] = append(groups[k], &cp)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var rng *rand.Rand
	if opts.Strategy == SampleStrategyRandom {
		seed := opts.Seed
		if seed == 0 {
			seed = time.Now().UnixNano()
		}
		// nolint:gosec // sample selection is not security-sensitive
		rng = rand.New(rand.NewPCG(uint64(seed), uint64(seed)^0x9E3779B97F4A7C15))
	}

	out := []*Comment{}
	for _, k := range keys {
		picked := pickFromGroup(groups[k], opts.Strategy, opts.PerGroup, rng)
		out = append(out, picked...)
	}
	return out, nil
}

func pickFromGroup(items []*Comment, strategy SampleStrategy, n int, rng *rand.Rand) []*Comment {
	switch strategy {
	case SampleStrategyBlocking:
		filtered := items[:0:0]
		for _, c := range items {
			if c.ReviewState == "CHANGES_REQUESTED" {
				filtered = append(filtered, c)
			}
		}
		sortByCreatedDesc(filtered)
		return takeN(filtered, n)
	case SampleStrategyDiverseAuthors:
		sortByCreatedDesc(items)
		seen := map[string]bool{}
		out := []*Comment{}
		for _, c := range items {
			if seen[c.Author] {
				continue
			}
			seen[c.Author] = true
			out = append(out, c)
			if len(out) >= n {
				break
			}
		}
		return out
	case SampleStrategyRandom:
		idx := make([]int, len(items))
		for i := range idx {
			idx[i] = i
		}
		rng.Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
		out := make([]*Comment, 0, min(n, len(items)))
		for _, i := range idx[:min(n, len(items))] {
			out = append(out, items[i])
		}
		return out
	default: // recent
		sortByCreatedDesc(items)
		return takeN(items, n)
	}
}

func sortByCreatedDesc(items []*Comment) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}

func takeN(items []*Comment, n int) []*Comment {
	if len(items) <= n {
		out := make([]*Comment, len(items))
		copy(out, items)
		return out
	}
	out := make([]*Comment, n)
	copy(out, items[:n])
	return out
}

func sampleMatches(c *Comment, f SampleFilters) bool {
	if !f.IncludeBots && c.AuthorIsBot {
		return false
	}
	if f.MinLength > 0 && len(strings.TrimSpace(c.Body)) < f.MinLength {
		return false
	}
	if f.Since != nil && c.CreatedAt.Before(*f.Since) {
		return false
	}
	if f.Until != nil && c.CreatedAt.After(*f.Until) {
		return false
	}
	if len(f.CommentTypes) > 0 && !containsCT(f.CommentTypes, c.Type) {
		return false
	}
	if len(f.ReviewStates) > 0 && !containsString(f.ReviewStates, c.ReviewState) {
		return false
	}
	if len(f.Authors) > 0 && !containsString(f.Authors, c.Author) {
		return false
	}
	if len(f.PathPrefixes) > 0 {
		if c.Path == "" {
			return false
		}
		matched := false
		for _, p := range f.PathPrefixes {
			if strings.HasPrefix(c.Path, p) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func sampleGroupFn(group string) (func(c *Comment, prLabels map[string][]string) []string, error) {
	if group == "" {
		return func(c *Comment, _ map[string][]string) []string { return []string{""} }, nil
	}
	return groupKeyFn(group)
}

func containsCT(set []CommentType, v CommentType) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func containsString(set []string, v string) bool {
	for _, s := range set {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}
