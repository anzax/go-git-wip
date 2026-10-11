//go:build linux || darwin

package wip

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureGitProcess(cmd *exec.Cmd) {
	// A zero Pgid creates a new group whose ID is the child's PID, keeping the
	// caller and other library commands outside the cancellation signal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Do not signal a stale group after Wait has reaped its leader. Cancel is
		// normally called only by CommandContext, while the command is running.
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			return err
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			// Match exec's cancellation contract when exit wins the race.
			return os.ErrProcessDone
		}
		return err
	}
}
