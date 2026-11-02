//go:build windows

package localexec

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createRestrictedTokenDisableMaxPrivilege = 0x1

var advapi32 = syscall.NewLazyDLL("advapi32.dll")
var procCreateRestrictedToken = advapi32.NewProc("CreateRestrictedToken")

func configureCommand(cmd *exec.Cmd) (func(), error) {
	token, err := newRestrictedToken()
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(token)}
	return func() { _ = token.Close() }, nil
}

func newRestrictedToken() (windows.Token, error) {
	current, err := windows.OpenProcessToken(
		windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY,
	)
	if err != nil {
		return 0, fmt.Errorf("open current process token: %w", err)
	}
	defer current.Close()

	adminSID, err := windows.StringToSid("S-1-5-32-544")
	if err != nil {
		return 0, fmt.Errorf("resolve built-in Administrators SID: %w", err)
	}
	disabledGroup := windows.SIDAndAttributes{
		Sid:        adminSID,
		Attributes: windows.SE_GROUP_USE_FOR_DENY_ONLY,
	}

	var restricted windows.Token
	result, _, callErr := procCreateRestrictedToken.Call(
		uintptr(current),
		createRestrictedTokenDisableMaxPrivilege,
		1,
		uintptr(unsafe.Pointer(&disabledGroup)),
		0, 0, 0, 0,
		uintptr(unsafe.Pointer(&restricted)),
	)
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return 0, fmt.Errorf("create restricted process token: %w", callErr)
		}
		return 0, fmt.Errorf("create restricted process token failed")
	}
	return restricted, nil
}
