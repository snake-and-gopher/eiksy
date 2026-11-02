//go:build windows

package localexec

// The CLI process itself retains its normal token. Each local command receives
// a restricted primary token with administrative membership disabled and
// privileges removed, preventing child processes from requesting elevation.
func HardenProcess() error { return nil }
