package attestation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/httputil"
	"github.com/srz-zumix/go-gh-extension/pkg/ioutil"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/markdown"
)

// AssetLocation identifies where in a pull request an asset URL was found.
type AssetLocation string

const (
	// LocationBody indicates the asset was found in the pull request body.
	LocationBody AssetLocation = "body"
	// LocationIssueComment indicates the asset was found in an issue comment.
	LocationIssueComment AssetLocation = "issue_comment"
	// LocationReviewComment indicates the asset was found in a pull request
	// review comment.
	LocationReviewComment AssetLocation = "review_comment"
)

// PRAsset describes a single GitHub-hosted asset found while scanning a pull
// request, together with any Git provenance metadata read from it.
type PRAsset struct {
	// PRNumber is the pull request number the asset was found in.
	PRNumber int `json:"pr_number"`
	// PRURL is the HTML URL of the pull request.
	PRURL string `json:"pr_url"`
	// Location identifies where the asset URL was found.
	Location AssetLocation `json:"location"`
	// LocationID is the comment ID the asset was found in, or 0 for the
	// pull request body.
	LocationID int64 `json:"location_id,omitempty"`
	// LocationURL is the HTML URL of the comment the asset was found in, or
	// of the pull request itself for the body.
	LocationURL string `json:"location_url,omitempty"`
	// AssetURL is the GitHub-hosted URL of the asset.
	AssetURL string `json:"asset_url"`
	// Filename is the asset's original upload name, when known.
	Filename string `json:"filename"`
	// FileSize is the asset size in bytes, or -1 when unknown.
	FileSize int64 `json:"file_size"`
	// Tags is the Git provenance metadata read from the asset, empty when
	// the asset has no embedded attestation.
	Tags []Tag `json:"tags,omitempty"`
	// Attested reports whether Git provenance metadata was found.
	Attested bool `json:"attested"`
	// Error records a non-fatal per-asset failure (download or read error),
	// empty when Attested reflects the asset's actual state.
	Error string `json:"error,omitempty"`
}

// PRAssetOptions configures ReadPRAssets.
type PRAssetOptions struct {
	// Repo is the repository containing the pull request.
	Repo repository.Repository
	// PR identifies the pull request by number, URL, or branch name.
	PR string
	// MaxAssetSize, when greater than zero, skips assets whose reported size
	// exceeds this many bytes instead of downloading them.
	MaxAssetSize int64
}

// scannedAsset is an asset URL found while scanning pull request text,
// before any HTTP metadata or attestation has been read.
type scannedAsset struct {
	url         string
	filename    string
	location    AssetLocation
	locationID  int64
	locationURL string
}

// scannedText is a single piece of pull request text to scan for asset
// URLs, together with where it came from.
type scannedText struct {
	text        string
	location    AssetLocation
	locationID  int64
	locationURL string
}

// scanAssetURLs scans each entry in texts for GitHub-hosted asset URLs
// matching patterns, in order, deduplicating URLs across all entries (the
// first occurrence wins). It has no GitHub API or network dependency, so it
// can be unit-tested directly against sample Markdown bodies.
func scanAssetURLs(patterns []*regexp.Regexp, texts []scannedText) []scannedAsset {
	var scanned []scannedAsset
	seen := make(map[string]bool)

	for _, entry := range texts {
		if entry.text == "" {
			continue
		}
		hints := markdown.ExtractFilenameHints(entry.text)
		for _, pattern := range patterns {
			for _, u := range pattern.FindAllString(entry.text, -1) {
				u = strings.TrimRight(u, ".,;:!?)")
				if seen[u] {
					continue
				}
				seen[u] = true
				scanned = append(scanned, scannedAsset{
					url:         u,
					filename:    hints[u],
					location:    entry.location,
					locationID:  entry.locationID,
					locationURL: entry.locationURL,
				})
			}
		}
	}
	return scanned
}

// fetchPRAssets downloads each scanned asset to a temporary directory using
// httpClient and reads any embedded Git provenance metadata. Per-asset
// download or read failures are recorded on the asset rather than aborting
// the scan. It has no GitHub API dependency beyond the plain HTTP client, so
// it can be unit-tested against an httptest.Server.
func fetchPRAssets(ctx context.Context, httpClient *http.Client, host string, prNumber int, prURL string, scanned []scannedAsset, maxAssetSize int64) ([]*PRAsset, error) {
	if len(scanned) == 0 {
		return nil, nil
	}

	tmpDir, err := os.MkdirTemp("", "gh-review-kit-attestation-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary download directory: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			logger.Warn(fmt.Sprintf("failed to remove temporary directory %q: %v", tmpDir, rmErr))
		}
	}()

	assets := make([]*PRAsset, 0, len(scanned))
	for i, s := range scanned {
		asset := &PRAsset{
			PRNumber:    prNumber,
			PRURL:       prURL,
			Location:    s.location,
			LocationID:  s.locationID,
			LocationURL: s.locationURL,
			AssetURL:    s.url,
			FileSize:    -1,
		}

		meta := httputil.FetchAssetMeta(ctx, httpClient, s.url, host)
		if meta.Size >= 0 {
			asset.FileSize = meta.Size
		}
		filename := meta.Filename
		if filename == "" {
			filename = s.filename
		}
		if filename == "" {
			filename = ioutil.GetFilename(s.url)
		}
		asset.Filename = ioutil.SafeFilename(s.url, filename)

		if maxAssetSize > 0 && meta.Size > maxAssetSize {
			asset.Error = fmt.Sprintf("skipped: asset size %d bytes exceeds the limit of %d bytes", meta.Size, maxAssetSize)
			logger.Warn(fmt.Sprintf("skipping asset %q: size %d bytes exceeds the limit of %d bytes", s.url, meta.Size, maxAssetSize))
			assets = append(assets, asset)
			continue
		}

		destPath := filepath.Join(tmpDir, fmt.Sprintf("%d-%s", i, asset.Filename))
		if err := ioutil.DownloadFile(ctx, httpClient, s.url, destPath); err != nil {
			asset.Error = err.Error()
			logger.Warn(fmt.Sprintf("failed to download asset %q: %v", s.url, err))
			assets = append(assets, asset)
			continue
		}

		result, err := ReadGitMetadata(ctx, ReadOptions{Input: destPath})
		// Remove the downloaded file as soon as it is read so peak disk usage
		// stays around a single asset instead of the whole pull request.
		if rmErr := os.Remove(destPath); rmErr != nil && !os.IsNotExist(rmErr) {
			logger.Warn(fmt.Sprintf("failed to remove temporary asset file %q: %v", destPath, rmErr))
		}
		switch {
		case err == nil:
			asset.Tags = result.Tags
			asset.Attested = true
		case errors.Is(err, ErrNoMetadata):
			// No attestation embedded; leave Tags empty and Attested false.
		default:
			asset.Error = err.Error()
			logger.Warn(fmt.Sprintf("failed to read git metadata from asset %q: %v", s.url, err))
		}
		assets = append(assets, asset)
	}

	return assets, nil
}

// ReadPRAssets scans a pull request's body, issue comments, and review
// comments for GitHub-hosted asset URLs, downloads each one to a temporary
// directory, and reads any embedded Git provenance metadata. Per-asset
// download or read failures are recorded on the asset rather than aborting
// the scan.
func ReadPRAssets(ctx context.Context, g *gh.GitHubClient, opts PRAssetOptions) ([]*PRAsset, error) {
	pr, err := gh.FindPRByIdentifier(ctx, g, opts.Repo, opts.PR)
	if err != nil {
		return nil, fmt.Errorf("failed to find pull request %q: %w", opts.PR, err)
	}

	texts := []scannedText{{text: pr.GetBody(), location: LocationBody, locationURL: pr.GetHTMLURL()}}

	issueComments, err := gh.ListIssueComments(ctx, g, opts.Repo, pr.GetNumber())
	if err != nil {
		return nil, fmt.Errorf("failed to list issue comments for pull request #%d: %w", pr.GetNumber(), err)
	}
	for _, comment := range issueComments {
		texts = append(texts, scannedText{text: comment.GetBody(), location: LocationIssueComment, locationID: comment.GetID(), locationURL: comment.GetHTMLURL()})
	}

	reviewComments, err := gh.ListPullRequestReviewComments(ctx, g, opts.Repo, pr.GetNumber())
	if err != nil {
		return nil, fmt.Errorf("failed to list review comments for pull request #%d: %w", pr.GetNumber(), err)
	}
	for _, comment := range reviewComments {
		texts = append(texts, scannedText{text: comment.GetBody(), location: LocationReviewComment, locationID: comment.GetID(), locationURL: comment.GetHTMLURL()})
	}

	patterns := gh.BuildAssetURLPatterns(opts.Repo.Host)
	scanned := scanAssetURLs(patterns, texts)

	httpClient := httputil.NewHostAwareClient(g.GetClient().Client(), opts.Repo.Host)
	return fetchPRAssets(ctx, httpClient, opts.Repo.Host, pr.GetNumber(), pr.GetHTMLURL(), scanned, opts.MaxAssetSize)
}

// ReadAssetURL downloads the asset at assetURL to a temporary file and reads
// its embedded Git provenance metadata. host selects the authenticated
// transport to use when the asset is hosted on a GitHub-authenticated host.
func ReadAssetURL(ctx context.Context, g *gh.GitHubClient, host, assetURL string) ([]Tag, error) {
	httpClient := httputil.NewHostAwareClient(g.GetClient().Client(), host)

	tmpDir, err := os.MkdirTemp("", "gh-review-kit-attestation-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary download directory: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			logger.Warn(fmt.Sprintf("failed to remove temporary directory %q: %v", tmpDir, rmErr))
		}
	}()

	filename := ioutil.GetFilename(assetURL)
	if filename == "" {
		filename = "asset"
	}
	destPath := filepath.Join(tmpDir, ioutil.SafeFilename(assetURL, filename))

	if err := ioutil.DownloadFile(ctx, httpClient, assetURL, destPath); err != nil {
		return nil, fmt.Errorf("failed to download asset %q: %w", assetURL, err)
	}

	result, err := ReadGitMetadata(ctx, ReadOptions{Input: destPath})
	if err != nil {
		return nil, err
	}
	return result.Tags, nil
}
