package copilot

import (
	"fmt"
	"strings"
	"time"

	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// RenderComments writes comments using r, one "key=value" block per comment,
// separated by blank lines, or as exported data (e.g. JSON) when r has an
// exporter configured via --format/--jq. A table would truncate or wrap the
// comment body unreadably, so a block layout is used instead.
func RenderComments(r *render.Renderer, comments []*Comment) error {
	if r.HasExporter() {
		return r.RenderExportedData(comments)
	}
	if len(comments) == 0 {
		r.WriteLine("no copilot review comments found")
		return nil
	}
	for i, c := range comments {
		if i > 0 {
			r.WriteLine("")
		}
		r.WriteLine(fmt.Sprintf("%d %s:%d", c.CommentID, c.Path, c.Line))
		r.WriteLine(fmt.Sprintf("resolved=%t", c.IsResolved))
		r.WriteLine(fmt.Sprintf("outdated=%t", c.IsOutdated))
		r.WriteLine(fmt.Sprintf("url=%s", c.URL))
		writeSources(r, c.Sources)
		r.WriteLine(fmt.Sprintf("body=%s", c.Body))
	}
	return nil
}

// RenderEvaluationResults writes evaluation results using r, one
// "key=value" block per result, separated by blank lines, or as exported
// data (e.g. JSON) when r has an exporter configured. A table would
// truncate or wrap the verdict reason unreadably, so a block layout is used
// instead.
func RenderEvaluationResults(r *render.Renderer, results []*EvaluationResult) error {
	if r.HasExporter() {
		return r.RenderExportedData(results)
	}
	if len(results) == 0 {
		r.WriteLine("no copilot review comments found")
		return nil
	}
	for i, res := range results {
		if i > 0 {
			r.WriteLine("")
		}
		verdict := ""
		reason := ""
		if res.Evaluation != nil {
			verdict = string(res.Evaluation.Verdict)
			reason = res.Evaluation.Reason
		}
		r.WriteLine(fmt.Sprintf("%d %s", res.Comment.CommentID, res.Comment.URL))
		r.WriteLine(fmt.Sprintf("verdict=%s", verdict))
		r.WriteLine(fmt.Sprintf("action=%s", res.Action))
		r.WriteLine(fmt.Sprintf("reason=%s", reason))
		writeSources(r, res.Comment.Sources)
		if res.Reply != nil {
			r.WriteLine(fmt.Sprintf("reply=%s", res.Reply.Status))
			if res.Reply.CommentID != 0 {
				r.WriteLine(fmt.Sprintf("reply_id=%d", res.Reply.CommentID))
			}
			if res.Reply.URL != "" {
				r.WriteLine(fmt.Sprintf("reply_url=%s", res.Reply.URL))
			}
		}
		if res.Error != "" {
			r.WriteLine(fmt.Sprintf("error=%s", res.Error))
		}
	}
	if summary := SummarizeVerdictsBySource(results); hasAttributedSource(summary) {
		r.WriteLine("")
		r.WriteLine("verdicts by source:")
		for _, s := range summary {
			r.WriteLine(fmt.Sprintf("source=%s total=%d valid=%d invalid=%d unclear=%d failed=%d valid_rate=%.0f%%",
				s.Source, s.Total, s.Valid, s.Invalid, s.Unclear, s.Failed, s.ValidRate()*100))
		}
	}
	return nil
}

// writeSources writes the Copilot code review sources line when there are any.
func writeSources(r *render.Renderer, sources []gh.CopilotCodeReviewSource) {
	if len(sources) == 0 {
		return
	}
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = FormatSource(s)
	}
	r.WriteLine(fmt.Sprintf("sources=%s", strings.Join(names, ",")))
}

// RenderReviewStatus writes a Copilot review status using r as "key=value"
// lines, or as exported data (e.g. JSON) when r has an exporter configured.
func RenderReviewStatus(r *render.Renderer, status *ReviewStatus) error {
	if r.HasExporter() {
		return r.RenderExportedData(status)
	}
	lastReviewedAt := ""
	if status.LastReviewedAt != nil {
		lastReviewedAt = status.LastReviewedAt.Format(time.RFC3339)
	}
	r.WriteLine(fmt.Sprintf("#%d", status.PullRequest))
	r.WriteLine(fmt.Sprintf("status=%s", status.Status))
	r.WriteLine(fmt.Sprintf("requested=%t", status.Requested))
	r.WriteLine(fmt.Sprintf("reviews=%d", status.ReviewCount))
	r.WriteLine(fmt.Sprintf("last_review_state=%s", status.LastReviewState))
	r.WriteLine(fmt.Sprintf("last_reviewed_at=%s", lastReviewedAt))
	r.WriteLine(fmt.Sprintf("last_review_effort=%s", status.LastReviewEffort))
	r.WriteLine(fmt.Sprintf("comments=%d", status.Comments))
	r.WriteLine(fmt.Sprintf("unresolved_comments=%d", status.UnresolvedComments))
	r.WriteLine(fmt.Sprintf("head_reviewed=%t", status.HeadReviewed))
	return nil
}
