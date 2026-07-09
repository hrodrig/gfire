// Command gfire is the GFire background job service binary.
package main

import (
	"fmt"
	"os"
	"runtime"
)

// Set via -ldflags at build time (see Dockerfile / Makefile).
var (
	version   = "dev"
	commit    = "unknown"
	branch    = "unknown"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: gfire <command>\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version", "--version", "-V":
		fmt.Printf("gfire %s\n", version)
		fmt.Printf("  commit:     %s\n", commit)
		fmt.Printf("  branch:     %s\n", branch)
		fmt.Printf("  built:      %s\n", buildDate)
		fmt.Printf("  go:         %s\n", runtime.Version())
		return
	}

	fmt.Fprintf(os.Stderr, "gfire: %q: not implemented\n", os.Args[1])
	os.Exit(1)
}
