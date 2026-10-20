// Command bamboohr is a command line client for the BambooHR API.
package main

import (
	"os"

	"github.com/gdesouza/gobamboohr/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Execute(version, os.Args[1:], os.Stdout, os.Stderr))
}
