//go:build !unix

package claude

import "os/exec"

func prepareProcess(cmd *exec.Cmd)            {}
func signalProcess(cmd *exec.Cmd, force bool) { _ = cmd.Process.Kill() }
