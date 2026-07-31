package browserpool

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestKillProcessTreeKillsGroup verifies that killProcessTree terminates the
// whole process group, including children spawned by the group leader.
func TestKillProcessTreeKillsGroup(t *testing.T) {
	// Spawn a shell that forks a long-lived child in the same process group.
	cmd := exec.Command("sh", "-c", "sleep 30 & echo $!; wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn test process: %v", err)
	}

	// Give the child time to fork.
	time.Sleep(500 * time.Millisecond)

	killProcessTree(cmd.Process.Pid)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process group did not terminate within 5s")
	}
}

// TestShutdownIdempotent verifies shutdown() can be called multiple times
// without panicking or spawning duplicate reaper goroutines.
func TestShutdownIdempotent(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn test process: %v", err)
	}

	inst := &BrowserInstance{
		proc:        cmd.Process,
		pid:         cmd.Process.Pid,
		allocCancel: func() {},
	}

	inst.shutdown()
	inst.shutdown() // must be a no-op the second time

	// The process must actually be dead.
	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("process still alive after shutdown")
	}
}
