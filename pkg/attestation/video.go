package attestation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// commandRunner abstracts external command execution so that the FFmpeg /
// ffprobe workflow can be tested without invoking real binaries.
type commandRunner interface {
	// LookPath resolves name to an absolute path using PATH.
	LookPath(name string) (string, error)
	// Output runs name with args and returns its standard output.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// execCommandRunner is the commandRunner implementation backed by os/exec.
type execCommandRunner struct{}

func (execCommandRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (execCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// movFamilyExtensions lists output extensions that require the
// use_metadata_tags muxer option to retain arbitrary custom metadata keys.
var movFamilyExtensions = map[string]bool{
	".mp4": true,
	".mov": true,
	".m4v": true,
	".m4a": true,
	".3gp": true,
}

// EmbedOptions configures EmbedGitMetadata.
type EmbedOptions struct {
	// Input is the path to the source video file (required).
	Input string
	// Output is the path to write the resulting video file to (required).
	Output string
	// RepoDir is the Git repository directory to collect provenance from.
	// When empty, the current directory is used.
	RepoDir string
	// Force allows overwriting an existing Output file.
	Force bool
}

// EmbedResult reports the outcome of EmbedGitMetadata.
type EmbedResult struct {
	// Output is the path of the written video file.
	Output string
	// Tags is the ordered list of Git metadata tags that were embedded.
	Tags []Tag
	// Warnings lists tags that ffprobe could not verify after embedding,
	// e.g. because the output container does not support arbitrary custom
	// metadata keys. The command still succeeds when only warnings occur.
	Warnings []string
}

// EmbedGitMetadata collects Git provenance from opts.RepoDir and embeds it as
// global metadata tags into a copy of opts.Input written to opts.Output,
// using FFmpeg to stream-copy all media without transcoding. The result is
// verified with ffprobe before being promoted to opts.Output.
func EmbedGitMetadata(ctx context.Context, opts EmbedOptions) (*EmbedResult, error) {
	return embedGitMetadata(ctx, opts, execCommandRunner{})
}

func embedGitMetadata(ctx context.Context, opts EmbedOptions, runner commandRunner) (*EmbedResult, error) {
	if opts.Input == "" {
		return nil, fmt.Errorf("input path must not be empty")
	}
	if opts.Output == "" {
		return nil, fmt.Errorf("output path must not be empty")
	}

	if err := validateInputOutput(opts.Input, opts.Output, opts.Force); err != nil {
		return nil, err
	}

	ffmpegPath, err := runner.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg is required but was not found on PATH: %w", err)
	}
	ffprobePath, err := runner.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe is required but was not found on PATH: %w", err)
	}

	meta, err := CollectGitMetadata(ctx, opts.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to collect git metadata: %w", err)
	}
	tags := meta.Tags()

	outputDir := filepath.Dir(opts.Output)
	ext := filepath.Ext(opts.Output)
	tmp, err := os.CreateTemp(outputDir, "attestation-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary output file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to close temporary output file: %w", err)
	}
	// Keep the securely created placeholder in place; ffmpegArgs always passes
	// -y, so ffmpeg overwrites it. Removing it and letting ffmpeg recreate the
	// path would open a window for another process to substitute a symlink at
	// tmpPath, causing ffmpeg to follow it and overwrite an arbitrary file.
	// The placeholder is cleaned up unless it is promoted to the output path.
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			os.Remove(tmpPath)
		}
	}()

	if _, err := runner.Output(ctx, ffmpegPath, ffmpegArgs(opts.Input, tmpPath, tags)...); err != nil {
		return nil, fmt.Errorf("failed to embed git metadata into %q: %w", opts.Input, err)
	}

	warnings, err := verifyTags(ctx, runner, ffprobePath, tmpPath, tags)
	if err != nil {
		return nil, fmt.Errorf("failed to verify embedded metadata: %w", err)
	}

	if err := promoteOutput(opts.Input, tmpPath, opts.Output, opts.Force); err != nil {
		return nil, err
	}
	cleanupTmp = false

	return &EmbedResult{Output: opts.Output, Tags: tags, Warnings: warnings}, nil
}

// validateInputOutput checks that input is a usable source file, that input
// and output are not the same file, and that output may be written given the
// force flag.
func validateInputOutput(input, output string, force bool) error {
	info, err := os.Stat(input)
	if err != nil {
		return fmt.Errorf("failed to access input file %q: %w", input, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input path %q is not a regular file", input)
	}

	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return fmt.Errorf("failed to resolve input path %q: %w", input, err)
	}
	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("failed to resolve output path %q: %w", output, err)
	}
	if filepath.Clean(inputAbs) == filepath.Clean(outputAbs) {
		return fmt.Errorf("output path %q must not be the same as the input path (in-place editing is not supported)", output)
	}

	outputDir := filepath.Dir(outputAbs)
	if dirInfo, err := os.Stat(outputDir); err != nil {
		return fmt.Errorf("output directory %q does not exist: %w", outputDir, err)
	} else if !dirInfo.IsDir() {
		return fmt.Errorf("output directory %q is not a directory", outputDir)
	}

	if outputInfo, err := os.Stat(output); err == nil {
		// os.SameFile detects aliases that the path string comparison above
		// cannot, such as hard links, symlinks, and case-only variants on
		// case-insensitive filesystems. This prevents --force from letting
		// promoteOutput overwrite the input itself.
		if os.SameFile(info, outputInfo) {
			return fmt.Errorf("output path %q must not be the same as the input path (in-place editing is not supported)", output)
		}
		if !outputInfo.Mode().IsRegular() {
			return fmt.Errorf("output path %q exists and is not a regular file", output)
		}
		if !force {
			return fmt.Errorf("output file %q already exists; use --force to overwrite", output)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to access output file %q: %w", output, err)
	}

	return nil
}

// ffmpegArgs builds the FFmpeg command line that stream-copies input to
// output while adding the given global metadata tags and preserving existing
// streams, metadata, and chapters.
func ffmpegArgs(input, output string, tags []Tag) []string {
	args := []string{
		"-y",
		"-i", input,
		"-map", "0",
		"-map_metadata", "0",
		"-map_chapters", "0",
		"-c", "copy",
	}
	for _, tag := range tags {
		args = append(args, "-metadata", fmt.Sprintf("%s=%s", tag.Key, tag.Value))
	}
	if movFamilyExtensions[strings.ToLower(filepath.Ext(output))] {
		args = append(args, "-movflags", "use_metadata_tags")
	}
	args = append(args, output)
	return args
}

// ffprobeFormat models the subset of ffprobe's JSON output needed to verify
// embedded global metadata tags.
type ffprobeFormat struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

// verifyTags probes path with ffprobe and compares its global format tags
// against the expected tags. Missing or mismatched tags are returned as
// warnings; a failure to run or parse ffprobe's output is a fatal error.
func verifyTags(ctx context.Context, runner commandRunner, ffprobePath, path string, tags []Tag) ([]string, error) {
	out, err := runner.Output(ctx, ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		path,
	)
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var probed ffprobeFormat
	if err := json.Unmarshal(out, &probed); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	var warnings []string
	for _, tag := range tags {
		got, ok := probed.Format.Tags[tag.Key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("tag %q was not present in the output container after embedding", tag.Key))
			continue
		}
		if got != tag.Value {
			warnings = append(warnings, fmt.Sprintf("tag %q was rewritten by the output container: got %q want %q", tag.Key, got, tag.Value))
		}
	}
	return warnings, nil
}

// gitTagOrder lists the known Git provenance metadata keys, in the same
// order as GitMetadata.Tags, so ReadGitMetadata reports them consistently.
var gitTagOrder = []string{
	GitTagCommit,
	GitTagBranch,
	GitTagDirty,
	GitTagCommitDate,
	GitTagAuthor,
	GitTagRepository,
}

// ReadOptions configures ReadGitMetadata.
type ReadOptions struct {
	// Input is the path to the video file to inspect (required).
	Input string
}

// ReadResult reports the Git provenance metadata found in a video file.
type ReadResult struct {
	// Tags is the ordered list of Git metadata tags found in the file.
	Tags []Tag
}

// ReadGitMetadata reads the Git provenance metadata tags embedded in
// opts.Input using ffprobe, without modifying the file.
func ReadGitMetadata(ctx context.Context, opts ReadOptions) (*ReadResult, error) {
	return readGitMetadata(ctx, opts, execCommandRunner{})
}

func readGitMetadata(ctx context.Context, opts ReadOptions, runner commandRunner) (*ReadResult, error) {
	if opts.Input == "" {
		return nil, fmt.Errorf("input path must not be empty")
	}
	if _, err := os.Stat(opts.Input); err != nil {
		return nil, fmt.Errorf("failed to access input file %q: %w", opts.Input, err)
	}

	ffprobePath, err := runner.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe is required but was not found on PATH: %w", err)
	}

	out, err := runner.Output(ctx, ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		opts.Input,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to probe %q: %w", opts.Input, err)
	}

	var probed ffprobeFormat
	if err := json.Unmarshal(out, &probed); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output for %q: %w", opts.Input, err)
	}

	var tags []Tag
	for _, key := range gitTagOrder {
		if value, ok := probed.Format.Tags[key]; ok {
			tags = append(tags, Tag{Key: key, Value: value})
		}
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("no git provenance metadata found in %q", opts.Input)
	}

	return &ReadResult{Tags: tags}, nil
}

// promoteOutput moves tmpPath to output. When force is set and output
// already exists, the existing file is backed up first and restored if the
// final rename fails, so a failure never destroys an existing output file.
// input is re-checked against output with os.SameFile immediately before the
// destructive rename, since input and output may have become aliases (e.g.
// via a hard link, symlink, or case-only rename) after validateInputOutput
// ran and before the ffmpeg/ffprobe steps completed.
func promoteOutput(input, tmpPath, output string, force bool) error {
	inputInfo, err := os.Stat(input)
	if err != nil {
		return fmt.Errorf("failed to access input file %q: %w", input, err)
	}

	outputInfo, statErr := os.Stat(output)
	switch {
	case statErr == nil:
		if os.SameFile(inputInfo, outputInfo) {
			return fmt.Errorf("output path %q must not be the same as the input path (in-place editing is not supported)", output)
		}
		if !outputInfo.Mode().IsRegular() {
			return fmt.Errorf("output path %q exists and is not a regular file", output)
		}
	case errors.Is(statErr, os.ErrNotExist):
		if err := os.Rename(tmpPath, output); err != nil {
			return fmt.Errorf("failed to write output file %q: %w", output, err)
		}
		return nil
	default:
		return fmt.Errorf("failed to access output file %q: %w", output, statErr)
	}

	if !force {
		return fmt.Errorf("output file %q already exists; use --force to overwrite", output)
	}

	// Back up the existing output into a private temporary directory on the
	// same filesystem. A fixed backup name (e.g. output+".attestation-bak")
	// could clobber an unrelated pre-existing file or, when it happens to
	// equal the input path, overwrite and later remove the input itself.
	// Renaming into a freshly created directory avoids both, and renaming to a
	// nonexistent destination inside it does not rely on replace-on-rename
	// semantics that are not portable across platforms.
	backupDir, err := os.MkdirTemp(filepath.Dir(output), ".attestation-backup-*")
	if err != nil {
		return fmt.Errorf("failed to allocate backup directory for %q: %w", output, err)
	}
	backupPath := filepath.Join(backupDir, filepath.Base(output))
	if err := os.Rename(output, backupPath); err != nil {
		os.RemoveAll(backupDir)
		return fmt.Errorf("failed to back up existing output file %q: %w", output, err)
	}
	if err := os.Rename(tmpPath, output); err != nil {
		// Roll back: restore the original output file.
		if rollbackErr := os.Rename(backupPath, output); rollbackErr != nil {
			return fmt.Errorf("failed to write output file %q: %w (rollback also failed, original preserved at %q: %v)", output, err, backupPath, rollbackErr)
		}
		os.RemoveAll(backupDir)
		return fmt.Errorf("failed to write output file %q: %w", output, err)
	}
	os.RemoveAll(backupDir)
	return nil
}
