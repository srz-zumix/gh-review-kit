package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewReviewedCmd creates a new command to mark files in a pull request as viewed
func NewReviewedCmd() *cobra.Command {
	var (
		repo         string
		prIdentifier string
	)

	cmd := &cobra.Command{
		Use:   "reviewed [file...]",
		Short: "Mark files in a pull request as viewed",
		Long: `Mark files in a pull request as viewed using the GitHub markFileAsViewed API.

If file paths are specified as arguments, only those files will be marked as viewed.
If no file paths are specified, all files marked as linguist-generated
in the repository's .gitattributes file will be marked as viewed.

The pull request can be specified with --pr. If omitted, the command
uses the pull request associated with the current branch.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			var targetFiles []string

			if len(args) > 0 {
				targetFiles = args
			} else {
				// Get linguist-generated files from the PR
				prFiles, err := gh.ListPullRequestFiles(ctx, client, repository, pr)
				if err != nil {
					return fmt.Errorf("failed to list files for pull request #%d: %w", pr.GetNumber(), err)
				}
				generatedFiles, err := gh.GetLinguistGenerated(ctx, client, repository, pr.GetHead().Ref, prFiles)
				if err != nil {
					return fmt.Errorf("failed to get linguist-generated files for pull request #%d: %w", pr.GetNumber(), err)
				}
				if len(generatedFiles) == 0 {
					fmt.Fprintln(os.Stdout, "No linguist-generated files found in pull request")
					return nil
				}
				for _, f := range generatedFiles {
					targetFiles = append(targetFiles, f.GetFilename())
				}
			}

			for _, filePath := range targetFiles {
				if err := gh.MarkPullRequestFileAsViewed(ctx, client, pr, filePath); err != nil {
					return fmt.Errorf("failed to mark file '%s' as viewed in pull request #%d: %w", filePath, pr.GetNumber(), err)
				}
				fmt.Fprintf(os.Stdout, "Marked as viewed: %s\n", filePath)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&prIdentifier, "pr", "", "Pull request number, URL, or branch name (default: current branch)")

	return cmd
}

func init() {
	rootCmd.AddCommand(NewReviewedCmd())
}
