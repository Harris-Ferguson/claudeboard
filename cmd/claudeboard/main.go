// Command claudeboard is a terminal control panel for Claude Code background sessions.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3". Builds from a
// git checkout without it report the commit instead.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches subcommands and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Fprintln(stdout, "claudeboard", buildVersion())
			return 0
		}
	}
	fmt.Fprintln(stderr, "claudeboard: the board isn't built yet")
	return 1
}

// buildVersion returns version, or "dev (<commit>)" when built from a git checkout.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return "dev (" + s.Value[:7] + ")"
		}
	}
	return version
}
