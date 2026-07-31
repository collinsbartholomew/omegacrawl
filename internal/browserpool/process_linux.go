//go:build linux
// +build linux

package browserpool

import (
	"os/exec"
	"syscall"
)

// modifyChromeCmd runs Chrome in its own process group (so the entire tree of
// renderer/GPU/network children can be signalled together) and preserves the
// default SIGKILL-on-parent-death behaviour that chromedp would otherwise set.
func modifyChromeCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// killProcessTree SIGKILLs the whole Chrome process group led by pid, so no
// orphaned child processes are left behind to become zombies.
func killProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
