//go:build windows

package copilot

import "os/exec"

// setProcessGroup is a no-op: Windows has no process groups to join, and the
// copilot executable is not launched through a forking shell wrapper there.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills cmd.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
