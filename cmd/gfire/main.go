package main

import (
	"github.com/hrodrig/gfire/internal/cli"
	"github.com/hrodrig/gfire/internal/version"
)

func main() {
	cli.Execute(version.Version)
}
