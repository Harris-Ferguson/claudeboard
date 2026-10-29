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

const usage = `usage: claudeboard [command]

  claudeboard              open the board
  claudeboard new …        start a session (claudeboard new -h for options)
  claudeboard --version    print the version
`

// run dispatches subcommands and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runBoard(stderr)
	}
	switch args[0] {
	case "--version", "-v", "version":
		fmt.Fprintln(stdout, "claudeboard", buildVersion())
		return 0
	case "new":
		return runNew(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		if len(args) > 1 && args[1] == "new" {
			fmt.Fprint(stdout, newUsage)
		} else {
			fmt.Fprint(stdout, usage)
		}
		return 0
	}
	fmt.Fprintf(stderr, "claudeboard: unknown command %q\n\n%s", args[0], usage)
	return 2
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
