//go:build windows

package localexec

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsLocalAgentRejectsElevatedParentAndCreatesRestrictedToken(t *testing.T) {
	if err := requireUnelevatedToken(true); err == nil { t.Fatal("expected elevated parent token to be rejected") }
	if err := requireUnelevatedToken(false); err != nil { t.Fatalf("unelevated parent token rejected: %v", err) }

	current, err := windows.OpenCurrentProcessToken()
	if err != nil { t.Fatalf("OpenCurrentProcessToken() error = %v", err) }
	elevated := current.IsElevated()
	_ = current.Close()
	if err := HardenProcess(); elevated && err == nil { t.Fatal("HardenProcess() accepted an elevated parent token") } else if !elevated && err != nil { t.Fatalf("HardenProcess() rejected an unelevated parent token: %v", err) }

	token, err := newRestrictedToken()
	if err != nil {
		t.Fatalf("newRestrictedToken() error = %v", err)
	}
	defer token.Close()

	groups, err := token.GetTokenGroups()
	if err != nil {
		t.Fatalf("GetTokenGroups() error = %v", err)
	}
	for _, group := range groups.AllGroups() {
		if group.Sid.String() != "S-1-5-32-544" {
			continue
		}
		if group.Attributes&windows.SE_GROUP_USE_FOR_DENY_ONLY == 0 {
			t.Fatal("Administrators group is not marked deny-only in restricted token")
		}
		return
	}
	// Non-administrator accounts do not carry the Administrators SID. The
	// restricted token still has no added privileges for those accounts.
}
