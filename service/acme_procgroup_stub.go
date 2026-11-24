//go:build windows

package service

import "os/exec"

// killGroupOnCancel is a no-op on Windows, where acme.sh does not run: every
// acme entry point returns before it would start one. runCmd's WaitDelay still
// bounds the wait for a command whose children outlive it.
func killGroupOnCancel(*exec.Cmd) {}
