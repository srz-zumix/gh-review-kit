//go:build !windows

package copilot

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts cmd in a process group of its own so that the whole
// process tree can be signalled at once.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
