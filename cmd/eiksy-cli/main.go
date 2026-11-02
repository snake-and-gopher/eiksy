package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"eiksy/internal/app"
	"eiksy/internal/cli"
	"eiksy/internal/localtui"
	"eiksy/internal/localexec"
	"eiksy/internal/storage/disk"
)

var releaseVersion = "dev"

func resolveReleaseVersion() string {
	version := strings.TrimSpace(releaseVersion)
	if version != "" && version != "dev" {
		return version
	}

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		if buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
			return buildInfo.Main.Version
		}
		for _, setting := range buildInfo.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				shortRevision := setting.Value
				if len(shortRevision) > 7 {
					shortRevision = shortRevision[:7]
				}
				return "dev-" + shortRevision
			}
		}
	}

	return version
}

func newBackend() (*app.Service, error) {
	if err := localexec.HardenProcess(); err != nil {
		return nil, fmt.Errorf("harden local execution: %w", err)
	}
	store, err := disk.NewStore()
	if err != nil {
		return nil, fmt.Errorf("initialize storage: %w", err)
	}
	service, err := app.NewLocalAgentService(store)
	if err != nil {
		return nil, fmt.Errorf("initialize local agent: %w", err)
	}
	return service, nil
}

func main() {
	if len(os.Args) == 1 {
		service, err := newBackend()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "eiksy-cli: initialize: %v\n", err)
			os.Exit(1)
		}
		agent := app.NewLocalAIBackendAgent(service)
		if err := localtui.Run(service, agent); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "eiksy-cli: tui: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if handled, exitCode := cli.Run(os.Args[1:], resolveReleaseVersion(), os.Stdout, os.Stderr); handled {
		os.Exit(exitCode)
	}

	_, _ = fmt.Fprintln(os.Stderr, "eiksy-cli: unexpected command state")
	os.Exit(2)
}
