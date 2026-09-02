package attestation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// Input is the path to the source video, PNG, or JPEG file (required).
	Input string
	// Output is the path to write the resulting file to (required).
	Output string
	// RepoDir is the Git repository directory to collect provenance from.
	// When empty, the current directory is used.
	RepoDir string
	// Comment is an optional freeform comment embedded alongside the Git
	// provenance tags under CommentTag. When empty, no comment tag is added.
	Comment string
	// Force allows overwriting an existing Output file.
	Force bool
}

// EmbedResult reports the outcome of EmbedGitMetadata.
type EmbedResult struct {
	// Output is the path of the written file.
	Output string
	// Tags is the ordered list of Git metadata tags that were embedded.
	Tags []Tag
	// Warnings lists tags that could not be verified after embedding, e.g.
	// because the output container does not support arbitrary custom
	// metadata keys. The command still succeeds when only warnings occur.
	Warnings []string
}

// EmbedGitMetadata collects Git provenance from opts.RepoDir and embeds it
// into a copy of opts.Input written to opts.Output. Video files are
// stream-copied with FFmpeg without transcoding and verified with ffprobe;
// PNG and JPEG files are embedded and verified directly, without
// ffmpeg/ffprobe.
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

	format, err := peekImageFormat(opts.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to access input file %q: %w", opts.Input, err)
	}

	// Fail fast on missing ffmpeg/ffprobe before doing any other work, since
	// they are required for the (more common) video path.
	var ffmpegPath, ffprobePath string
	if format == imageFormatUnknown {
		ffmpegPath, err = runner.LookPath("ffmpeg")
		if err != nil {
			return nil, fmt.Errorf("ffmpeg is required but was not found on PATH: %w", err)
		}
		ffprobePath, err = runner.LookPath("ffprobe")
		if err != nil {
			return nil, fmt.Errorf("ffprobe is required but was not found on PATH: %w", err)
		}
	}

	meta, err := CollectGitMetadata(ctx, opts.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to collect git metadata: %w", err)
	}
	tags := meta.Tags()
	if opts.Comment != "" {
		tags = append(tags, Tag{Key: CommentTag, Value: opts.Comment})
	}

	outputDir := filepath.Dir(opts.Output)
	ext := filepath.Ext(opts.Output)
	// Stage the write inside a private temporary directory (0700) created on
	// the same filesystem as the final output. Writing the work file
	// directly into the caller-controlled output directory would let another
	// user unlink it and substitute a symlink before it is opened for
	// writing, which could then write through the symlink to an unintended
	// file. A private directory that only the owner can traverse closes that
	// window on filesystems that honor Unix permissions, while keeping the
	// file on the same filesystem for an atomic publish. It is not a defense
	// against a privileged or same-user process able to mutate the output
	// directory entry itself.
	stageDir, err := os.MkdirTemp(outputDir, ".attestation-stage-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary work directory: %w", err)
	}
	tmpPath := filepath.Join(stageDir, "video"+ext)
	defer func() {
		// Best-effort cleanup. os.Remove (not RemoveAll) avoids recursively
		// deleting a directory that may have been substituted in place of the
		// staging directory. tmpPath may already be gone when it was renamed
		// to the output path on the --force branch.
		os.Remove(tmpPath)
		os.Remove(stageDir)
	}()

	var warnings []string
	if format != imageFormatUnknown {
		warnings, err = embedImageGitMetadata(opts.Input, tmpPath, format, tags)
	} else {
		warnings, err = embedVideoGitMetadata(ctx, runner, ffmpegPath, ffprobePath, opts.Input, tmpPath, tags)
	}
	if err != nil {
		return nil, err
	}

	if err := promoteOutput(opts.Input, tmpPath, opts.Output, opts.Force); err != nil {
		return nil, err
	}

	return &EmbedResult{Output: opts.Output, Tags: tags, Warnings: warnings}, nil
}

// embedImageGitMetadata embeds tags into input's native image metadata
// (PNG iTXt chunks (UTF-8 text) or JPEG COM segments) and writes the result to tmpPath,
// without invoking ffmpeg/ffprobe. It returns warnings for any tag that a
// read-back of the written file does not confirm.
func embedImageGitMetadata(input, tmpPath string, format imageFormat, tags []Tag) ([]string, error) {
	data, err := os.ReadFile(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read input file %q: %w", input, err)
	}

	out, err := embedImageTags(format, data, tags)
	if err != nil {
		return nil, fmt.Errorf("failed to embed git metadata into %q: %w", input, err)
	}
	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write temporary output file: %w", err)
	}

	written, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read back written file %q: %w", tmpPath, err)
	}
	readBack, err := readImageTags(format, written)
	if err != nil {
		return nil, fmt.Errorf("failed to verify embedded metadata: %w", err)
	}
	got := make(map[string]string, len(readBack))
	for _, tag := range readBack {
		got[tag.Key] = tag.Value
	}
	return warningsForMismatch(tags, got), nil
}

// embedVideoGitMetadata embeds tags into input using ffmpeg to stream-copy
// all media without transcoding into tmpPath, verified afterwards with
// ffprobe.
func embedVideoGitMetadata(ctx context.Context, runner commandRunner, ffmpegPath, ffprobePath, input, tmpPath string, tags []Tag) ([]string, error) {
	if _, err := runner.Output(ctx, ffmpegPath, ffmpegArgs(input, tmpPath, tags)...); err != nil {
		return nil, fmt.Errorf("failed to embed git metadata into %q: %w", input, err)
	}

	warnings, err := verifyTags(ctx, runner, ffprobePath, tmpPath, tags)
	if err != nil {
		return nil, fmt.Errorf("failed to verify embedded metadata: %w", err)
	}
	return warnings, nil
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
	// -map_metadata 0 copies the input's global metadata, so a previously
	// embedded comment would survive a re-embed that supplies no --comment.
	// Emit an empty value to delete any inherited comment, keeping re-embedding
	// authoritative and consistent with the PNG/JPEG paths (which strip known
	// tags before writing).
	hasComment := false
	for _, tag := range tags {
		if tag.Key == CommentTag {
			hasComment = true
			break
		}
	}
	if !hasComment {
		args = append(args, "-metadata", CommentTag+"=")
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

	return warningsForMismatch(tags, probed.Format.Tags), nil
}

// ReadOptions configures ReadGitMetadata.
type ReadOptions struct {
	// Input is the path to the video or image file to inspect (required).
	Input string
}

// ReadResult reports the Git provenance metadata found in a video or image
// file.
type ReadResult struct {
	// Tags is the ordered list of Git metadata tags found in the file.
	Tags []Tag
}

// ReadGitMetadata reads the Git provenance metadata tags embedded in
// opts.Input, without modifying the file. Video files are probed with
// ffprobe; PNG and JPEG files are read directly, without ffmpeg/ffprobe.
func ReadGitMetadata(ctx context.Context, opts ReadOptions) (*ReadResult, error) {
	return readGitMetadata(ctx, opts, execCommandRunner{})
}

func readGitMetadata(ctx context.Context, opts ReadOptions, runner commandRunner) (*ReadResult, error) {
	if opts.Input == "" {
		return nil, fmt.Errorf("input path must not be empty")
	}

	format, err := peekImageFormat(opts.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to access input file %q: %w", opts.Input, err)
	}

	var tags []Tag
	if format != imageFormatUnknown {
		data, err := os.ReadFile(opts.Input)
		if err != nil {
			return nil, fmt.Errorf("failed to read input file %q: %w", opts.Input, err)
		}
		tags, err = readImageTags(format, data)
		if err != nil {
			return nil, fmt.Errorf("failed to read git metadata from %q: %w", opts.Input, err)
		}
	} else {
		tags, err = readVideoGitMetadata(ctx, runner, opts.Input)
		if err != nil {
			return nil, err
		}
	}

	if len(tags) == 0 {
		return nil, fmt.Errorf("%w in %q", ErrNoMetadata, opts.Input)
	}
	return &ReadResult{Tags: tags}, nil
}

// ErrNoMetadata is returned (wrapped) by ReadGitMetadata when the input file
// has no embedded Git provenance metadata. Callers can distinguish this case
// from other read failures via errors.Is.
var ErrNoMetadata = errors.New("no git provenance metadata found")

// readVideoGitMetadata reads the Git provenance metadata tags embedded in
// input using ffprobe.
func readVideoGitMetadata(ctx context.Context, runner commandRunner, input string) ([]Tag, error) {
	ffprobePath, err := runner.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe is required but was not found on PATH: %w", err)
	}

	out, err := runner.Output(ctx, ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		input,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to probe %q: %w", input, err)
	}

	var probed ffprobeFormat
	if err := json.Unmarshal(out, &probed); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output for %q: %w", input, err)
	}

	return orderedKnownTags(probed.Format.Tags), nil
}

// publishNoReplace writes the staged file at tmpPath to output without ever
// replacing an existing file. It first attempts an atomic, zero-copy hard
// link, which fails when output already exists. On filesystems that do not
// support hard links (e.g. exFAT/FAT) it falls back to an exclusive
// create-and-copy, which also refuses to overwrite an existing file. A
// replacing rename is deliberately never used, since it would silently
// overwrite a file created concurrently and violate the --force=false
// contract. The caller remains responsible for cleaning up tmpPath.
func publishNoReplace(tmpPath, output string) error {
	if err := os.Link(tmpPath, output); err == nil {
		return nil
	} else if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("output file %q already exists; use --force to overwrite", output)
	}

	// Hard links are unsupported (or failed for another reason): fall back to
	// an exclusive create so a concurrently created output is still not
	// overwritten.
	dst, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("output file %q already exists; use --force to overwrite", output)
		}
		return fmt.Errorf("failed to write output file %q: %w", output, err)
	}
	src, err := os.Open(tmpPath)
	if err != nil {
		dst.Close()
		os.Remove(output)
		return fmt.Errorf("failed to read staged output file: %w", err)
	}
	_, copyErr := io.Copy(dst, src)
	src.Close()
	closeErr := dst.Close()
	if copyErr != nil {
		os.Remove(output)
		return fmt.Errorf("failed to write output file %q: %w", output, copyErr)
	}
	if closeErr != nil {
		os.Remove(output)
		return fmt.Errorf("failed to write output file %q: %w", output, closeErr)
	}
	return nil
}

// promoteOutput publishes the staged file at tmpPath to output. When output
// does not yet exist it is published with publishNoReplace so a --force=false
// run never overwrites a file created concurrently. When output already exists
// it requires force; the existing file is backed up first and restored if the
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
		// Publish without replacing a destination that may have been created
		// concurrently after the existence check above, so a --force=false run
		// never clobbers a racing writer's file.
		return publishNoReplace(tmpPath, output)
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
