//go:build linux

package localexec

import (
	"os/exec"

	"golang.org/x/sys/unix"
)

// HardenProcess prevents this process and all descendants from gaining
// privileges through setuid/setgid binaries or file capabilities. Linux does
// not permit clearing this flag, so it is applied only by the standalone CLI.
func HardenProcess() error {
	return unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
}

func configureCommand(_ *exec.Cmd) (func(), error) { return nil, nil }
