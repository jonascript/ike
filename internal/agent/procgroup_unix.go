//go:build unix

package agent

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the agent in a process group of its own, so the whole
// tree can be signaled at once.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup signals the agent and everything it started.
//
// The negative PID is what addresses the group rather than the one process.
// That matters here more than it usually does: the agent runs shell commands
// and spawns tools of its own, and killing only the parent would leave those
// running against the user's files after the run appeared to stop.
//
// SIGTERM rather than SIGKILL, so the CLI can shut its own children down
// cleanly. A run that ignores it is reaped by the CommandContext cancellation
// the caller has already triggered.
func killProcessGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
