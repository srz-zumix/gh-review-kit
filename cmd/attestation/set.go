package attestation

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/attestation"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/guardrails"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// embedGitMetadata is the attestation workflow invoked by the command.
// It is a package variable so tests can substitute a fake implementation
// without invoking ffmpeg/ffprobe.
var embedGitMetadata = attestation.EmbedGitMetadata

// updateAssets is the attestation workflow invoked by the command for the
// --pr/--issue modes. It is a package variable so tests can substitute a
// fake implementation without calling the GitHub API.
var updateAssets = attestation.UpdateAssets

// newGitHubClientWithRepo constructs the GitHub client for the --pr/--issue
// modes. It is a package variable so tests can substitute a fake without
// requiring authentication.
var newGitHubClientWithRepo = gh.NewGitHubClientWithRepo

// isReadonly reports whether the global --read-only guardrail is active.
// It is a package variable so tests can override it without mutating the
// process-wide guardrail singleton.
var isReadonly = guardrails.IsReadonly

// NewSetCmd creates a new command to embed Git provenance metadata into a
// video, PNG, or JPEG file.
func NewSetCmd() *cobra.Command {
	var (
		output       string
		repo         string
		pr           string
		issue        string
		repoDir      string
		comment      string
		maxAssetSize int64
		force        bool
		format       string
		exporter     cmdutil.Exporter
	)

	cmd := &cobra.Command{
		Use:   "set [<input-file> | <asset-url>]",
		Short: "Embed Git provenance metadata into a video, PNG, or JPEG file",
		Long: `Embed Git provenance metadata into a video, PNG, or JPEG file.

This command collects Git information (commit, branch, dirty state, commit
date, and repository) from a local Git repository and embeds it as metadata
tags into a copy of the input file, together with an optional freeform
comment supplied via --comment. Video files are stream-copied with FFmpeg without
transcoding and verified with ffprobe. PNG and JPEG files are embedded
natively (PNG iTXt chunks (UTF-8 text) or JPEG COM segments), without invoking
FFmpeg.

It supports two kinds of input:
  - <input-file>: embed metadata into a local file and write the result to
    --output, which is required in this mode.
  - --pr or --issue: re-embed metadata into files already attached to a pull
    request or issue. Each attachment is downloaded, re-embedded, uploaded
    again through GitHub's user-attachments endpoint, and every link to it in
    the body and comments is rewritten to the new URL. Attachments that already
    carry provenance metadata are left untouched. Passing an <asset-url>
    argument as well limits the run to that single attachment. --output is
    optional here and, when given, also keeps a local copy of the single
    re-embedded attachment. GitHub offers no API to delete the originals, so
    they remain reachable at their old URLs, and uploading is unavailable on
    GitHub Enterprise Server.

This embeds unsigned provenance metadata only. It is not a cryptographic
signature, GitHub artifact attestation, or tamper-proof claim; the metadata
can be edited or removed like any other file metadata.

Requires ffmpeg and ffprobe to be available on PATH for video files; PNG and
JPEG files have no external tool dependency.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if pr != "" && issue != "" {
				return fmt.Errorf("cannot specify both --pr and --issue")
			}
			target, isPR := pr, pr != ""
			if issue != "" {
				target = issue
			}
			if target == "" && len(args) == 0 {
				return fmt.Errorf("requires exactly one of <input-file>, --pr, or --issue")
			}
			if maxAssetSize < 0 {
				return fmt.Errorf("--max-asset-size must be zero or a positive number of bytes")
			}

			if target == "" {
				// The <asset-url> form only makes sense with --pr/--issue.
				// Reject a URL here so it is not mistaken for a local file
				// path and failing later with a confusing file error.
				if _, isURL, err := classifyAsset(args[0]); err != nil {
					return err
				} else if isURL {
					return fmt.Errorf("%q is a URL; specify --pr or --issue to re-embed a pull request or issue attachment", args[0])
				}
				if isReadonly() {
					return fmt.Errorf("attestation set cannot run in read-only mode because it writes a local file")
				}
				return runLocalSet(cmd, args[0], output, repoDir, comment, force, exporter)
			}

			if isReadonly() {
				return fmt.Errorf("attestation set cannot run in read-only mode because it uploads assets and edits comments")
			}

			var assetURL string
			if len(args) > 0 {
				normalized, isURL, err := classifyAsset(args[0])
				if err != nil {
					return err
				}
				if !isURL {
					return fmt.Errorf("%q must be an asset URL when --pr or --issue is used", args[0])
				}
				assetURL = normalized
			}
			if output != "" && assetURL == "" {
				return fmt.Errorf("--output requires a single <asset-url> argument because it names one file")
			}

			resolvedRepo, err := parser.Repository(parser.RepositoryInput(repo), parser.RepositoryFromURL(target))
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			if assetURL != "" && !isGitHubAssetURL(resolvedRepo.Host, assetURL) {
				return fmt.Errorf("%q is not a recognized GitHub-hosted asset URL for host %q; only assets pasted into a pull request or issue are supported", assetURL, resolvedRepo.Host)
			}

			client, err := newGitHubClientWithRepo(resolvedRepo)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			updates, err := updateAssets(cmd.Context(), client, attestation.UpdateAssetsOptions{
				Repo:         resolvedRepo,
				Target:       target,
				PullRequest:  isPR,
				AssetURL:     assetURL,
				Output:       output,
				RepoDir:      repoDir,
				Comment:      comment,
				Force:        force,
				MaxAssetSize: maxAssetSize,
			})
			if err != nil {
				return fmt.Errorf("failed to re-embed attestations for %q: %w", target, err)
			}

			return attestation.RenderAssetUpdates(render.NewRenderer(exporter), updates)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&force, "force", "f", false, "Overwrite the output file if it already exists")
	f.StringVar(&issue, "issue", "", "Re-embed and re-upload the attachments of an issue, rewriting its links (number or URL; mutually exclusive with --pr)")
	f.Int64Var(&maxAssetSize, "max-asset-size", 0, "In --pr/--issue mode, skip attachments whose server-reported size exceeds this many bytes instead of downloading them (0: no limit)")
	f.StringVarP(&output, "output", "o", "", "Output file path (required for <input-file>, optional for a single <asset-url>)")
	f.StringVar(&pr, "pr", "", "Re-embed and re-upload the attachments of a pull request, rewriting its links (number, URL, or branch name; mutually exclusive with --issue)")
	f.StringVarP(&repo, "repo", "R", "", "Repository for GitHub authentication and asset uploads, [HOST/]OWNER/REPO (default: current repository or derived from --pr/--issue)")
	f.StringVarP(&repoDir, "repo-dir", "C", "", "Git repository directory to collect provenance from (default: current directory)")
	f.StringVar(&comment, "comment", "", "Freeform comment to embed alongside the Git provenance tags (optional, default: none)")
	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "text", []string{"text"}); err != nil {
		panic(err)
	}

	return cmd
}

// runLocalSet embeds provenance metadata into a local file and writes the
// result to output.
func runLocalSet(cmd *cobra.Command, input, output, repoDir, comment string, force bool, exporter cmdutil.Exporter) error {
	if output == "" {
		return fmt.Errorf("--output is required")
	}

	result, err := embedGitMetadata(cmd.Context(), attestation.EmbedOptions{
		Input:   input,
		Output:  output,
		RepoDir: repoDir,
		Comment: comment,
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
}
