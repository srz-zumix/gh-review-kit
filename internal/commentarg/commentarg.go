// Package commentarg parses positional comment arguments accepted by the
// gh review-kit copilot feedback/resolve commands.
package commentarg

import (
	"fmt"
	"strconv"

	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// Parse parses a positional comment argument, which may be a bare comment
// ID or a pull request review comment URL (https://.../pull/N#discussion_r<id>).
// prNumber is 0 when arg does not carry its own pull request number.
func Parse(arg string) (commentID int64, prNumber int, err error) {
	if id, convErr := strconv.ParseInt(arg, 10, 64); convErr == nil {
		return id, 0, nil
	}

	parsed, err := parser.ParsePullRequestReviewCommentURL(arg)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid comment ID or review comment URL '%s': %w", arg, err)
	}
	if parsed == nil {
		return 0, 0, fmt.Errorf("invalid comment ID or review comment URL '%s'", arg)
	}
	if parsed.PRNumber != nil {
		prNumber = *parsed.PRNumber
	}
	return parsed.CommentID, prNumber, nil
}
