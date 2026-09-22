package copilot

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// ListOptions controls which review threads are returned by ListComments.
type ListOptions struct {
	// Authors overrides the default Copilot author login used to identify
	// comments. When empty, DefaultAuthor is used.
	Authors []string
	// IncludeResolved includes comments whose review thread is already resolved.
	IncludeResolved bool
	// IncludeOutdated includes comments whose review thread is outdated.
	IncludeOutdated bool
}

// ListComments lists Copilot review comments for a pull request, filtered by
// author login and, by default, excluding resolved and outdated threads.
func ListComments(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, pull_request any, opts ListOptions) ([]*Comment, error) {
	authors := opts.Authors
	if len(authors) == 0 {
		authors = []string{DefaultAuthor}
	}

	threads, err := gh.ListPullRequestReviewThreads(ctx, g, repo, pull_request)
	if err != nil {
		return nil, err
	}

	var comments []*Comment
	for _, thread := range threads {
		if !opts.IncludeResolved && thread.IsResolved {
			continue
		}
		if !opts.IncludeOutdated && thread.IsOutdated {
			continue
		}
		for _, c := range thread.Comments {
			if !isAuthorMatch(c.Author, authors) {
				continue
			}
			comments = append(comments, &Comment{
				ThreadID:   thread.ID,
				CommentID:  c.DatabaseID,
				URL:        c.URL,
				Author:     c.Author,
				Body:       c.Body,
				Path:       c.Path,
				Line:       c.Line,
				DiffHunk:   c.DiffHunk,
				IsResolved: thread.IsResolved,
				IsOutdated: thread.IsOutdated,
				CreatedAt:  c.CreatedAt,
			})
		}
	}
	return comments, nil
}

// FindComment locates a Copilot comment by its comment ID within a pull request's
// review threads. It always includes resolved and outdated threads so that
// feedback and resolve operations can target any existing comment.
func FindComment(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, pull_request any, commentID int64, authors []string) (*Comment, error) {
	comments, err := ListComments(ctx, g, repo, pull_request, ListOptions{
		Authors:         authors,
		IncludeResolved: true,
		IncludeOutdated: true,
	})
	if err != nil {
		return nil, err
	}
	for _, c := range comments {
		if c.CommentID == commentID {
			return c, nil
		}
	}
	return nil, fmt.Errorf("comment %d not found among Copilot review comments", commentID)
}

func isAuthorMatch(author string, authors []string) bool {
	for _, a := range authors {
		if strings.EqualFold(author, a) {
			return true
		}
	}
	return false
}
