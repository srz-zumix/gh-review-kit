//go:build !windows

package copilot

import (
	"os/exec"
	"testing"
)

func TestSetProcessGroup(t *testing.T) {
	for _, tt := range []struct {
		name   string
		detach bool
	}{
		{"copilot", false},
		{"claude", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("true")
			setProcessGroup(cmd, tt.detach)
			if cmd.SysProcAttr == nil || cmd.SysProcAttr.Setsid != tt.detach || cmd.SysProcAttr.Setpgid == tt.detach {
				t.Errorf("process attributes = %+v, want Setsid=%v and Setpgid=%v", cmd.SysProcAttr, tt.detach, !tt.detach)
			}
			if err := cmd.Run(); err != nil {
				t.Fatalf("cmd.Run() error = %v", err)
			}
		})
	}
}
