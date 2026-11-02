//go:build !linux && !windows

package localexec

import "os/exec"
	"fmt"

// HardenProcess is a no-op on platforms without a supported no-elevation
// primitive. Local execution remains subject to the launching user's token.
func HardenProcess() error { return fmt.Errorf("local command execution is not supported on this platform because privilege restrictions are unavailable") }

func configureCommand(_ *exec.Cmd) (func(), error) { return nil, nil }
