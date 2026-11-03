package gitrepo

import (
	"os/exec"
	"syscall"
	"time"
)

// confine puts the child in its own process group and kills the whole group when the context ends, so an ssh
// started by git does not outlive the call.
func confine(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
}
