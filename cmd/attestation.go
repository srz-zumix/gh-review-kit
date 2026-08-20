/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/attestation"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/guardrails"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// embedGitMetadata is the attestation workflow invoked by the command.
// It is a package variable so tests can substitute a fake implementation
// without invoking ffmpeg/ffprobe.
var embedGitMetadata = attestation.EmbedGitMetadata

// isReadonly reports whether the global --read-only guardrail is active.
// It is a package variable so tests can override it without mutating the
// process-wide guardrail singleton.
var isReadonly = guardrails.IsReadonly

// NewAttestationCmd creates a new command to embed Git provenance metadata
// into a video file.
func NewAttestationCmd() *cobra.Command {
	var (
		output  string
		repoDir string
		force   bool
	)

	cmd := &cobra.Command{
		Use:   "attestation <input-video>",
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
				return fmt.Errorf("attestation cannot run in read-only mode because it writes a local video file")
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

			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", result.Output)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "Overwrite the output file if it already exists")
	f.StringVar(&output, "output", "", "Output video file path (required)")
	f.StringVar(&repoDir, "repo-dir", "", "Git repository directory to collect provenance from (default: current directory)")

	return cmd
}

func init() {
	rootCmd.AddCommand(NewAttestationCmd())
}
