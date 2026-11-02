//go:build windows

package localexec

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// HardenProcess rejects an already-elevated CLI process. File tools execute in
// the CLI process itself, so allowing an elevated parent would bypass the
// restricted child token used for shell commands.
func HardenProcess() error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("open current process token: %w", err)
	}
	defer token.Close()
	return requireUnelevatedToken(token.IsElevated())
}

func requireUnelevatedToken(elevated bool) error {
	if elevated { return fmt.Errorf("eiksy-cli must be started without Run as administrator; elevated parent processes are not supported") }
	return nil
}
