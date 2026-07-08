package comments

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v84/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// ExtractOptions configures one extraction run.
type ExtractOptions struct {
	Repo         repository.Repository
	State        string // open|closed|all (default: all)
	MergedOnly   bool
	Since        *time.Time
	Until        *time.Time
	Labels       []string
	CommentTypes []CommentType // empty = all
	IncludeBots  bool
	MinLength    int
	Paths        []string // path prefix filter for inline review comments
	Limit        int      // 0 = no limit; counts processed PRs
	NoRedact     bool
	Update       bool // re-fetch PRs whose updated_at advanced since the last run
}

// Extract runs an extraction loop against the configured repository, writing
// records to the dataset. PRs already recorded in the dataset's checkpoint are
// skipped so the call is safe to re-run after an interruption.
func Extract(ctx context.Context, client *gh.GitHubClient, ds *Dataset, opts ExtractOptions) error {
	state := opts.State
	if state == "" {
		state = "all"
	}
	listOpts := []gh.ListPullRequestsOption{
		&gh.ListPullRequestsOptionState{State: state},
		gh.ListPullRequestsOptionSortUpdated(),
		gh.ListPullRequestsOptionDirectionDescending(),
	}

	logger.Info("Listing pull requests", "repo", opts.Repo.Owner+"/"+opts.Repo.Name, "state", state)
	prs, err := gh.ListPullRequests(ctx, client, opts.Repo, listOpts...)
	if err != nil {
		return fmt.Errorf("failed to list pull requests: %w", err)
	}
	logger.Info("Listed pull requests", "count", len(prs))

	repoKey := opts.Repo.Owner + "/" + opts.Repo.Name
	commentTypes := commentTypeSet(opts.CommentTypes)

	if opts.Update {
		stale := map[int]struct{}{}
		for _, pr := range prs {
			if !prMatches(pr, opts) {
				continue
			}
			if !ds.IsPRDone(repoKey, pr.GetNumber()) {
				continue
			}
			prev := ds.PRUpdatedAt(repoKey, pr.GetNumber())
			if pr.GetUpdatedAt().Time.After(prev) {
				stale[pr.GetNumber()] = struct{}{}
			}
		}
		if len(stale) > 0 {
			prsRemoved, commentsRemoved, err := ds.PurgePRs(repoKey, stale)
			if err != nil {
				return fmt.Errorf("failed to purge stale PRs: %w", err)
			}
			logger.Info("Purged stale PRs for re-fetch",
				"prs", prsRemoved,
				"review_bodies", commentsRemoved.ReviewBodies,
				"review_comments", commentsRemoved.ReviewComments,
				"issue_comments", commentsRemoved.IssueComments,
			)
			if err := ds.Flush(); err != nil {
				return err
			}
		}
	}

	processed := 0

	for _, pr := range prs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !prMatches(pr, opts) {
			continue
		}
		if ds.IsPRDone(repoKey, pr.GetNumber()) {
			continue
		}
		if opts.Limit > 0 && processed >= opts.Limit {
			break
		}

		if err := extractOnePR(ctx, client, ds, opts.Repo, pr, opts, commentTypes); err != nil {
			// Best-effort flush so partial progress is persisted before returning.
			_ = ds.Flush()
			return fmt.Errorf("failed to extract PR #%d: %w", pr.GetNumber(), err)
		}
		ds.MarkPRDone(repoKey, pr.GetNumber(), pr.GetUpdatedAt().Time)
		processed++
		// Periodic flush for crash safety.
		if processed%25 == 0 {
			if err := ds.Flush(); err != nil {
				return err
			}
			logger.Info("Extraction progress", "processed_prs", processed)
		}
	}

	logger.Info("Extraction complete", "processed_prs", processed)
	return nil
}

func extractOnePR(ctx context.Context, client *gh.GitHubClient, ds *Dataset, repo repository.Repository, pr *github.PullRequest, opts ExtractOptions, commentTypes map[CommentType]bool) error {
	repoKey := repo.Owner + "/" + repo.Name

	// Collect all comments for this PR before touching the dataset so a mid-PR
	// API failure leaves no partial PR/comment records on disk. The next run
	// will then re-process this PR cleanly because the checkpoint is updated
	// only after a successful write.
	var comments []*Comment

	// Fetch reviews once when either review bodies or inline review comments are
	// requested. The review ID -> state map lets inline review_comment records
	// inherit their review's state (e.g. CHANGES_REQUESTED), which powers the
	// blocking strategy and review-state filters.
	var reviews []*github.PullRequestReview
	reviewStates := map[int64]string{}
	if commentTypes[CommentTypeReviewBody] || commentTypes[CommentTypeReviewComment] {
		rv, err := gh.GetPullRequestReviews(ctx, client, repo, pr)
		if err != nil {
			return fmt.Errorf("failed to get reviews: %w", err)
		}
		reviews = rv
		for _, r := range reviews {
			reviewStates[r.GetID()] = r.GetState()
		}
	}

	if commentTypes[CommentTypeReviewBody] {
		for _, rv := range reviews {
			if rec := toReviewBodyRecord(repoKey, pr, rv, opts); rec != nil {
				comments = append(comments, rec)
			}
		}
	}

	if commentTypes[CommentTypeReviewComment] {
		rc, err := gh.ListPullRequestReviewComments(ctx, client, repo, pr)
		if err != nil {
			return fmt.Errorf("failed to list review comments: %w", err)
		}
		for _, c := range rc {
			if rec := toReviewCommentRecord(repoKey, pr, c, reviewStates, opts); rec != nil {
				comments = append(comments, rec)
			}
		}
	}

	if commentTypes[CommentTypeIssueComment] {
		issueComments, err := gh.ListIssueComments(ctx, client, repo, pr)
		if err != nil {
			return fmt.Errorf("failed to list issue comments: %w", err)
		}
		for _, c := range issueComments {
			if rec := toIssueCommentRecord(repoKey, pr, c, opts); rec != nil {
				comments = append(comments, rec)
			}
		}
	}

	if err := ds.AppendPR(toPRRecord(repoKey, pr)); err != nil {
		return err
	}
	for _, c := range comments {
		if err := ds.AppendComment(c); err != nil {
			return err
		}
	}
	return nil
}

func commentTypeSet(types []CommentType) map[CommentType]bool {
	if len(types) == 0 {
		return map[CommentType]bool{
			CommentTypeReviewBody:    true,
			CommentTypeReviewComment: true,
			CommentTypeIssueComment:  true,
		}
	}
	m := map[CommentType]bool{}
	for _, t := range types {
		m[t] = true
	}
	return m
}

func prMatches(pr *github.PullRequest, opts ExtractOptions) bool {
	if opts.MergedOnly && !pr.GetMerged() && pr.MergedAt == nil {
		return false
	}
	if opts.Since != nil && pr.GetUpdatedAt().Before(*opts.Since) {
		// PRs are sorted by updated desc; older entries imply we can stop, but
		// we keep filtering inline to also cover unsorted callers in the future.
		return false
	}
	if opts.Until != nil && pr.GetCreatedAt().After(*opts.Until) {
		return false
	}
	if len(opts.Labels) > 0 {
		labelSet := map[string]bool{}
		for _, l := range pr.Labels {
			labelSet[strings.ToLower(l.GetName())] = true
		}
		matched := false
		for _, want := range opts.Labels {
			if labelSet[strings.ToLower(want)] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func toPRRecord(repoKey string, pr *github.PullRequest) *PR {
	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.GetName())
	}
	rec := &PR{
		Repo:        repoKey,
		Number:      pr.GetNumber(),
		Title:       pr.GetTitle(),
		URL:         pr.GetHTMLURL(),
		State:       pr.GetState(),
		Merged:      pr.GetMerged() || pr.MergedAt != nil,
		Draft:       pr.GetDraft(),
		Author:      pr.GetUser().GetLogin(),
		AuthorIsBot: isBotUser(pr.GetUser()),
		BaseRef:     pr.GetBase().GetRef(),
		HeadRef:     pr.GetHead().GetRef(),
		Labels:      labels,
		CreatedAt:   pr.GetCreatedAt().Time,
		UpdatedAt:   pr.GetUpdatedAt().Time,
	}
	if pr.ClosedAt != nil {
		t := pr.ClosedAt.Time
		rec.ClosedAt = &t
	}
	if pr.MergedAt != nil {
		t := pr.MergedAt.Time
		rec.MergedAt = &t
	}
	return rec
}

func toReviewBodyRecord(repoKey string, pr *github.PullRequest, rv *github.PullRequestReview, opts ExtractOptions) *Comment {
	body := rv.GetBody()
	if strings.TrimSpace(body) == "" {
		// Skip approve-only reviews with no narrative.
		return nil
	}
	if !opts.IncludeBots && isBotUser(rv.GetUser()) {
		return nil
	}
	body, redacted := maybeRedact(body, opts)
	if !lengthOK(body, opts.MinLength) {
		return nil
	}
	created := rv.GetSubmittedAt().Time
	return &Comment{
		ID:           rv.GetID(),
		NodeID:       rv.GetNodeID(),
		Type:         CommentTypeReviewBody,
		Repo:         repoKey,
		PRNumber:     pr.GetNumber(),
		Author:       rv.GetUser().GetLogin(),
		AuthorIsBot:  isBotUser(rv.GetUser()),
		Body:         body,
		BodyRedacted: redacted,
		URL:          rv.GetHTMLURL(),
		CreatedAt:    created,
		ReviewID:     rv.GetID(),
		ReviewState:  rv.GetState(),
	}
}

func toReviewCommentRecord(repoKey string, pr *github.PullRequest, c *github.PullRequestComment, reviewStates map[int64]string, opts ExtractOptions) *Comment {
	if !opts.IncludeBots && isBotUser(c.GetUser()) {
		return nil
	}
	if !pathMatches(c.GetPath(), opts.Paths) {
		return nil
	}
	body, redacted := maybeRedact(c.GetBody(), opts)
	if !lengthOK(body, opts.MinLength) {
		return nil
	}
	rec := &Comment{
		ID:           c.GetID(),
		NodeID:       c.GetNodeID(),
		Type:         CommentTypeReviewComment,
		Repo:         repoKey,
		PRNumber:     pr.GetNumber(),
		Author:       c.GetUser().GetLogin(),
		AuthorIsBot:  isBotUser(c.GetUser()),
		Body:         body,
		BodyRedacted: redacted,
		URL:          c.GetHTMLURL(),
		CreatedAt:    c.GetCreatedAt().Time,
		ReviewID:     c.GetPullRequestReviewID(),
		ReviewState:  reviewStates[c.GetPullRequestReviewID()],
		Path:         c.GetPath(),
		Line:         c.GetLine(),
		OriginalLine: c.GetOriginalLine(),
		Side:         c.GetSide(),
		DiffHunk:     c.GetDiffHunk(),
		InReplyTo:    c.GetInReplyTo(),
	}
	if c.UpdatedAt != nil {
		t := c.GetUpdatedAt().Time
		rec.UpdatedAt = &t
	}
	// "Outdated" is signaled by Position being nil while OriginalPosition is set.
	if c.Position == nil && c.OriginalPosition != nil {
		rec.Outdated = true
	}
	return rec
}

func toIssueCommentRecord(repoKey string, pr *github.PullRequest, c *github.IssueComment, opts ExtractOptions) *Comment {
	if !opts.IncludeBots && isBotUser(c.GetUser()) {
		return nil
	}
	body, redacted := maybeRedact(c.GetBody(), opts)
	if !lengthOK(body, opts.MinLength) {
		return nil
	}
	rec := &Comment{
		ID:           c.GetID(),
		NodeID:       c.GetNodeID(),
		Type:         CommentTypeIssueComment,
		Repo:         repoKey,
		PRNumber:     pr.GetNumber(),
		Author:       c.GetUser().GetLogin(),
		AuthorIsBot:  isBotUser(c.GetUser()),
		Body:         body,
		BodyRedacted: redacted,
		URL:          c.GetHTMLURL(),
		CreatedAt:    c.GetCreatedAt().Time,
	}
	if c.UpdatedAt != nil {
		t := c.GetUpdatedAt().Time
		rec.UpdatedAt = &t
	}
	return rec
}

func maybeRedact(body string, opts ExtractOptions) (string, bool) {
	if opts.NoRedact {
		return body, false
	}
	return Redact(body)
}

func lengthOK(body string, min int) bool {
	if min <= 0 {
		return true
	}
	return len(strings.TrimSpace(body)) >= min
}

func pathMatches(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func isBotUser(u *github.User) bool {
	if u == nil {
		return false
	}
	if strings.EqualFold(u.GetType(), "Bot") {
		return true
	}
	login := u.GetLogin()
	return strings.HasSuffix(login, "[bot]")
}

// CommentTypesFromStrings parses user-supplied comment type names.
func CommentTypesFromStrings(values []string) ([]CommentType, error) {
	out := []CommentType{}
	for _, v := range values {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "":
			continue
		case "review_body", "review-body", "body":
			out = append(out, CommentTypeReviewBody)
		case "review_comment", "review-comment", "inline":
			out = append(out, CommentTypeReviewComment)
		case "issue_comment", "issue-comment", "issue":
			out = append(out, CommentTypeIssueComment)
		default:
			return nil, fmt.Errorf("unknown comment type %q (allowed: review_body, review_comment, issue_comment)", v)
		}
	}
	slices.Sort(out)
	return out, nil
}
