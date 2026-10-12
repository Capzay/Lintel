//go:build windows

package prompt

import (
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200
const detachedProcess = 0x00000008

// detach starts a process outside the agent's console, so it outlives the
// agent's shell call.
func detach(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
	return cmd.Start()
}

// Alive cannot probe a Windows process without extra APIs, so the pid file's
// age decides instead (see Launch).
func Alive(pid int) bool {
	return pid > 0
}
