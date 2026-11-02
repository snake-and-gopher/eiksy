//go:build !linux && !windows

package localexec

import "os/exec"

// HardenProcess is a no-op on platforms without a supported no-elevation
// primitive. Local execution remains subject to the launching user's token.
func HardenProcess() error { return nil }

func configureCommand(_ *exec.Cmd) (func(), error) { return nil, nil }
