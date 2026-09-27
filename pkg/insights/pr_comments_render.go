package insights

import (
	"fmt"

	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// RenderPRComments writes comments using r, one "key=value" block per
// comment, separated by blank lines, or as exported data (e.g. JSON) when r
// has an exporter configured via --format/--jq. A table would truncate or
// wrap the comment body unreadably, so a block layout is used instead.
func RenderPRComments(r *render.Renderer, comments []*Comment) error {
	if r.HasExporter() {
		return r.RenderExportedData(comments)
	}
	if len(comments) == 0 {
		r.WriteLine("no review feedback found")
		return nil
	}
	for i, c := range comments {
		if i > 0 {
			r.WriteLine("")
		}
		if c.Type == CommentTypeReviewComment {
			// Line is 0 when the comment's diff position is outdated (GitHub
			// returns line=null in that case); fall back to OriginalLine so
			// the output still shows a useful line number.
			line := c.Line
			if line == 0 {
				line = c.OriginalLine
			}
			r.WriteLine(fmt.Sprintf("%d %s %s:%d", c.ID, c.Type, c.Path, line))
		} else {
			r.WriteLine(fmt.Sprintf("%d %s", c.ID, c.Type))
		}
		r.WriteLine(fmt.Sprintf("author=%s", c.Author))
		r.WriteLine(fmt.Sprintf("created_at=%s", c.CreatedAt.Format("2006-01-02T15:04:05Z07:00")))
		if c.ReviewState != "" {
			r.WriteLine(fmt.Sprintf("review_state=%s", c.ReviewState))
		}
		if c.Type == CommentTypeReviewComment {
			r.WriteLine(fmt.Sprintf("outdated=%t", c.Outdated))
		}
		r.WriteLine(fmt.Sprintf("url=%s", c.URL))
		r.WriteLine(fmt.Sprintf("body=%s", c.Body))
	}
	return nil
}
