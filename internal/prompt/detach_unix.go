//go:build !windows

package prompt

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts a process in its own session, so it outlives the agent's
// shell call and is not killed with it.
func detach(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// Alive reports whether a process with this id is still running.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
