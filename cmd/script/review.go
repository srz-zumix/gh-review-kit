package script

import (
	"errors"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	pkgscript "github.com/srz-zumix/gh-review-kit/pkg/script"
)

func NewReviewCmd() *cobra.Command {
	return newCommand(false)
}

func newCommand(execute bool) *cobra.Command {
	opts := pkgscript.DefaultOptions()
	run := pkgscript.RunOptions{Execute: execute, MaxRisk: "low", Format: "text"}
	cmd := &cobra.Command{
		Use:   "review SOURCE [flags]",
		Short: "Review a shell script without executing it",
		Long: `Fetch a shell script from an HTTPS URL, local file, or stdin (-) and review it
with built-in Bash syntax checks. Use --evaluate[=copilot|claude] for an additional
read-only AI review, --sha256 or --checksums-url to verify the root script,
and --depth to review nested script downloads (default 1, maximum 3).
Use --format=json for a report or --save to keep scripts and the report in a new directory.
Review is heuristic, not proof of safety. Runtime downloads can change after review;
binaries, archives, and dynamically constructed destinations are not inspected.
Checksums verify bytes, not publisher identity. No GitHub authentication is used for downloads.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if execute {
				if dash := cmd.ArgsLenAtDash(); dash >= 0 {
					run.Args = args[dash:]
				}
			}
			err := pkgscript.Workflow(cmd.Context(), args[0], cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, run)
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() > 0 {
				os.Exit(exit.ExitCode())
			}
			return err
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&opts.AllowHTTP, "allow-http", false, "Permit HTTP downloads (including redirects)")
	flags.StringVar(&opts.Bin, "bin", "", "AI CLI executable (default: copilot or claude)")
	flags.StringVar(&opts.ChecksumsURL, "checksums-url", "", "URL of a SHA256SUMS file for the named root script")
	flags.IntVar(&opts.Depth, "depth", opts.Depth, "Nested script review depth (0-3); at most 16 scripts are fetched")
	flags.StringVar(&opts.Evaluator, "evaluate", "", "Add AI review with copilot or claude (bare flag selects copilot)")
	flags.Lookup("evaluate").NoOptDefVal = "copilot"
	flags.StringVar(&run.Format, "format", run.Format, "Report format: text or json")
	flags.StringVar(&opts.Language, "language", "", "AI response language (default: evaluator's language)")
	flags.Int64Var(&opts.MaxSize, "max-size", opts.MaxSize, "Maximum bytes per script/checksum file (1-67108864)")
	flags.StringVar(&opts.Model, "model", "", "AI model (default: evaluator's model)")
	flags.StringVar(&opts.PromptFile, "prompt-file", "", "File containing additional AI review instructions")
	flags.StringVar(&run.Save, "save", "", "Save scripts and report.json to a new private directory")
	flags.StringVar(&opts.SHA256, "sha256", "", "Expected root script SHA256 (64 hexadecimal characters)")
	flags.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "Timeout per download or AI invocation")
	cmd.MarkFlagsMutuallyExclusive("sha256", "checksums-url")
	if execute {
		flags.StringVar(&run.MaxRisk, "max-risk", run.MaxRisk, "Maximum risk for --yes or simple y/N confirmation: none, low, medium, high, critical")
		flags.StringVar(&run.Shell, "shell", "", "Override shell (sh, bash, dash, zsh, ksh; optionally an absolute path)")
		flags.BoolVar(&run.Yes, "yes", false, "Skip confirmation, but enforce --max-risk")
	}
	return cmd
}
