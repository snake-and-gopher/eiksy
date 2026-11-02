//go:build linux

package localexec

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHardenProcessSetsNoNewPrivileges(t *testing.T) {
	const marker = "EIKSY_TEST_HARDEN_PROCESS"
	if os.Getenv(marker) == "1" {
		if err := HardenProcess(); err != nil {
			t.Fatalf("HardenProcess() error = %v", err)
		}
		status, err := os.ReadFile("/proc/self/status")
		if err != nil {
			t.Fatalf("read process status: %v", err)
		}
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "NoNewPrivs:") {
				if strings.TrimSpace(strings.TrimPrefix(line, "NoNewPrivs:")) != "1" {
					t.Fatalf("NoNewPrivs status = %q, want 1", line)
				}
				return
			}
		}
		t.Fatal("NoNewPrivs field not found in /proc/self/status")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestHardenProcessSetsNoNewPrivileges$")
	cmd.Env = append(os.Environ(), marker+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hardened subprocess failed: %v\n%s", err, output)
	}
}
