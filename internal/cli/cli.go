package cli

import (
	"fmt"
	"io"
)

const usage = "Eiksy — Think. Connect. Operate.\n\nUsage:\n  eiksy [command]\n\nCommands:\n  version   Print the Eiksy version\n\nOptions:\n  -h, --help       Show this help\n  -v, --version    Print the Eiksy version\n\nRunning the standalone eiksy-cli binary without arguments starts the local-agent terminal UI. Running the desktop eiksy binary without arguments starts the desktop application.\n"

// Run handles CLI-only arguments.
// It returns handled=true when the process should exit instead of starting the desktop application.
func Run(args []string, version string, stdout, stderr io.Writer) (handled bool, exitCode int) {
	if len(args) == 0 {
		return false, 0
	}

	switch args[0] {
	case "-h", "--help", "help":
		_, _ = io.WriteString(stdout, usage)
		return true, 0
	case "-v", "--version", "version":
		_, _ = fmt.Fprintln(stdout, version)
		return true, 0
	default:
		_, _ = fmt.Fprintf(stderr, "eiksy: unknown command or argument %q\n\n%s", args[0], usage)
		return true, 2
	}
}
