package attestation

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/attestation"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// readGitMetadata is the attestation view workflow invoked by the command
// for the local-file mode. It is a package variable so tests can substitute
// a fake implementation without invoking ffprobe.
var readGitMetadata = attestation.ReadGitMetadata

// readAssetURL is the attestation view workflow invoked by the command for
// the single asset URL mode. It is a package variable so tests can
// substitute a fake implementation without making real HTTP requests.
var readAssetURL = attestation.ReadAssetURL

// readPRAssets is the attestation view workflow invoked by the command for
// the --pr mode. It is a package variable so tests can substitute a fake
// implementation without calling the GitHub API.
var readPRAssets = attestation.ReadPRAssets

// assetHostFromURL maps a GitHub-hosted asset URL to the host whose
// authenticated client should be used to fetch it. The dedicated image CDN
// hostnames used by github.com serve assets on behalf of github.com itself.
func assetHostFromURL(assetURL string) string {
	u, err := url.Parse(assetURL)
	if err != nil {
		return ""
	}
	switch u.Host {
	case "user-images.githubusercontent.com", "private-user-images.githubusercontent.com":
		return "github.com"
	default:
		return u.Host
	}
}

// resolveAssetHost determines the repository (host) whose authenticated client
// should fetch an asset URL. An explicit --repo takes precedence; when it is
// set but cannot be parsed the error is surfaced rather than being silently
// ignored. When no usable repository is available (no --repo and no current
// repository), the host is derived from the asset URL itself.
func resolveAssetHost(repo, assetURL string) (repository.Repository, error) {
	resolved, err := parser.Repository(parser.RepositoryInput(repo))
	if err != nil {
		if repo != "" {
			return repository.Repository{}, fmt.Errorf("failed to resolve repository %q: %w", repo, err)
		}
		resolved = repository.Repository{}
	}
	if resolved.Host == "" {
		resolved = repository.Repository{Host: assetHostFromURL(assetURL)}
	}
	if resolved.Host == "" {
		return repository.Repository{}, fmt.Errorf("failed to determine the GitHub host for %q; specify --repo", assetURL)
	}
	return resolved, nil
}

// NewViewCmd creates a new command to display Git provenance metadata
// embedded in a video, PNG, or JPEG file, whether stored locally, hosted at
// a GitHub asset URL, or attached to a pull request.
func NewViewCmd() *cobra.Command {
	var (
		repo     string
		pr       string
		format   string
		exporter cmdutil.Exporter
	)

	cmd := &cobra.Command{
		Use:   "view [<input-file> | <asset-url>]",
		Short: "Display Git provenance metadata embedded in a video, PNG, or JPEG file",
		Long: `Display Git provenance metadata embedded in a video, PNG, or JPEG file.

This command reads the metadata tags previously embedded by 'attestation
set', without modifying the file. Video files are probed with ffprobe; PNG
and JPEG files are read natively.

It supports three modes, mutually exclusive with each other:
  - <input-file>: read metadata from a local video, PNG, or JPEG file.
  - <asset-url>: download a GitHub-hosted asset URL (e.g. a file pasted into
    a pull request) to a temporary location and read its metadata.
  - --pr: scan a pull request's body, issue comments, and review comments
    for GitHub-hosted asset URLs, and read metadata from each one found.
    Assets with no embedded attestation are listed with a "no attestation
    found" note; per-asset download or read failures are shown as
    "error=<message>" rather than aborting the scan.

Output defaults to "key=value" lines, one per tag. In --pr text output, each
asset is rendered as a block beginning with a "<filename> (<location>)"
header, followed by its "key=value" tags, "no attestation found", or
"error=<message>", with blocks separated by blank lines. The json format
(--format json, optionally with --jq/--template) produces structured data
instead.

Requires ffprobe to be available on PATH for video files; PNG and JPEG
files have no external tool dependency.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := render.NewRenderer(exporter)

			if pr != "" && len(args) > 0 {
				return fmt.Errorf("cannot specify both an input file or asset URL argument and --pr")
			}
			if pr == "" && len(args) == 0 {
				return fmt.Errorf("requires exactly one of <input-file>, <asset-url>, or --pr")
			}

			if pr != "" {
				resolvedRepo, err := parser.Repository(parser.RepositoryInput(repo), parser.RepositoryFromURL(pr))
				if err != nil {
					return fmt.Errorf("failed to resolve repository: %w", err)
				}
				client, err := gh.NewGitHubClientWithRepo(resolvedRepo)
				if err != nil {
					return fmt.Errorf("failed to create GitHub client: %w", err)
				}

				assets, err := readPRAssets(ctx, client, attestation.PRAssetOptions{Repo: resolvedRepo, PR: pr})
				if err != nil {
					return fmt.Errorf("failed to scan pull request %q for attestations: %w", pr, err)
				}

				return attestation.RenderPRAssets(r, assets)
			}

			input := args[0]
			if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
				resolved, err := resolveAssetHost(repo, input)
				if err != nil {
					return err
				}

				client, err := gh.NewGitHubClientWithRepo(resolved)
				if err != nil {
					return fmt.Errorf("failed to create GitHub client: %w", err)
				}

				tags, err := readAssetURL(ctx, client, resolved.Host, input)
				if err != nil {
					return fmt.Errorf("failed to read git metadata from %q: %w", input, err)
				}
				return attestation.Render(r, tags)
			}

			result, err := readGitMetadata(ctx, attestation.ReadOptions{Input: input})
			if err != nil {
				return fmt.Errorf("failed to read git metadata from %q: %w", input, err)
			}
			return attestation.Render(r, result.Tags)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository to use for GitHub API access ([HOST/]OWNER/REPO, default: current repository or derived from --pr/the asset URL)")
	f.StringVar(&pr, "pr", "", "Scan a pull request's attachments for Git provenance metadata (number, URL, or branch name; mutually exclusive with <input-file>/<asset-url>)")
	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "text", []string{"text"}); err != nil {
		panic(err)
	}

	return cmd
}
