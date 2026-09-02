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

// classifyAsset determines whether input is an http(s) asset URL rather than a
// local file path. It returns (normalizedURL, true, nil) for a valid asset URL
// with its scheme canonicalized to lower case (net/http only accepts lower-case
// "http"/"https"), ("", false, nil) for a local file path or a non-http scheme,
// and ("", false, err) for a malformed http(s) URL so the failure surfaces
// instead of being misread as a file path.
func classifyAsset(input string) (string, bool, error) {
	u, err := url.Parse(input)
	if err != nil {
		if hasHTTPSchemePrefix(input) {
			return "", false, fmt.Errorf("invalid asset URL %q: %w", input, err)
		}
		return "", false, nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return "", false, fmt.Errorf("invalid asset URL %q: missing host", input)
		}
		u.Scheme = strings.ToLower(u.Scheme)
		return u.String(), true, nil
	default:
		return "", false, nil
	}
}

// hasHTTPSchemePrefix reports whether s begins with an http:// or https://
// scheme, case-insensitively.
func hasHTTPSchemePrefix(s string) bool {
	ls := strings.ToLower(s)
	return strings.HasPrefix(ls, "http://") || strings.HasPrefix(ls, "https://")
}

// isGitHubAssetURL reports whether assetURL is a GitHub-hosted asset URL for
// the given authentication authority host, matching the same URL shapes that
// are recognized when scanning a pull request. The whole URL must match a
// pattern (not merely contain one) so a foreign host cannot smuggle a matching
// substring in its path or query.
func isGitHubAssetURL(host, assetURL string) bool {
	for _, p := range gh.BuildAssetURLPatterns(host) {
		if p.FindString(assetURL) == assetURL {
			return true
		}
	}
	return false
}

// githubComAssetHost reports whether u is a github.com-hosted asset URL and,
// if so, returns the authentication authority host ("github.com"). The
// dedicated user-attachment CDN hostnames serve assets on behalf of github.com
// itself. It returns an empty string for any other host, which must not be
// treated as a GitHub authentication authority.
func githubComAssetHost(u *url.URL) string {
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "www.github.com",
		"user-images.githubusercontent.com", "private-user-images.githubusercontent.com":
		return "github.com"
	default:
		return ""
	}
}

// resolveAssetHost determines the GitHub host whose authenticated client should
// download an asset URL. The host is the authentication authority; the
// host-aware transport applies credentials only when a request host matches it
// and strips them otherwise, so a mismatch with a redirected CDN/storage host
// is expected and safe. Precedence:
//  1. An explicit --repo: its host is the authority. A parse error is surfaced
//     rather than being silently ignored.
//  2. Otherwise, a recognized github.com asset URL (including its
//     user-attachment CDN hosts): github.com is the authority.
//  3. Otherwise, the current repository's host, when available: this covers
//     GitHub Enterprise Server assets served from a separate storage host.
//
// An arbitrary, unrecognized URL host is never adopted as the authority, so a
// configured token is not sent to a non-GitHub server; the user must pass
// --repo in that case.
func resolveAssetHost(repo, assetURL string) (repository.Repository, error) {
	u, err := url.Parse(assetURL)
	if err != nil {
		return repository.Repository{}, fmt.Errorf("invalid asset URL %q: %w", assetURL, err)
	}
	if u.Hostname() == "" {
		return repository.Repository{}, fmt.Errorf("invalid asset URL %q: missing host", assetURL)
	}
	// Require HTTPS so a token is never transmitted in cleartext when the
	// request host matches the resolved authentication authority.
	if !strings.EqualFold(u.Scheme, "https") {
		return repository.Repository{}, fmt.Errorf("asset URL %q must use https", assetURL)
	}

	if repo != "" {
		resolved, err := parser.Repository(parser.RepositoryInput(repo))
		if err != nil {
			return repository.Repository{}, fmt.Errorf("failed to resolve repository %q: %w", repo, err)
		}
		if resolved.Host != "" {
			return resolved, nil
		}
	}

	if host := githubComAssetHost(u); host != "" {
		return repository.Repository{Host: host}, nil
	}

	if current, err := parser.Repository(); err == nil && current.Host != "" {
		return current, nil
	}

	return repository.Repository{}, fmt.Errorf("cannot determine a trusted GitHub host for %q; specify --repo", assetURL)
}

// NewViewCmd creates a new command to display Git provenance metadata
// embedded in a video, PNG, or JPEG file, whether stored locally, hosted at
// a GitHub asset URL, or attached to a pull request.
func NewViewCmd() *cobra.Command {
	var (
		repo         string
		pr           string
		maxAssetSize int64
		format       string
		exporter     cmdutil.Exporter
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
			if maxAssetSize < 0 {
				return fmt.Errorf("--max-asset-size must be zero or a positive number of bytes")
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

				assets, err := readPRAssets(ctx, client, attestation.PRAssetOptions{Repo: resolvedRepo, PR: pr, MaxAssetSize: maxAssetSize})
				if err != nil {
					return fmt.Errorf("failed to scan pull request %q for attestations: %w", pr, err)
				}

				return attestation.RenderPRAssets(r, assets)
			}

			input := args[0]
			assetURL, isURL, err := classifyAsset(input)
			if err != nil {
				return err
			}
			if isURL {
				resolved, err := resolveAssetHost(repo, assetURL)
				if err != nil {
					return err
				}

				if !isGitHubAssetURL(resolved.Host, assetURL) {
					return fmt.Errorf("%q is not a recognized GitHub-hosted asset URL for host %q; only assets pasted into a pull request are supported", assetURL, resolved.Host)
				}

				client, err := gh.NewGitHubClientWithRepo(resolved)
				if err != nil {
					return fmt.Errorf("failed to create GitHub client: %w", err)
				}

				tags, err := readAssetURL(ctx, client, resolved.Host, assetURL)
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
	f.StringVarP(&repo, "repo", "R", "", "Repository for GitHub authentication (PR API access and asset downloads), [HOST/]OWNER/REPO (default: current repository or derived from --pr/the asset URL)")
	f.StringVar(&pr, "pr", "", "Scan a pull request's attachments for Git provenance metadata (number, URL, or branch name; mutually exclusive with <input-file>/<asset-url>)")
	f.Int64Var(&maxAssetSize, "max-asset-size", 0, "In --pr mode, skip assets whose server-reported size exceeds this many bytes instead of downloading them (0: no limit)")
	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "text", []string{"text"}); err != nil {
		panic(err)
	}

	return cmd
}
