package copilot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cli/cli/v2/pkg/cmdutil"
	ghrepo "github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	pkgcopilot "github.com/srz-zumix/gh-review-kit/pkg/copilot"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// deprecatedFlagReplacements maps the deprecated Copilot CLI specific flags to their replacements.
var deprecatedFlagReplacements = map[string]string{
	"copilot-bin":     "bin",
	"allow-all-tools": "auto-approve",
}

// NewCommentsCmd creates a new command to list, and optionally evaluate, Copilot review comments on a pull request.
func NewCommentsCmd() *cobra.Command {
	var (
		repo            string
		prIdentifier    string
		authors         []string
		includeResolved bool
		includeOutdated bool
		batch           bool
		prompt          string
		promptFile      string
		evaluatorName   string
		aliasName       string
		bin             string
		agent           string
		autoApprove     bool
		model           string
		evaluateTimeout time.Duration
		sandbox         bool
		sessionID       string
		rubberDuck      bool
		dryRun          bool
		language        string
		checkWorktree   bool
		opts            struct{ Exporter cmdutil.Exporter }
	)

	cmd := &cobra.Command{
		Use:   "comments [-- agent-cli-arg...]",
		Short: "List Copilot code review comments on a pull request",
		Long: `List GitHub Copilot code review comments on a pull request.

By default, resolved and outdated review threads are excluded. Use --evaluate
to additionally judge each comment with the Copilot CLI (or with Claude Code,
see --evaluate=claude): comments judged invalid receive a thumbs-down reaction and
have their review thread resolved,
while comments judged valid have their review thread resolved without a
reaction. The prompt used to judge comments must be supplied with --prompt or
--prompt-file.

The Copilot CLI runs in non-interactive mode (-p) for evaluation, so it can
never prompt for tool permissions and denies anything not pre-authorized,
regardless of whether the command is run from a terminal or in CI. Use
--auto-approve to apply the permission setting review-kit recommends for the
Copilot CLI (--allow-all-tools, which allows every tool), or pass arguments
after a -- separator to forward them to the Copilot CLI, e.g.
-- --allow-tool=... --deny-tool=... to scope permissions more tightly (note
that an organization's Copilot policy may disable these bypass options
entirely). Without either, a warning is
printed before evaluation starts, and denied tool calls are reported as a
warning afterward, since they may leave the Copilot CLI's judgement based on
incomplete information.

The Copilot CLI keeps tool, path, and URL permissions in separate categories,
so --auto-approve authorizes tool execution but not file access outside
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
Missing or duplicate comment IDs are retried once in the same session, for
only the affected comments. The retry asks for corrected JSON judgements
without repeating fixes, and runs without tool permissions whatever
--auto-approve or passthrough options grant: the Copilot CLI is limited to
the view, grep and glob tools with built-in MCP servers disabled and shell,
write, URL and memory access denied, and Claude Code has every tool denied. Successful judgements are preserved;
remaining failures are reported per comment. No retry runs after cancellation
or a reported quota limit.

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

Use --language to have the evaluation reason written in a specific language.

Use --alias-set NAME to register a gh alias instead of running: the other flags
given (and any arguments after --) are embedded in a shell alias, so that
"gh NAME [copilot|claude] [flags...]" runs this command with them. The
evaluator given to --evaluate becomes the alias's default (copilot otherwise),
and flags passed to the alias are appended, so they take precedence. A relative
--prompt-file, and a --bin given as a path rather than a bare executable name,
are stored as absolute paths. Deprecated flags are stored under their
replacement name. An existing alias with the same name is overwritten.

Use --evaluate=claude to judge comments with Claude Code instead of the
Copilot CLI (--evaluate alone, or --evaluate=copilot, selects the Copilot CLI;
the value must be attached with =); --bin selects its executable (default: claude). Claude Code also
runs in non-interactive mode (-p), so --auto-approve applies auto mode
(--permission-mode auto), and arguments after a -- separator are
forwarded to it, e.g. -- --allowedTools='Bash(go *)'. Denied tool calls are
reported with the --allowedTools and --add-dir options that would have allowed
them. --sandbox enables Claude Code's sandbox through --settings, --rubber-duck
is ignored with a warning, and the estimated cost Claude Code reports is
logged instead of AI credits. A session ID passed with --session-id is resumed
with --resume.

When run inside a local work tree of the repository, --evaluate first checks
that the current branch is the pull request's head branch and contains its
latest commit, and fails otherwise, so the Copilot CLI never judges comments
against stale code. When the check passes, the Copilot CLI is told that the
working directory reflects the pull request. Use --check-worktree=false to
skip the check.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if dash := cmd.ArgsLenAtDash(); dash > 0 || (dash < 0 && len(args) > 0) {
				return fmt.Errorf("accepts no positional arguments; use -- to forward arguments to the evaluator CLI")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("alias-set") {
				if aliasName == "" {
					return fmt.Errorf("--alias-set requires an alias name")
				}
				if (prompt == "") == (promptFile == "") {
					return fmt.Errorf("--alias-set requires exactly one of --prompt or --prompt-file")
				}
				defaultEvaluator, err := pkgcopilot.ParseEvaluator(evaluatorName)
				if err != nil {
					return fmt.Errorf("invalid --evaluate value: %w", err)
				}
				aliasFlags, err := pkgcopilot.CollectAliasFlags(cmd.Flags(), pkgcopilot.AliasFlagOptions{
					Skip:          []string{"alias-set", "evaluate"},
					PathFlags:     []string{"prompt-file"},
					ExecPathFlags: []string{"bin", "copilot-bin"},
					Rename:        deprecatedFlagReplacements,
				})
				if err != nil {
					return fmt.Errorf("failed to collect flags for alias '%s': %w", aliasName, err)
				}
				expansion := pkgcopilot.BuildAliasExpansion(defaultEvaluator, aliasFlags, args)
				if err := pkgcopilot.RegisterAlias(context.Background(), aliasName, expansion, cmd.ErrOrStderr()); err != nil {
					return fmt.Errorf("failed to register gh alias '%s': %w", aliasName, err)
				}
				return nil
			}

			evaluate := cmd.Flags().Changed("evaluate")
			var evaluator pkgcopilot.Evaluator
			if evaluate {
				if (prompt == "") == (promptFile == "") {
					return fmt.Errorf("--evaluate requires exactly one of --prompt or --prompt-file")
				}
				var err error
				evaluator, err = pkgcopilot.ParseEvaluator(evaluatorName)
				if err != nil {
					return fmt.Errorf("invalid --evaluate value: %w", err)
				}
				if evaluator.IsClaude() {
					for _, name := range []string{"copilot-bin", "allow-all-tools"} {
						if cmd.Flags().Changed(name) {
							return fmt.Errorf("--%s applies only to the Copilot CLI evaluator; use --%s instead", name, deprecatedFlagReplacements[name])
						}
					}
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

			var checkout *pkgcopilot.LocalCheckout
			if checkWorktree {
				head := pkgcopilot.PRHead{
					Number: pr.GetNumber(),
					Ref:    pr.GetHead().GetRef(),
					SHA:    pr.GetHead().GetSHA(),
					Repos: []ghrepo.Repository{
						repository,
						{Host: repository.Host, Owner: pr.GetHead().GetRepo().GetOwner().GetLogin(), Name: pr.GetHead().GetRepo().GetName()},
					},
				}
				checkout, err = pkgcopilot.CheckLocalCheckout(ctx, head)
				if err != nil {
					return fmt.Errorf("local work tree does not match pull request #%d (use --check-worktree=false to skip this check): %w", pr.GetNumber(), err)
				}
				if checkout != nil {
					logger.Info("Local work tree matches the pull request head", "branch", checkout.Branch, "sha", checkout.SHA)
				}
			}

			name := evaluator.DisplayName()
			// An existing session must be resumed rather than created again.
			resumeSession := sessionID != ""
			if sessionID == "" {
				sessionID = pkgcopilot.NewSessionID()
			}
			log := cmd.ErrOrStderr()

			logger.Info(name+" session", "session_id", sessionID)

			if !evaluator.IsClaude() && pkgcopilot.LooksLikeSlashCommand(promptText) {
				logger.Warn("Prompt starts with what looks like a slash command; the Copilot CLI's -p mode passes it through as literal text instead of expanding it", "hint", "use --agent for a custom agent, or --rubber-duck for the rubber duck agent")
			}
			if pkgcopilot.NeedsToolPermissionWarning(evaluator, autoApprove, args) {
				hint := "use --auto-approve, or -- --allow-tool=... to scope permissions more tightly"
				if evaluator.IsClaude() {
					hint = "use --auto-approve, or -- --allowedTools=... to scope permissions more tightly"
				}
				logger.Warn("No tool permissions were pre-authorized; "+name+"'s -p mode cannot prompt for approval, so any tool call that needs one will be denied", "hint", hint)
			}
			if evaluator.IsClaude() {
				if rubberDuck {
					logger.Warn("--rubber-duck is ignored: Claude Code has no rubber duck agent")
				}
				if sandbox {
					logger.Warn("--sandbox enables Claude Code's sandbox through --settings; a --settings passed after -- takes precedence")
				}
			}

			evalOpts := pkgcopilot.EvaluateOptions{
				Evaluator:     evaluator,
				Bin:           bin,
				Agent:         agent,
				Prompt:        promptText,
				AutoApprove:   autoApprove,
				Model:         model,
				RubberDuck:    rubberDuck,
				ExtraArgs:     args,
				Timeout:       evaluateTimeout,
				SessionID:     sessionID,
				ResumeSession: resumeSession,
				Sandbox:       sandbox,
				Language:      language,
				LocalCheckout: checkout,
				Log:           log,
			}

			results, usage, evalErr := pkgcopilot.EvaluateAll(ctx, client, repository, evalOpts, repoSlug, pr.GetNumber(), comments, batch, dryRun)

			if err := pkgcopilot.RenderEvaluationResults(renderer, results); err != nil {
				return err
			}
			if usage != nil {
				if evaluator.IsClaude() {
					logger.Info("Cost", "usd", usage.CostUSD)
				} else {
					logger.Info("AI credits", "credits", usage.AICredits)
				}
				if usage.InputTokens > 0 || usage.OutputTokens > 0 {
					logger.Info("Tokens", "input", usage.InputTokens, "output", usage.OutputTokens, "cached", usage.CachedTokens)
				}
				if usage.QuotaExceeded {
					logger.Warn(name+" ran out of quota and stopped before finishing; any missing verdict is a consequence of that, not of the comment",
						"hint", "wait for the quota to reset or upgrade the plan, then re-run")
				}
				if len(usage.Denials) > 0 {
					logger.Warn("Tool calls were denied; the evaluation may be based on incomplete information",
						"count", len(usage.Denials),
						"denied", strings.Join(pkgcopilot.SummarizeDenials(usage.Denials), ", "))
				}
				if len(usage.Recommendations) > 0 {
					logger.Warn("Re-run with these "+name+" options to allow the denied tool calls",
						"options", "-- "+pkgcopilot.FormatRecommendations(usage.Recommendations))
					if sandbox && !evaluator.IsClaude() {
						logger.Warn(pkgcopilot.SandboxDenialNote)
						if hint := pkgcopilot.SandboxSettingsHint(usage.Recommendations, usage.WritablePaths); hint != "" {
							logger.Warn("Merge this into ~/.copilot/settings.json to grant the paths for every run",
								"settings", hint)
						}
					}
				}
			}
			logger.Info(name+" session", "session_id", sessionID, "hint", "pass --session-id to resume it")
			return evalErr
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&prIdentifier, "pr", "", "Pull request number, URL, or branch name (default: current branch)")
	f.StringSliceVar(&authors, "author", nil, "Comment author logins to match (default: the Copilot code review bot)")
	f.BoolVar(&includeResolved, "include-resolved", false, "Include comments whose review thread is already resolved")
	f.BoolVar(&includeOutdated, "include-outdated", false, "Include comments whose review thread is outdated")
	f.StringVar(&evaluatorName, "evaluate", "", "Judge each comment with the Copilot CLI or Claude Code and act on the verdict; use --evaluate=copilot|claude to pick the CLI (default: copilot)")
	f.Lookup("evaluate").NoOptDefVal = string(pkgcopilot.EvaluatorCopilot)
	f.BoolVar(&batch, "batch", true, "Judge every comment with a single CLI invocation instead of one per comment")
	f.StringVarP(&prompt, "prompt", "p", "", "Prompt used to judge comments (mutually exclusive with --prompt-file)")
	f.StringVar(&promptFile, "prompt-file", "", "File containing the prompt used to judge comments (mutually exclusive with --prompt)")
	f.StringVar(&bin, "bin", "", "Evaluator CLI executable name or path (default: copilot, or claude with --evaluate=claude)")
	f.StringVar(&aliasName, "alias-set", "", "Register a gh alias with this name that runs this command with the other given flags, then exit; the alias takes an optional leading copilot|claude and further flags")
	// Deprecated aliases kept for compatibility with scripts written for earlier releases.
	f.StringVar(&bin, "copilot-bin", "", "Copilot CLI executable name or path")
	f.BoolVar(&autoApprove, "allow-all-tools", false, "Allow the Copilot CLI to use any tool without approval during evaluation")
	for name, replacement := range deprecatedFlagReplacements {
		_ = f.MarkDeprecated(name, fmt.Sprintf("use --%s instead", replacement))
	}
	f.StringVar(&agent, "agent", "", "Custom agent to use for evaluation")
	f.BoolVar(&autoApprove, "auto-approve", false, "Apply the tool permission setting review-kit recommends for the evaluator CLI, so evaluation runs without approval prompts (Copilot CLI: --allow-all-tools, Claude Code: --permission-mode auto)")
	f.StringVar(&model, "model", "", "Model to use for evaluation (default: the evaluator CLI's default model)")
	f.DurationVar(&evaluateTimeout, "evaluate-timeout", 15*time.Minute, "Timeout for a single CLI evaluation, per comment (a batch run is given this much for every comment it covers)")
	f.BoolVar(&sandbox, "sandbox", false, "Enable the evaluator CLI's OS-level shell sandbox for the evaluation (Copilot CLI also gets --experimental and --add-dir for the current directory)")
	f.StringVar(&sessionID, "session-id", "", "Session to resume (default: a new session)")
	f.BoolVar(&rubberDuck, "rubber-duck", false, "Ask the Copilot CLI's built-in rubber duck agent for a second opinion before deciding (ignored for Claude Code)")
	f.BoolVarP(&dryRun, "dryrun", "n", false, "Report the action that would be taken without performing it")
	f.StringVar(&language, "language", "", "Language for the evaluation reason (default: the evaluator CLI's default language)")
	f.BoolVar(&checkWorktree, "check-worktree", true, "With --evaluate, verify that a local work tree of the repository is on the pull request's head branch and contains its latest commit")
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}
