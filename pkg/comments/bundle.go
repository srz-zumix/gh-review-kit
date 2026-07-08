package comments

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// BundleOptions configures Bundle.
type BundleOptions struct {
	OutputDir  string
	GroupBy    string // empty = single linear bundle stream
	MaxRecords int    // 0 = no per-bundle record cap
	MaxBytes   int64  // 0 = no per-bundle byte cap
	Filters    SampleFilters
}

// BundleManifest describes the output of Bundle.
type BundleManifest struct {
	Dataset   string         `json:"dataset"`
	GroupBy   string         `json:"group_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	Bundles   []BundleEntry  `json:"bundles"`
}

// BundleEntry describes a single bundle file.
type BundleEntry struct {
	File    string `json:"file"`
	Group   string `json:"group,omitempty"`
	Records int    `json:"records"`
	Bytes   int64  `json:"bytes"`
}

// Bundle splits a comments corpus into smaller JSONL files suitable for
// distributing across parallel Agent runs. A manifest.json is written next to
// the bundle files to make the split reproducible and self-describing.
func Bundle(dir string, opts BundleOptions) (*BundleManifest, error) {
	if opts.OutputDir == "" {
		return nil, fmt.Errorf("output-dir is required")
	}
	if opts.MaxRecords == 0 && opts.MaxBytes == 0 {
		return nil, fmt.Errorf("at least one of --max-records or --max-bytes must be set")
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output dir: %w", err)
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

	writers := map[string]*bundleWriter{}
	defer func() {
		for _, w := range writers {
			_ = w.Close()
		}
	}()

	manifest := &BundleManifest{
		Dataset:   AbsDataset(dir),
		GroupBy:   opts.GroupBy,
		CreatedAt: time.Now().UTC(),
	}

	if err := IterateComments(dir, func(c *Comment) error {
		if !sampleMatches(c, opts.Filters) {
			return nil
		}
		line, err := json.Marshal(c)
		if err != nil {
			return err
		}
		line = append(line, '\n')
		for _, key := range groupKey(c, prLabels) {
			w, ok := writers[key]
			if !ok {
				w = newBundleWriter(opts.OutputDir, key, opts.MaxRecords, opts.MaxBytes)
				writers[key] = w
			}
			entry, err := w.Write(line)
			if err != nil {
				return err
			}
			if entry != nil {
				manifest.Bundles = append(manifest.Bundles, *entry)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	for _, w := range writers {
		entry, err := w.Finish()
		if err != nil {
			return nil, err
		}
		if entry != nil {
			manifest.Bundles = append(manifest.Bundles, *entry)
		}
	}

	sort.Slice(manifest.Bundles, func(i, j int) bool { return manifest.Bundles[i].File < manifest.Bundles[j].File })

	if err := writeJSONFile(filepath.Join(opts.OutputDir, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

type bundleWriter struct {
	dir        string
	group      string
	maxRecords int
	maxBytes   int64
	index      int
	curFile    *os.File
	curW       *bufio.Writer
	curRecords int
	curBytes   int64
}

func newBundleWriter(dir, group string, maxRecords int, maxBytes int64) *bundleWriter {
	return &bundleWriter{dir: dir, group: group, maxRecords: maxRecords, maxBytes: maxBytes}
}

func (b *bundleWriter) Write(line []byte) (*BundleEntry, error) {
	if b.curFile == nil {
		if err := b.openNext(); err != nil {
			return nil, err
		}
	}
	// Roll over before writing if appending this line would exceed caps.
	if (b.maxRecords > 0 && b.curRecords+1 > b.maxRecords) ||
		(b.maxBytes > 0 && b.curBytes+int64(len(line)) > b.maxBytes && b.curRecords > 0) {
		entry, err := b.closeCurrent()
		if err != nil {
			return nil, err
		}
		if err := b.openNext(); err != nil {
			return entry, err
		}
		if _, werr := b.curW.Write(line); werr != nil {
			return entry, werr
		}
		b.curRecords++
		b.curBytes += int64(len(line))
		return entry, nil
	}
	if _, err := b.curW.Write(line); err != nil {
		return nil, err
	}
	b.curRecords++
	b.curBytes += int64(len(line))
	return nil, nil
}

func (b *bundleWriter) Finish() (*BundleEntry, error) {
	return b.closeCurrent()
}

func (b *bundleWriter) Close() error {
	if b.curFile == nil {
		return nil
	}
	if err := b.curW.Flush(); err != nil {
		return err
	}
	return b.curFile.Close()
}

func (b *bundleWriter) openNext() error {
	b.index++
	name := fmt.Sprintf("bundle-%04d.jsonl", b.index)
	if b.group != "" {
		name = fmt.Sprintf("%s-%04d.jsonl", sanitizeName(b.group), b.index)
	}
	path := filepath.Join(b.dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open bundle file: %w", err)
	}
	b.curFile = f
	b.curW = bufio.NewWriter(f)
	b.curRecords = 0
	b.curBytes = 0
	return nil
}

func (b *bundleWriter) closeCurrent() (*BundleEntry, error) {
	if b.curFile == nil {
		return nil, nil
	}
	if b.curRecords == 0 {
		_ = b.curW.Flush()
		path := b.curFile.Name()
		_ = b.curFile.Close()
		_ = os.Remove(path)
		b.curFile = nil
		return nil, nil
	}
	if err := b.curW.Flush(); err != nil {
		return nil, err
	}
	name := filepath.Base(b.curFile.Name())
	entry := &BundleEntry{
		File:    name,
		Group:   b.group,
		Records: b.curRecords,
		Bytes:   b.curBytes,
	}
	if err := b.curFile.Close(); err != nil {
		return entry, err
	}
	b.curFile = nil
	return entry, nil
}

func sanitizeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "group"
	}
	return string(out)
}
