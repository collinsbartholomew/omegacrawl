//go:build !linux
// +build !linux

package browserpool

import "os/exec"

// modifyChromeCmd is a no-op on platforms without process groups.
func modifyChromeCmd(cmd *exec.Cmd) {}

// killProcessTree is a no-op on platforms without process groups.
func killProcessTree(pid int) {}
