// Command gfire is the GFire background job service binary.
package main

import (
	"github.com/hrodrig/gfire/internal/cli"
)

// Set via -ldflags at build time (see Dockerfile / Makefile).
var (
	version   = "dev"
	commit    = "unknown"
	branch    = "unknown"
	buildDate = "unknown"
)

func main() {
	cli.Execute(version)
}
