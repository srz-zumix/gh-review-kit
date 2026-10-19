package script

import (
	"github.com/spf13/cobra"
)

func NewRunCmd() *cobra.Command {
	cmd := newCommand(true)
	cmd.Use = "run SOURCE [flags] [-- script-arg...]"
	cmd.Short = "Review and confirm before executing a shell script"
	cmd.Long += `

After review, confirm on /dev/tty with y/N, v to view, or r to display the report.
Above --max-risk (default low), type the exact word execute to approve instead of y.
With --yes, refuse execution above --max-risk; no interactive override is available.
Without a terminal, --yes is required. Execute the root script's reviewed bytes,
not a second download, with the shebang's shell or bash if no shebang is present.
Use --shell to override the interpreter. Arguments after -- go only to the script.
Execution is NOT sandboxed and has your user's permissions. --read-only forbids execution.
The script's exit code is preserved.`
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if dash := cmd.ArgsLenAtDash(); dash >= 0 {
			args = args[:dash]
		}
		return cobra.ExactArgs(1)(cmd, args)
	}
	return cmd
}
