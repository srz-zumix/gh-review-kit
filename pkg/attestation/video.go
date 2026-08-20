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
	// ffmpeg refuses to write to a pre-existing (empty) file without -y;
	// remove the placeholder so ffmpeg creates it fresh, and always clean it
	// up unless it has been promoted to the final output path.
	os.Remove(tmpPath)
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

	if err := promoteOutput(tmpPath, opts.Output, opts.Force); err != nil {
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

	if _, err := os.Stat(output); err == nil {
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

// promoteOutput moves tmpPath to output. When force is set and output
// already exists, the existing file is backed up first and restored if the
// final rename fails, so a failure never destroys an existing output file.
func promoteOutput(tmpPath, output string, force bool) error {
	if !force {
		if err := os.Rename(tmpPath, output); err != nil {
			return fmt.Errorf("failed to write output file %q: %w", output, err)
		}
		return nil
	}

	if _, err := os.Stat(output); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(tmpPath, output); err != nil {
				return fmt.Errorf("failed to write output file %q: %w", output, err)
			}
			return nil
		}
		return fmt.Errorf("failed to access output file %q: %w", output, err)
	}

	backupPath := output + ".attestation-bak"
	if err := os.Rename(output, backupPath); err != nil {
		return fmt.Errorf("failed to back up existing output file %q: %w", output, err)
	}
	if err := os.Rename(tmpPath, output); err != nil {
		// Roll back: restore the original output file.
		if rollbackErr := os.Rename(backupPath, output); rollbackErr != nil {
			return fmt.Errorf("failed to write output file %q: %w (rollback also failed: %v)", output, err, rollbackErr)
		}
		return fmt.Errorf("failed to write output file %q: %w", output, err)
	}
	os.Remove(backupPath)
	return nil
}
