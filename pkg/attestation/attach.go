package attestation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/httputil"
	"github.com/srz-zumix/go-gh-extension/pkg/ioutil"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// uploadTimeout bounds a single user-attachments upload request. Videos may
// be up to 100 MB, so it is well above the API client default.
const uploadTimeout = 10 * time.Minute

// AssetUpdate reports what happened to one asset URL while re-embedding and
// re-uploading a pull request or issue's attachments.
type AssetUpdate struct {
	// Number is the pull request or issue number that was rewritten.
	Number int `json:"number"`
	// Location identifies where the asset URL was first found.
	Location AssetLocation `json:"location"`
	// LocationID is the comment ID the asset was first found in, or 0 for
	// the pull request or issue body.
	LocationID int64 `json:"location_id,omitempty"`
	// OldURL is the asset URL that was found.
	OldURL string `json:"old_url"`
	// NewURL is the re-uploaded asset URL, empty when the asset was skipped
	// or failed.
	NewURL string `json:"new_url,omitempty"`
	// Filename is the asset's upload name.
	Filename string `json:"filename"`
	// Tags is the Git provenance metadata that was embedded.
	Tags []Tag `json:"tags,omitempty"`
	// Output is the local path the re-embedded copy was also written to,
	// empty when no local copy was requested.
	Output string `json:"output,omitempty"`
	// Skipped records why an asset was left untouched, empty when it was
	// processed.
	Skipped string `json:"skipped,omitempty"`
	// Error records a non-fatal per-asset failure, empty on success.
	Error string `json:"error,omitempty"`
}

// UpdateAssetsOptions configures UpdateAssets.
type UpdateAssetsOptions struct {
	// Repo is the repository owning the target and receiving the uploads.
	Repo repository.Repository
	// Target identifies the pull request or issue by number, URL, or (for a
	// pull request) branch name.
	Target string
	// PullRequest reports whether Target names a pull request, which also
	// has review comments to scan.
	PullRequest bool
	// AssetURL, when set, limits processing to that single asset URL.
	AssetURL string
	// Output, when set, also writes the re-embedded asset to this local
	// path. It is only meaningful together with AssetURL.
	Output string
	// RepoDir is the Git repository directory to collect provenance from.
	RepoDir string
	// Comment is an optional freeform comment embedded alongside the tags.
	Comment string
	// Force allows overwriting an existing Output file.
	Force bool
	// MaxAssetSize, when greater than zero, skips assets whose reported size
	// exceeds this many bytes instead of downloading them.
	MaxAssetSize int64
}

// targetTexts is the set of editable texts belonging to one pull request or
// issue, in the order they should be scanned.
type targetTexts struct {
	number int
	texts  []scannedText
}

// replaceAssetURLs rewrites every asset URL in text that has a replacement,
// reporting whether anything changed. Longer URLs are replaced first so a URL
// that is a prefix of another cannot rewrite the shorter one's suffix. It has
// no GitHub API or network dependency, so it can be unit-tested directly.
func replaceAssetURLs(text string, replacements map[string]string) (string, bool) {
	if text == "" || len(replacements) == 0 {
		return text, false
	}

	olds := make([]string, 0, len(replacements))
	for old := range replacements {
		olds = append(olds, old)
	}
	sort.Slice(olds, func(i, j int) bool { return len(olds[i]) > len(olds[j]) })

	updated := text
	for _, old := range olds {
		updated = strings.ReplaceAll(updated, old, replacements[old])
	}
	return updated, updated != text
}

// filterScannedAssets narrows scanned to the single asset URL the caller
// named, returning an error when the pull request or issue does not
// reference it.
func filterScannedAssets(scanned []scannedAsset, assetURL string) ([]scannedAsset, error) {
	for _, s := range scanned {
		if s.url == assetURL {
			return []scannedAsset{s}, nil
		}
	}
	return nil, fmt.Errorf("asset %q is not referenced by the target", assetURL)
}

// loadTargetTexts resolves the target pull request or issue and collects the
// texts whose asset links can be rewritten: its body and every comment.
func loadTargetTexts(ctx context.Context, g *gh.GitHubClient, opts UpdateAssetsOptions) (*targetTexts, error) {
	var number int
	var body string

	if opts.PullRequest {
		pr, err := gh.FindPRByIdentifier(ctx, g, opts.Repo, opts.Target)
		if err != nil {
			return nil, fmt.Errorf("failed to find pull request %q: %w", opts.Target, err)
		}
		number, body = pr.GetNumber(), pr.GetBody()
	} else {
		issue, err := gh.FindIssueByIdentifier(ctx, g, opts.Repo, opts.Target)
		if err != nil {
			return nil, fmt.Errorf("failed to find issue %q: %w", opts.Target, err)
		}
		number, body = issue.GetNumber(), issue.GetBody()
	}

	texts := []scannedText{{text: body, location: LocationBody}}

	issueComments, err := gh.ListIssueComments(ctx, g, opts.Repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to list comments for #%d: %w", number, err)
	}
	for _, comment := range issueComments {
		texts = append(texts, scannedText{text: comment.GetBody(), location: LocationIssueComment, locationID: comment.GetID()})
	}

	if opts.PullRequest {
		reviewComments, err := gh.ListPullRequestReviewComments(ctx, g, opts.Repo, number)
		if err != nil {
			return nil, fmt.Errorf("failed to list review comments for pull request #%d: %w", number, err)
		}
		for _, comment := range reviewComments {
			texts = append(texts, scannedText{text: comment.GetBody(), location: LocationReviewComment, locationID: comment.GetID()})
		}
	}

	return &targetTexts{number: number, texts: texts}, nil
}

// reuploadAssets downloads each scanned asset, embeds Git provenance metadata
// into it, and uploads the result as a new user attachment. It returns one
// update per asset plus the old-to-new URL replacements to apply to the
// target's texts. Per-asset failures are recorded on the update rather than
// aborting the run; a rate-limited upload aborts, since retrying the
// remaining assets immediately would hit the same limit.
func reuploadAssets(ctx context.Context, g *gh.GitHubClient, downloadClient *http.Client, repoID int64, number int, scanned []scannedAsset, opts UpdateAssetsOptions) ([]*AssetUpdate, map[string]string, error) {
	if len(scanned) == 0 {
		return nil, nil, nil
	}

	tmpDir, err := os.MkdirTemp("", "gh-review-kit-attestation-")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temporary download directory: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			logger.Warn(fmt.Sprintf("failed to remove temporary directory %q: %v", tmpDir, rmErr))
		}
	}()

	updates := make([]*AssetUpdate, 0, len(scanned))
	replacements := make(map[string]string, len(scanned))

	for i, s := range scanned {
		update := &AssetUpdate{
			Number:     number,
			Location:   s.location,
			LocationID: s.locationID,
			OldURL:     s.url,
		}
		updates = append(updates, update)

		meta := httputil.FetchAssetMeta(ctx, downloadClient, s.url, opts.Repo.Host)
		filename := meta.Filename
		if filename == "" {
			filename = s.filename
		}
		if filename == "" {
			filename = ioutil.GetFilename(s.url)
		}
		update.Filename = ioutil.SafeFilename(s.url, filename)

		if opts.MaxAssetSize > 0 && meta.Size > opts.MaxAssetSize {
			update.Skipped = fmt.Sprintf("asset size %d bytes exceeds the limit of %d bytes", meta.Size, opts.MaxAssetSize)
			continue
		}
		contentType, ok := gh.UserAttachmentSupported(update.Filename, meta.Size)
		if !ok {
			update.Skipped = fmt.Sprintf("%q cannot be re-uploaded: %v", update.Filename, gh.ErrUserAttachmentUnsupported)
			continue
		}

		srcPath := filepath.Join(tmpDir, fmt.Sprintf("%d-src-%s", i, update.Filename))
		if err := ioutil.DownloadFile(ctx, downloadClient, s.url, srcPath); err != nil {
			update.Error = err.Error()
			logger.Warn(fmt.Sprintf("failed to download asset %q: %v", s.url, err))
			continue
		}

		// Leave an asset that already carries provenance untouched, so a
		// second run does not replace working links with fresh uploads.
		existing, err := ReadGitMetadata(ctx, ReadOptions{Input: srcPath})
		switch {
		case err == nil:
			update.Tags = existing.Tags
			update.Skipped = "asset is already attested"
			removeTempFile(srcPath)
			continue
		case !errors.Is(err, ErrNoMetadata):
			// Without a readable answer there is no way to tell an attested
			// asset from a bare one, so re-embedding could double-embed.
			update.Error = err.Error()
			logger.Warn(fmt.Sprintf("failed to read git metadata from asset %q: %v", s.url, err))
			removeTempFile(srcPath)
			continue
		}

		embeddedPath := opts.Output
		if embeddedPath == "" {
			embeddedPath = filepath.Join(tmpDir, fmt.Sprintf("%d-out-%s", i, update.Filename))
		}
		result, err := EmbedGitMetadata(ctx, EmbedOptions{
			Input:   srcPath,
			Output:  embeddedPath,
			RepoDir: opts.RepoDir,
			Comment: opts.Comment,
			Force:   opts.Force,
		})
		// The download is no longer needed once it has been re-embedded, so
		// peak disk usage stays around a single asset instead of the whole
		// pull request.
		removeTempFile(srcPath)
		if err != nil {
			update.Error = err.Error()
			logger.Warn(fmt.Sprintf("failed to embed git metadata into asset %q: %v", s.url, err))
			continue
		}
		update.Tags = result.Tags
		if opts.Output != "" {
			update.Output = result.Output
		}
		for _, warning := range result.Warnings {
			logger.Warn(warning)
		}

		newURL, err := uploadAsset(ctx, g, opts.Repo.Host, repoID, update.Filename, contentType, result.Output)
		if opts.Output == "" {
			removeTempFile(result.Output)
		}
		if err != nil {
			var rateLimited *gh.UserAttachmentRateLimitError
			if errors.As(err, &rateLimited) {
				return updates, replacements, fmt.Errorf("failed to upload asset %q: %w", update.Filename, err)
			}
			update.Error = err.Error()
			logger.Warn(fmt.Sprintf("failed to upload asset %q: %v", update.Filename, err))
			continue
		}

		update.NewURL = newURL
		replacements[s.url] = newURL
	}

	return updates, replacements, nil
}

// removeTempFile deletes a working copy of an asset, warning instead of
// failing the run when it cannot be removed.
func removeTempFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Warn(fmt.Sprintf("failed to remove temporary asset file %q: %v", path, err))
	}
}

// uploadAsset posts path to the user-attachments upload endpoint and returns
// the new asset URL.
func uploadAsset(ctx context.Context, g *gh.GitHubClient, host string, repoID int64, filename, contentType, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat the re-embedded asset: %w", err)
	}
	if _, ok := gh.UserAttachmentSupported(filename, info.Size()); !ok {
		return "", fmt.Errorf("re-embedded %q is %d bytes: %w", filename, info.Size(), gh.ErrUserAttachmentUnsupported)
	}

	return gh.UploadUserAttachment(ctx, g, gh.UserAttachmentUpload{
		Host:         host,
		RepositoryID: repoID,
		Name:         filename,
		ContentType:  contentType,
		Size:         info.Size(),
		Open:         func() (io.ReadCloser, error) { return os.Open(path) },
		Timeout:      uploadTimeout,
	})
}

// applyReplacements rewrites every text of the target that references a
// replaced asset URL and pushes the change back to GitHub.
func applyReplacements(ctx context.Context, g *gh.GitHubClient, opts UpdateAssetsOptions, target *targetTexts, replacements map[string]string) error {
	if len(replacements) == 0 {
		return nil
	}

	for _, entry := range target.texts {
		body, changed := replaceAssetURLs(entry.text, replacements)
		if !changed {
			continue
		}

		var err error
		switch entry.location {
		case LocationBody:
			_, err = gh.UpdateIssueBody(ctx, g, opts.Repo, target.number, body)
		case LocationIssueComment:
			_, err = gh.EditIssueComment(ctx, g, opts.Repo, entry.locationID, body)
		case LocationReviewComment:
			_, err = gh.EditPullRequestComment(ctx, g, opts.Repo, entry.locationID, body)
		}
		if err != nil {
			return fmt.Errorf("failed to update %s of #%d: %w", entry.location, target.number, err)
		}
	}
	return nil
}

// UpdateAssets scans a pull request or issue for GitHub-hosted asset URLs,
// re-embeds Git provenance metadata into each one, uploads the result as a
// new user attachment, and rewrites the body and comments to point at the new
// URLs. The original assets are left in place, since GitHub offers no API to
// delete them.
func UpdateAssets(ctx context.Context, g *gh.GitHubClient, opts UpdateAssetsOptions) ([]*AssetUpdate, error) {
	ghRepo, err := gh.GetRepository(ctx, g, opts.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to look up repository %s/%s: %w", opts.Repo.Owner, opts.Repo.Name, err)
	}
	repoID := ghRepo.GetID()
	if err := gh.CheckUserAttachmentUploadSupported(opts.Repo.Host, repoID); err != nil {
		return nil, err
	}

	target, err := loadTargetTexts(ctx, g, opts)
	if err != nil {
		return nil, err
	}

	scanned := scanAssetURLs(gh.BuildAssetURLPatterns(opts.Repo.Host), target.texts)
	if opts.AssetURL != "" {
		if scanned, err = filterScannedAssets(scanned, opts.AssetURL); err != nil {
			return nil, err
		}
	}

	downloadClient := httputil.NewHostAwareClient(g.GetClient().Client(), opts.Repo.Host)

	updates, replacements, err := reuploadAssets(ctx, g, downloadClient, repoID, target.number, scanned, opts)
	if err != nil {
		return updates, err
	}

	if err := applyReplacements(ctx, g, opts, target, replacements); err != nil {
		return updates, err
	}
	return updates, nil
}
