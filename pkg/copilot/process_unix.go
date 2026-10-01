//go:build !windows

package copilot

import (
	"os/exec"
	"syscall"
)

// setProcessGroup isolates the process tree for cancellation. Claude also
// needs a new session: its shell initialization can access /dev/tty, which
// stops a background process group under terminal job control.
func setProcessGroup(cmd *exec.Cmd, detach bool) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: !detach, Setsid: detach}
}

// killProcessGroup kills cmd together with every process it spawned, falling
// back to cmd alone when it never became a process group leader.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
