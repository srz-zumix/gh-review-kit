package comments

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

// Dataset on-disk filenames.
const (
	FileCorpus     = "corpus.jsonl"
	FilePRs        = "prs.jsonl"
	FileManifest   = "manifest.json"
	FileCheckpoint = "checkpoint.json"
)

// Dataset represents a writable comments dataset directory.
type Dataset struct {
	Dir        string
	corpus     *os.File
	corpusW    *bufio.Writer
	prs        *os.File
	prsW       *bufio.Writer
	manifest   *Manifest
	checkpoint *Checkpoint
}

// OpenDataset opens (or creates) a dataset directory in append mode. If the
// directory does not contain a manifest yet, it is initialized with the
// provided filters.
func OpenDataset(dir string, filters Filters) (*Dataset, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create dataset directory: %w", err)
	}

	manifest, err := loadOrInitManifest(dir, filters)
	if err != nil {
		return nil, err
	}
	checkpoint, err := loadOrInitCheckpoint(dir)
	if err != nil {
		return nil, err
	}

	corpus, err := os.OpenFile(filepath.Join(dir, FileCorpus), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open corpus file: %w", err)
	}
	prs, err := os.OpenFile(filepath.Join(dir, FilePRs), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = corpus.Close()
		return nil, fmt.Errorf("failed to open prs file: %w", err)
	}

	return &Dataset{
		Dir:        dir,
		corpus:     corpus,
		corpusW:    bufio.NewWriter(corpus),
		prs:        prs,
		prsW:       bufio.NewWriter(prs),
		manifest:   manifest,
		checkpoint: checkpoint,
	}, nil
}

// Manifest returns the in-memory manifest. The returned pointer should not be
// mutated outside the dataset.
func (d *Dataset) Manifest() *Manifest { return d.manifest }

// Checkpoint returns the in-memory checkpoint.
func (d *Dataset) Checkpoint() *Checkpoint { return d.checkpoint }

// AppendComment writes one Comment record and updates manifest counts.
func (d *Dataset) AppendComment(c *Comment) error {
	c.SchemaVersion = SchemaVersion
	if err := writeJSONLine(d.corpusW, c); err != nil {
		return err
	}
	switch c.Type {
	case CommentTypeReviewBody:
		d.manifest.Counts.ReviewBodies++
	case CommentTypeReviewComment:
		d.manifest.Counts.ReviewComments++
	case CommentTypeIssueComment:
		d.manifest.Counts.IssueComments++
	}
	return nil
}

// AppendPR writes one PR record and updates manifest counts.
func (d *Dataset) AppendPR(pr *PR) error {
	pr.SchemaVersion = SchemaVersion
	if err := writeJSONLine(d.prsW, pr); err != nil {
		return err
	}
	d.manifest.Counts.PRs++
	return nil
}

// MarkPRDone records that all comments for the given PR have been written.
func (d *Dataset) MarkPRDone(repo string, number int, updatedAt time.Time) {
	d.checkpoint.markDone(repo, number, updatedAt)
}

// IsPRDone reports whether the given PR was already completed in a prior run.
func (d *Dataset) IsPRDone(repo string, number int) bool {
	return d.checkpoint.isDone(repo, number)
}

// PRUpdatedAt returns the recorded updated_at for a PR, or zero time if the PR
// has not been recorded yet.
func (d *Dataset) PRUpdatedAt(repo string, number int) time.Time {
	return d.checkpoint.prUpdatedAt(repo, number)
}

// PurgePRs removes existing PR and comment records for the given PR numbers in
// the given repo so they can be re-extracted. Buffered writers are flushed, the
// JSONL files are rewritten via temp files that are only committed once BOTH
// have been fully staged. The commit moves the originals aside to .bak files
// before swapping the temp files into place, so a failure part-way through the
// commit is rolled back, keeping corpus and prs consistent with each other. The
// manifest counts as well as the checkpoint are updated to reflect the removal.
// Returns the number of PR and comment records that were dropped.
func (d *Dataset) PurgePRs(repo string, numbers map[int]struct{}) (prsRemoved int, commentsRemoved Counts, err error) {
	if len(numbers) == 0 {
		return 0, Counts{}, nil
	}
	if err := d.corpusW.Flush(); err != nil {
		return 0, Counts{}, fmt.Errorf("failed to flush corpus before purge: %w", err)
	}
	if err := d.prsW.Flush(); err != nil {
		return 0, Counts{}, fmt.Errorf("failed to flush prs before purge: %w", err)
	}
	if err := d.corpus.Close(); err != nil {
		return 0, Counts{}, fmt.Errorf("failed to close corpus before purge: %w", err)
	}
	if err := d.prs.Close(); err != nil {
		return 0, Counts{}, fmt.Errorf("failed to close prs before purge: %w", err)
	}

	commentsRemoved, err = stageCorpusPurge(d.Dir, repo, numbers)
	if err != nil {
		_ = os.Remove(filepath.Join(d.Dir, FileCorpus) + ".tmp")
		_ = d.reopenAppendWriters()
		return 0, Counts{}, err
	}
	prsRemoved, err = stagePRsPurge(d.Dir, repo, numbers)
	if err != nil {
		// Nothing has been committed yet; drop both staged files.
		_ = os.Remove(filepath.Join(d.Dir, FileCorpus) + ".tmp")
		_ = os.Remove(filepath.Join(d.Dir, FilePRs) + ".tmp")
		_ = d.reopenAppendWriters()
		return 0, Counts{}, err
	}

	// Both temp files are fully written; commit them together. Two independent
	// renames cannot be a single atomic operation, so the originals are first
	// moved aside to .bak files. This lets a failure while swapping either file
	// be rolled back, keeping corpus and prs consistent with each other.
	corpusPath := filepath.Join(d.Dir, FileCorpus)
	prsPath := filepath.Join(d.Dir, FilePRs)
	corpusTmp, prsTmp := corpusPath+".tmp", prsPath+".tmp"
	corpusBak, prsBak := corpusPath+".bak", prsPath+".bak"

	if err := os.Rename(corpusPath, corpusBak); err != nil {
		_ = os.Remove(corpusTmp)
		_ = os.Remove(prsTmp)
		_ = d.reopenAppendWriters()
		return 0, Counts{}, fmt.Errorf("failed to stage corpus purge commit: %w", err)
	}
	if err := os.Rename(prsPath, prsBak); err != nil {
		// Nothing has been swapped in yet; restore the corpus original.
		_ = os.Rename(corpusBak, corpusPath)
		_ = os.Remove(corpusTmp)
		_ = os.Remove(prsTmp)
		_ = d.reopenAppendWriters()
		return 0, Counts{}, fmt.Errorf("failed to stage prs purge commit: %w", err)
	}
	if err := os.Rename(corpusTmp, corpusPath); err != nil {
		// Restore both originals from their backups.
		_ = os.Rename(corpusBak, corpusPath)
		_ = os.Rename(prsBak, prsPath)
		_ = os.Remove(prsTmp)
		_ = d.reopenAppendWriters()
		return 0, Counts{}, fmt.Errorf("failed to commit corpus purge: %w", err)
	}
	if err := os.Rename(prsTmp, prsPath); err != nil {
		// Roll back the corpus swap and restore both originals.
		_ = os.Rename(corpusBak, corpusPath)
		_ = os.Rename(prsBak, prsPath)
		_ = d.reopenAppendWriters()
		return 0, Counts{}, fmt.Errorf("failed to commit prs purge: %w", err)
	}
	// Commit succeeded; drop the backups.
	_ = os.Remove(corpusBak)
	_ = os.Remove(prsBak)

	d.manifest.Counts.PRs -= prsRemoved
	d.manifest.Counts.ReviewBodies -= commentsRemoved.ReviewBodies
	d.manifest.Counts.ReviewComments -= commentsRemoved.ReviewComments
	d.manifest.Counts.IssueComments -= commentsRemoved.IssueComments
	d.checkpoint.removePRs(repo, numbers)

	if err := d.reopenAppendWriters(); err != nil {
		return prsRemoved, commentsRemoved, err
	}
	return prsRemoved, commentsRemoved, nil
}

// reopenAppendWriters reopens the corpus and prs files in append mode after a
// purge (or a failed purge) so the Dataset remains usable for further writes.
func (d *Dataset) reopenAppendWriters() error {
	corpus, err := os.OpenFile(filepath.Join(d.Dir, FileCorpus), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to reopen corpus after purge: %w", err)
	}
	prsFile, err := os.OpenFile(filepath.Join(d.Dir, FilePRs), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = corpus.Close()
		return fmt.Errorf("failed to reopen prs after purge: %w", err)
	}
	d.corpus = corpus
	d.corpusW = bufio.NewWriter(corpus)
	d.prs = prsFile
	d.prsW = bufio.NewWriter(prsFile)
	return nil
}

// stageCorpusPurge writes a purged copy of corpus.jsonl to corpus.jsonl.tmp
// without renaming it into place, so the caller can commit corpus and prs
// together only after both temp files are fully written.
func stageCorpusPurge(dir, repo string, numbers map[int]struct{}) (Counts, error) {
	src := filepath.Join(dir, FileCorpus)
	tmp := src + ".tmp"
	in, err := os.Open(src)
	if err != nil {
		return Counts{}, fmt.Errorf("failed to open corpus for purge: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return Counts{}, fmt.Errorf("failed to create corpus tmp for purge: %w", err)
	}
	bw := bufio.NewWriter(out)
	r := bufio.NewReaderSize(in, 1<<20)
	var removed Counts
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			if data := trimNewline(line); len(data) > 0 {
				var c Comment
				if uerr := json.Unmarshal(data, &c); uerr == nil {
					if c.Repo == repo {
						if _, drop := numbers[c.PRNumber]; drop {
							switch c.Type {
							case CommentTypeReviewBody:
								removed.ReviewBodies++
							case CommentTypeReviewComment:
								removed.ReviewComments++
							case CommentTypeIssueComment:
								removed.IssueComments++
							}
							if rerr == io.EOF {
								break
							}
							continue
						}
					}
				}
				if _, werr := bw.Write(line); werr != nil {
					_ = out.Close()
					return Counts{}, fmt.Errorf("failed to write corpus tmp: %w", werr)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = out.Close()
			return Counts{}, fmt.Errorf("failed to read corpus for purge: %w", rerr)
		}
	}
	if err := bw.Flush(); err != nil {
		_ = out.Close()
		return Counts{}, fmt.Errorf("failed to flush corpus tmp: %w", err)
	}
	if err := out.Close(); err != nil {
		return Counts{}, fmt.Errorf("failed to close corpus tmp: %w", err)
	}
	return removed, nil
}

// stagePRsPurge writes a purged copy of prs.jsonl to prs.jsonl.tmp without
// renaming it into place (see stageCorpusPurge).
func stagePRsPurge(dir, repo string, numbers map[int]struct{}) (int, error) {
	src := filepath.Join(dir, FilePRs)
	tmp := src + ".tmp"
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("failed to open prs for purge: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("failed to create prs tmp for purge: %w", err)
	}
	bw := bufio.NewWriter(out)
	r := bufio.NewReaderSize(in, 1<<20)
	removed := 0
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			if data := trimNewline(line); len(data) > 0 {
				var p PR
				if uerr := json.Unmarshal(data, &p); uerr == nil {
					if p.Repo == repo {
						if _, drop := numbers[p.Number]; drop {
							removed++
							if rerr == io.EOF {
								break
							}
							continue
						}
					}
				}
				if _, werr := bw.Write(line); werr != nil {
					_ = out.Close()
					return 0, fmt.Errorf("failed to write prs tmp: %w", werr)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = out.Close()
			return 0, fmt.Errorf("failed to read prs for purge: %w", rerr)
		}
	}
	if err := bw.Flush(); err != nil {
		_ = out.Close()
		return 0, fmt.Errorf("failed to flush prs tmp: %w", err)
	}
	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("failed to close prs tmp: %w", err)
	}
	return removed, nil
}

// Flush flushes buffered writes and persists manifest/checkpoint to disk.
func (d *Dataset) Flush() error {
	if err := d.corpusW.Flush(); err != nil {
		return fmt.Errorf("failed to flush corpus: %w", err)
	}
	if err := d.prsW.Flush(); err != nil {
		return fmt.Errorf("failed to flush prs: %w", err)
	}
	d.manifest.UpdatedAt = time.Now().UTC()
	if err := writeJSONFile(filepath.Join(d.Dir, FileManifest), d.manifest); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(d.Dir, FileCheckpoint), d.checkpoint); err != nil {
		return err
	}
	return nil
}

// Close flushes and closes the dataset files.
func (d *Dataset) Close() error {
	flushErr := d.Flush()
	cErr := d.corpus.Close()
	pErr := d.prs.Close()
	return errors.Join(flushErr, cErr, pErr)
}

func loadOrInitManifest(dir string, filters Filters) (*Manifest, error) {
	path := filepath.Join(dir, FileManifest)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		now := time.Now().UTC()
		return &Manifest{
			SchemaVersion: SchemaVersion,
			CreatedAt:     now,
			UpdatedAt:     now,
			Filters:       filters,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("manifest schema version %d is not supported by this build (expected %d)", m.SchemaVersion, SchemaVersion)
	}
	if err := reconcileManifestFilters(&m, filters); err != nil {
		return nil, err
	}
	return &m, nil
}

// reconcileManifestFilters verifies that the requested filters are compatible
// with the existing dataset and keeps manifest provenance accurate. Appending
// records for a new repository into the same dataset is allowed (the repo list
// is unioned); any other difference in the corpus-shaping filters is rejected
// so that re-running extract with different options cannot silently mix
// incompatible corpora into a single dataset.
func reconcileManifestFilters(m *Manifest, requested Filters) error {
	if !filtersCompatible(m.Filters, requested) {
		return fmt.Errorf("requested filters do not match the existing dataset manifest; use a new --dataset directory for a different extraction scope")
	}
	m.Filters.Repos = unionStrings(m.Filters.Repos, requested.Repos)
	return nil
}

// filtersCompatible reports whether two filter sets describe the same corpus
// shape, ignoring the repo list (which may grow across runs).
func filtersCompatible(a, b Filters) bool {
	a.Repos = nil
	b.Repos = nil
	return reflect.DeepEqual(normalizeFilters(a), normalizeFilters(b))
}

// normalizeFilters returns a copy of f with order-insensitive slices sorted so
// they can be compared for equality regardless of input ordering.
func normalizeFilters(f Filters) Filters {
	f.Labels = sortedCopy(f.Labels)
	f.CommentTypes = sortedCopy(f.CommentTypes)
	f.Paths = sortedCopy(f.Paths)
	return f
}

func sortedCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func unionStrings(a, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string(nil), a...), b...) {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func writeJSONLine(w *bufio.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}
	if err := w.WriteByte('\n'); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}
	return nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", filepath.Base(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to commit %s: %w", filepath.Base(path), err)
	}
	return nil
}

// IterateComments invokes fn for every Comment record in the dataset.
func IterateComments(dir string, fn func(*Comment) error) error {
	return iterateJSONL(filepath.Join(dir, FileCorpus), func(line []byte) error {
		var c Comment
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		return fn(&c)
	})
}

// IteratePRs invokes fn for every PR record in the dataset.
func IteratePRs(dir string, fn func(*PR) error) error {
	return iterateJSONL(filepath.Join(dir, FilePRs), func(line []byte) error {
		var p PR
		if err := json.Unmarshal(line, &p); err != nil {
			return err
		}
		return fn(&p)
	})
}

// LoadManifest reads manifest.json from a dataset directory.
func LoadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, FileManifest)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("manifest schema version %d is not supported by this build (expected %d)", m.SchemaVersion, SchemaVersion)
	}
	return &m, nil
}

func iterateJSONL(path string, handle func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", filepath.Base(path), err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	lineNo := 0
	for {
		lineNo++
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := trimNewline(line)
			if len(trimmed) > 0 {
				if err := handle(trimmed); err != nil {
					return fmt.Errorf("%s line %d: %w", filepath.Base(path), lineNo, err)
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filepath.Base(path), err)
		}
	}
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
