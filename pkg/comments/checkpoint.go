package comments

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Checkpoint records which PRs have been fully processed so a resumed
// extraction can skip them.
type Checkpoint struct {
	SchemaVersion int                        `json:"schema_version"`
	Repos         map[string]*RepoCheckpoint `json:"repos"`
}

// RepoCheckpoint tracks per-repository progress.
type RepoCheckpoint struct {
	CompletedPRs  []int               `json:"completed_prs"`
	PRUpdatedAt   map[int]time.Time   `json:"pr_updated_at,omitempty"`
	LastUpdatedAt time.Time           `json:"last_updated_at,omitempty"`
	completedSet  map[int]struct{}
}

func (c *Checkpoint) markDone(repo string, number int, updatedAt time.Time) {
	if c.Repos == nil {
		c.Repos = map[string]*RepoCheckpoint{}
	}
	rc, ok := c.Repos[repo]
	if !ok {
		rc = &RepoCheckpoint{completedSet: map[int]struct{}{}}
		c.Repos[repo] = rc
	}
	if rc.completedSet == nil {
		rc.completedSet = map[int]struct{}{}
		for _, n := range rc.CompletedPRs {
			rc.completedSet[n] = struct{}{}
		}
	}
	if _, dup := rc.completedSet[number]; !dup {
		rc.completedSet[number] = struct{}{}
		rc.CompletedPRs = append(rc.CompletedPRs, number)
	}
	if rc.PRUpdatedAt == nil {
		rc.PRUpdatedAt = map[int]time.Time{}
	}
	if prev, ok := rc.PRUpdatedAt[number]; !ok || updatedAt.After(prev) {
		rc.PRUpdatedAt[number] = updatedAt
	}
	if updatedAt.After(rc.LastUpdatedAt) {
		rc.LastUpdatedAt = updatedAt
	}
}

func (c *Checkpoint) isDone(repo string, number int) bool {
	if c == nil || c.Repos == nil {
		return false
	}
	rc, ok := c.Repos[repo]
	if !ok {
		return false
	}
	if rc.completedSet == nil {
		rc.completedSet = map[int]struct{}{}
		for _, n := range rc.CompletedPRs {
			rc.completedSet[n] = struct{}{}
		}
	}
	_, ok = rc.completedSet[number]
	return ok
}

// prUpdatedAt returns the recorded updated_at for the given PR, or the zero
// time if the PR is not yet known to the checkpoint.
func (c *Checkpoint) prUpdatedAt(repo string, number int) time.Time {
	if c == nil || c.Repos == nil {
		return time.Time{}
	}
	rc, ok := c.Repos[repo]
	if !ok || rc.PRUpdatedAt == nil {
		return time.Time{}
	}
	return rc.PRUpdatedAt[number]
}

// removePRs drops the given PR numbers from a repo's checkpoint so they are
// re-processed on the next pass.
func (c *Checkpoint) removePRs(repo string, numbers map[int]struct{}) {
	if c == nil || c.Repos == nil || len(numbers) == 0 {
		return
	}
	rc, ok := c.Repos[repo]
	if !ok {
		return
	}
	if rc.completedSet == nil {
		rc.completedSet = map[int]struct{}{}
		for _, n := range rc.CompletedPRs {
			rc.completedSet[n] = struct{}{}
		}
	}
	kept := rc.CompletedPRs[:0]
	for _, n := range rc.CompletedPRs {
		if _, drop := numbers[n]; drop {
			delete(rc.completedSet, n)
			if rc.PRUpdatedAt != nil {
				delete(rc.PRUpdatedAt, n)
			}
			continue
		}
		kept = append(kept, n)
	}
	rc.CompletedPRs = kept
}

func loadOrInitCheckpoint(dir string) (*Checkpoint, error) {
	path := filepath.Join(dir, FileCheckpoint)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Checkpoint{SchemaVersion: SchemaVersion, Repos: map[string]*RepoCheckpoint{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read checkpoint: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(b, &cp); err != nil {
		return nil, fmt.Errorf("failed to parse checkpoint: %w", err)
	}
	if cp.Repos == nil {
		cp.Repos = map[string]*RepoCheckpoint{}
	}
	return &cp, nil
}
