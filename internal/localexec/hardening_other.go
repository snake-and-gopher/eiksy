//go:build !linux && !windows

package localexec

import (
	"fmt"
	"os/exec"
)

// HardenProcess fails closed on platforms without a supported no-elevation
// primitive. The local agent must not silently run unrestricted commands.
func HardenProcess() error {
	return fmt.Errorf("local command execution is not supported on this platform because privilege restrictions are unavailable")
}

func configureCommand(_ *exec.Cmd) (func(), error) { return nil, nil }
