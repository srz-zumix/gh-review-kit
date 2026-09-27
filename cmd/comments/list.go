package comments

import (
	"context"
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/insights"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewListCmd creates the 'comments list' command.
func NewListCmd() *cobra.Command {
	var (
		repo         string
		commentTypes []string
		includeBots  bool
		minLength    int
		paths        []string
		noRedact     bool
		colorFlag    string
		opts         struct{ Exporter cmdutil.Exporter }
	)

	cmd := &cobra.Command{
		Use:   "list [pull-request-identifier]",
		Short: "List all review feedback on a pull request",
		Long: `List every kind of review feedback on a pull request: review bodies,
inline review comments, and PR issue comments, merged into a single list
ordered by creation time.

Unlike 'gh pr view --comments', which only returns issue comments, this
command also includes review bodies and inline review comments, so it never
misses feedback left as part of a review.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prIdentifier := ""
			if len(args) > 0 {
				prIdentifier = args[0]
			}

			types, err := insights.CommentTypesFromStrings(commentTypes)
			if err != nil {
				return fmt.Errorf("invalid --comment-types: %w", err)
			}

			repository, err := parser.Repository(parser.RepositoryInput(repo), parser.RepositoryFromURL(prIdentifier))
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(repository)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			ctx := context.Background()

			pr, err := gh.FindPRByIdentifier(ctx, client, repository, prIdentifier)
			if err != nil {
				return fmt.Errorf("failed to get pull request %s: %w", prIdentifier, err)
			}

			comments, err := insights.ListPRComments(ctx, client, repository, pr, insights.ExtractOptions{
				CommentTypes: types,
				IncludeBots:  includeBots,
				MinLength:    minLength,
				Paths:        paths,
				NoRedact:     noRedact,
			})
			if err != nil {
				return fmt.Errorf("failed to list review feedback for pull request #%d: %w", pr.GetNumber(), err)
			}

			renderer := render.NewRenderer(opts.Exporter)
			renderer.SetColor(colorFlag)
			return insights.RenderPRComments(renderer, comments)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringSliceVar(&commentTypes, "comment-types", nil, "Comment types to include, repeatable (optional, default: all). Allowed: review_body, review_comment, issue_comment")
	f.BoolVar(&includeBots, "include-bots", false, "Include comments authored by bot users (optional, default: false)")
	f.IntVar(&minLength, "min-length", 0, "Skip comments whose trimmed body is shorter than this many bytes (optional, default: 0)")
	f.StringSliceVar(&paths, "path", nil, "Restrict inline review comments to these path prefixes, repeatable (optional)")
	f.BoolVar(&noRedact, "no-redact", false, "Disable conservative secret/token redaction (optional, default: false)")
	cmdutil.StringEnumFlag(cmd, &colorFlag, "color", "", render.ColorFlagAuto, render.ColorFlags, "Use color in output")
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}
