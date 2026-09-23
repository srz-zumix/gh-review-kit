package copilot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	pkgcopilot "github.com/srz-zumix/gh-review-kit/pkg/copilot"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewCommentsCmd creates a new command to list, and optionally evaluate, Copilot review comments on a pull request.
func NewCommentsCmd() *cobra.Command {
	var (
		repo            string
		prIdentifier    string
		authors         []string
		includeResolved bool
		includeOutdated bool
		evaluate        bool
		batch           bool
		prompt          string
		promptFile      string
		copilotBin      string
		agent           string
		allowAllTools   bool
		model           string
		evaluateTimeout time.Duration
		sandbox         bool
		sessionID       string
		rubberDuck      bool
		dryRun          bool
		language        string
		opts            struct{ Exporter cmdutil.Exporter }
	)

	cmd := &cobra.Command{
		Use:   "comments [-- copilot-cli-arg...]",
		Short: "List Copilot code review comments on a pull request",
		Long: `List GitHub Copilot code review comments on a pull request.

By default, resolved and outdated review threads are excluded. Use --evaluate
to additionally judge each comment with the Copilot CLI: comments judged
invalid receive a thumbs-down reaction and have their review thread resolved,
while comments judged valid have their review thread resolved without a
reaction. The prompt used to judge comments must be supplied with --prompt or
--prompt-file.

The Copilot CLI runs in non-interactive mode (-p) for evaluation, so it can
never prompt for tool permissions and denies anything not pre-authorized,
regardless of whether the command is run from a terminal or in CI. Use
--allow-all-tools to allow every tool, or pass arguments after a -- separator
to forward them to the Copilot CLI, e.g. -- --allow-tool=... --deny-tool=...
to scope permissions more tightly (note that an organization's Copilot policy
may disable these bypass options entirely). Without either, a warning is
printed before evaluation starts, and denied tool calls are reported as a
warning afterward, since they may leave the Copilot CLI's judgement based on
incomplete information.

The Copilot CLI keeps tool, path, and URL permissions in separate categories,
so --allow-all-tools authorizes tool execution but not file access outside
the working directory. Denied tool calls are therefore also reported with the
options that would have allowed them, as a -- passthrough list to paste onto
a re-run, e.g. -- --allow-tool='shell(go:*)' --add-dir=/path/to/module/cache.
Paths are always narrowed to --add-dir; --allow-all-paths is never
recommended. Note that --sandbox enforces an OS-level filesystem policy on
top of these permissions, so denials on paths no --add-dir can reach, such as
symlinks out of the working directory or network access, persist until
--sandbox is dropped.

Use --model to select the Copilot CLI model used for evaluation, and
--rubber-duck to additionally ask the Copilot CLI's built-in rubber duck
agent for a second opinion before it decides; the rubber duck agent runs on a
different model, so this adds latency and model usage.

Every comment is judged with a single Copilot CLI invocation, which avoids
repeated context and keeps AI credits down at the cost of a single combined
judgement pass. Use --batch=false to run one invocation per comment instead.

Each run starts a new Copilot CLI session, so runs never inherit each other's
context. The session ID is written to stderr both before and after the
evaluation, along with the Copilot CLI output and the AI credits the session
consumed. Pass that ID back with --session-id to resume the session, for
example to keep the context of a previous run.

Use --sandbox to enable the Copilot CLI's OS-level shell sandbox for the
evaluation. This also passes --experimental, since the Copilot CLI otherwise
ignores --sandbox, and --add-dir for the directory the command runs in, since
the sandbox otherwise blocks reading the checked-out repository. When paths
are recommended, a ~/.copilot/settings.json fragment granting them under
sandbox.userPolicy.filesystem.readonlyPaths is printed as well, so the grant
can be made permanent instead of repeated on every run; the Copilot CLI reads
repository settings (.github/copilot/settings.json and settings.local.json)
only in interactive mode, so they have no effect here.

Use --language to have the evaluation reason written in a specific language.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if dash := cmd.ArgsLenAtDash(); dash > 0 || (dash < 0 && len(args) > 0) {
				return fmt.Errorf("accepts no positional arguments; use -- to forward arguments to the Copilot CLI")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if evaluate {
				if (prompt == "") == (promptFile == "") {
					return fmt.Errorf("--evaluate requires exactly one of --prompt or --prompt-file")
				}
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

			comments, err := pkgcopilot.ListComments(ctx, client, repository, pr, pkgcopilot.ListOptions{
				Authors:         authors,
				IncludeResolved: includeResolved,
				IncludeOutdated: includeOutdated,
			})
			if err != nil {
				return fmt.Errorf("failed to list copilot review comments for pull request #%d: %w", pr.GetNumber(), err)
			}

			renderer := render.NewRenderer(opts.Exporter)

			if !evaluate {
				return pkgcopilot.RenderComments(renderer, comments)
			}

			promptText := prompt
			if promptFile != "" {
				b, err := os.ReadFile(promptFile)
				if err != nil {
					return fmt.Errorf("failed to read prompt file '%s': %w", promptFile, err)
				}
				promptText = string(b)
			}

			repoSlug := fmt.Sprintf("%s/%s", repository.Owner, repository.Name)
			if sessionID == "" {
				sessionID = pkgcopilot.NewSessionID()
			}
			log := cmd.ErrOrStderr()

			logger.Info("Copilot session", "session_id", sessionID)

			if pkgcopilot.LooksLikeSlashCommand(promptText) {
				logger.Warn("Prompt starts with what looks like a slash command; the Copilot CLI's -p mode passes it through as literal text instead of expanding it", "hint", "use --agent for a custom agent, or --rubber-duck for the rubber duck agent")
			}
			if pkgcopilot.NeedsToolPermissionWarning(allowAllTools, args) {
				logger.Warn("No tool permissions were pre-authorized; the Copilot CLI's -p mode cannot prompt for approval, so any tool call that needs one will be denied", "hint", "use --allow-all-tools, or -- --allow-tool=... to scope permissions more tightly")
			}

			evalOpts := pkgcopilot.EvaluateOptions{
				Bin:           copilotBin,
				Agent:         agent,
				Prompt:        promptText,
				AllowAllTools: allowAllTools,
				Model:         model,
				RubberDuck:    rubberDuck,
				ExtraArgs:     args,
				Timeout:       evaluateTimeout,
				SessionID:     sessionID,
				Sandbox:       sandbox,
				Language:      language,
				Log:           log,
			}

			results, usage, evalErr := pkgcopilot.EvaluateAll(ctx, client, repository, evalOpts, repoSlug, pr.GetNumber(), comments, batch, dryRun)

			if err := pkgcopilot.RenderEvaluationResults(renderer, results); err != nil {
				return err
			}
			if usage != nil {
				logger.Info("AI credits", "credits", usage.AICredits)
				if usage.QuotaExceeded {
					logger.Warn("The Copilot CLI ran out of quota and stopped before finishing; any missing verdict is a consequence of that, not of the comment",
						"hint", "wait for the quota to reset or upgrade the plan, then re-run")
				}
				if len(usage.Denials) > 0 {
					logger.Warn("Tool calls were denied; the evaluation may be based on incomplete information",
						"count", len(usage.Denials),
						"denied", strings.Join(pkgcopilot.SummarizeDenials(usage.Denials), ", "))
				}
				if len(usage.Recommendations) > 0 {
					logger.Warn("Re-run with these Copilot CLI options to allow the denied tool calls",
						"options", "-- "+pkgcopilot.FormatRecommendations(usage.Recommendations))
					if sandbox {
						logger.Warn(pkgcopilot.SandboxDenialNote)
						if hint := pkgcopilot.SandboxSettingsHint(usage.Recommendations, usage.WritablePaths); hint != "" {
							logger.Warn("Merge this into ~/.copilot/settings.json to grant the paths for every run",
								"settings", hint)
						}
					}
				}
			}
			logger.Info("Copilot session", "session_id", sessionID, "hint", "pass --session-id to resume it")
			return evalErr
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&prIdentifier, "pr", "", "Pull request number, URL, or branch name (default: current branch)")
	f.StringSliceVar(&authors, "author", nil, "Comment author logins to match (default: the Copilot code review bot)")
	f.BoolVar(&includeResolved, "include-resolved", false, "Include comments whose review thread is already resolved")
	f.BoolVar(&includeOutdated, "include-outdated", false, "Include comments whose review thread is outdated")
	f.BoolVar(&evaluate, "evaluate", false, "Judge each comment with the Copilot CLI and act on the verdict")
	f.BoolVar(&batch, "batch", true, "Judge every comment with a single Copilot CLI invocation instead of one per comment")
	f.StringVarP(&prompt, "prompt", "p", "", "Prompt used to judge comments with the Copilot CLI (mutually exclusive with --prompt-file)")
	f.StringVar(&promptFile, "prompt-file", "", "File containing the prompt used to judge comments with the Copilot CLI (mutually exclusive with --prompt)")
	f.StringVar(&copilotBin, "copilot-bin", "copilot", "Copilot CLI executable name or path")
	f.StringVar(&agent, "agent", "", "Copilot CLI custom agent to use for evaluation")
	f.BoolVar(&allowAllTools, "allow-all-tools", false, "Allow the Copilot CLI to use any tool without approval during evaluation")
	f.StringVar(&model, "model", "", "Copilot CLI model to use for evaluation (default: the Copilot CLI's default model)")
	f.DurationVar(&evaluateTimeout, "evaluate-timeout", 15*time.Minute, "Timeout for a single Copilot CLI evaluation, per comment (a batch run is given this much for every comment it covers)")
	f.BoolVar(&sandbox, "sandbox", false, "Enable the Copilot CLI's OS-level shell sandbox for the evaluation (also passes --experimental and --add-dir for the current directory)")
	f.StringVar(&sessionID, "session-id", "", "Copilot CLI session to resume (default: a new session)")
	f.BoolVar(&rubberDuck, "rubber-duck", false, "Ask the Copilot CLI's built-in rubber duck agent for a second opinion before deciding")
	f.BoolVarP(&dryRun, "dryrun", "n", false, "Report the action that would be taken without performing it")
	f.StringVar(&language, "language", "", "Language for the Copilot CLI's evaluation reason (default: the Copilot CLI's default language)")
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}
