//go:build !windows

package service

import (
	"os/exec"
	"syscall"
)

// killGroupOnCancel puts the command in a process group of its own and makes
// the context's cancellation kill that whole group, not just the process exec
// started.
//
// acme.sh is a shell script. What it runs -- socat listening on :80 for a
// standalone challenge, curl talking to the CA or to GitHub -- are its
// children, and the default Cancel kills only the shell. Those children
// survive it: socat keeps :80 bound (every later standalone issuance then
// finds the port taken), and they hold the shell's stdout, so CombinedOutput
// waits for an EOF that never comes and the timeout bounds nothing. A run
// stuck that way would hold acmeIssuing for the life of the process.
//
// Killing them is not the same as reaping them. Once sh is gone they belong to
// PID 1, which under Docker is sui itself unless the container runs an init
// (docker-compose.yml sets init: true, the READMEs pass --init); sui waits only
// on children it started, so without one each killed run leaves zombies. They
// hold no port and no pipe, which is what matters here.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Setpgid made the child its group's leader, so its pid is the pgid.
		// A negative pid signals the whole group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
