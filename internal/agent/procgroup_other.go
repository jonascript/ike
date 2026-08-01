//go:build !unix

package agent

import "os/exec"

// ike ships for Linux and macOS only — .goreleaser.yaml says why — but the
// tree is kept compiling everywhere, and CI's cross-build step is cheap
// precisely because nothing platform-specific creeps in unguarded. This file
// is what keeps that true now that delegation starts a process.
//
// The fallback is a real one rather than a stub: there is no process group to
// set, and killing the process itself is the closest equivalent available. It
// leaves the agent's own children running, which is exactly the shortcoming
// the Unix version exists to avoid — another reason those are the supported
// platforms.

func setProcessGroup(*exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
