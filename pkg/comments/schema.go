package comments

import "time"

// CommentType identifies which kind of pull request feedback a record represents.
type CommentType string

const (
	// CommentTypeReviewBody is the body of a PullRequestReview (the summary text submitted with a review action).
	CommentTypeReviewBody CommentType = "review_body"
	// CommentTypeReviewComment is an inline review comment attached to a diff line.
	CommentTypeReviewComment CommentType = "review_comment"
	// CommentTypeIssueComment is a comment on the pull request conversation that is not part of a review.
	CommentTypeIssueComment CommentType = "issue_comment"
)

// SchemaVersion is the version of the on-disk JSONL schema.
const SchemaVersion = 1

// Comment is the normalized record written to corpus.jsonl. One record per
// review body, inline review comment, or PR issue comment.
type Comment struct {
	SchemaVersion int         `json:"schema_version"`
	ID            int64       `json:"id"`
	NodeID        string      `json:"node_id,omitempty"`
	Type          CommentType `json:"type"`

	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`

	Author      string `json:"author,omitempty"`
	AuthorIsBot bool   `json:"author_is_bot,omitempty"`

	Body         string `json:"body"`
	BodyRedacted bool   `json:"body_redacted,omitempty"`
	URL          string `json:"url,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`

	// review_body / review_comment fields
	ReviewID    int64  `json:"review_id,omitempty"`
	ReviewState string `json:"review_state,omitempty"`

	// review_comment (inline) fields
	Path         string `json:"path,omitempty"`
	Line         int    `json:"line,omitempty"`
	OriginalLine int    `json:"original_line,omitempty"`
	Side         string `json:"side,omitempty"`
	DiffHunk     string `json:"diff_hunk,omitempty"`
	InReplyTo    int64  `json:"in_reply_to,omitempty"`
	Outdated     bool   `json:"outdated,omitempty"`
}

// PR is the normalized record written to prs.jsonl. One record per pull request
// included in the dataset, used for joining metadata back onto comments.
type PR struct {
	SchemaVersion int      `json:"schema_version"`
	Repo          string   `json:"repo"`
	Number        int      `json:"number"`
	Title         string   `json:"title,omitempty"`
	URL           string   `json:"url,omitempty"`
	State         string   `json:"state,omitempty"`
	Merged        bool     `json:"merged,omitempty"`
	Draft         bool     `json:"draft,omitempty"`
	Author        string   `json:"author,omitempty"`
	AuthorIsBot   bool     `json:"author_is_bot,omitempty"`
	BaseRef       string   `json:"base_ref,omitempty"`
	HeadRef       string   `json:"head_ref,omitempty"`
	Labels        []string `json:"labels,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	MergedAt  *time.Time `json:"merged_at,omitempty"`
}

// Filters captures the filter parameters used to build a dataset. Persisted in
// manifest.json so consumers can verify what the dataset represents and so
// future runs can detect option mismatches.
type Filters struct {
	Repos        []string   `json:"repos,omitempty"`
	State        string     `json:"state,omitempty"`
	MergedOnly   bool       `json:"merged_only,omitempty"`
	Since        *time.Time `json:"since,omitempty"`
	Until        *time.Time `json:"until,omitempty"`
	Labels       []string   `json:"labels,omitempty"`
	CommentTypes []string   `json:"comment_types,omitempty"`
	IncludeBots  bool       `json:"include_bots,omitempty"`
	MinLength    int        `json:"min_length,omitempty"`
	Paths        []string   `json:"paths,omitempty"`
	Limit        int        `json:"limit,omitempty"`
	NoRedact     bool       `json:"no_redact,omitempty"`
}

// Manifest describes a dataset directory.
type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Filters       Filters   `json:"filters"`
	Counts        Counts    `json:"counts"`
}

// Counts holds running totals for the dataset.
type Counts struct {
	PRs            int `json:"prs"`
	ReviewBodies   int `json:"review_bodies"`
	ReviewComments int `json:"review_comments"`
	IssueComments  int `json:"issue_comments"`
}
