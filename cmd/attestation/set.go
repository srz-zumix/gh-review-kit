package attestation

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/attestation"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/guardrails"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// embedGitMetadata is the attestation workflow invoked by the command.
// It is a package variable so tests can substitute a fake implementation
// without invoking ffmpeg/ffprobe.
var embedGitMetadata = attestation.EmbedGitMetadata

// isReadonly reports whether the global --read-only guardrail is active.
// It is a package variable so tests can override it without mutating the
// process-wide guardrail singleton.
var isReadonly = guardrails.IsReadonly

// NewSetCmd creates a new command to embed Git provenance metadata into a
// video file.
func NewSetCmd() *cobra.Command {
	var (
		output   string
		repoDir  string
		force    bool
		format   string
		exporter cmdutil.Exporter
	)

	cmd := &cobra.Command{
		Use:   "set <input-video>",
		Short: "Embed Git provenance metadata into a video file",
		Long: `Embed Git provenance metadata into a video file.

This command collects Git information (commit, branch, dirty state, commit
date, and repository) from a local Git repository and embeds it as global
metadata tags into a copy of the input video, using FFmpeg to stream-copy the
media without transcoding. The resulting metadata is verified with ffprobe
before being written to the output path.

This embeds unsigned provenance metadata only. It is not a cryptographic
signature, GitHub artifact attestation, or tamper-proof claim; the metadata
can be edited or removed like any other video metadata.

Requires ffmpeg and ffprobe to be available on PATH.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isReadonly() {
				return fmt.Errorf("attestation set cannot run in read-only mode because it writes a local video file")
			}
			if output == "" {
				return fmt.Errorf("--output is required")
			}

			input := args[0]
			ctx := cmd.Context()

			result, err := embedGitMetadata(ctx, attestation.EmbedOptions{
				Input:   input,
				Output:  output,
				RepoDir: repoDir,
				Force:   force,
			})
			if err != nil {
				return fmt.Errorf("failed to embed git metadata into %q: %w", input, err)
			}

			for _, warning := range result.Warnings {
				logger.Warn(warning)
			}

			r := render.NewRenderer(exporter)
			r.WriteLine(fmt.Sprintf("Wrote %s", result.Output))
			return attestation.Render(r, result.Tags)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&force, "force", "f", false, "Overwrite the output file if it already exists")
	f.StringVarP(&output, "output", "o", "", "Output video file path (required)")
	f.StringVarP(&repoDir, "repo-dir", "C", "", "Git repository directory to collect provenance from (default: current directory)")
	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "text", []string{"text"}); err != nil {
		panic(err)
	}

	return cmd
}
