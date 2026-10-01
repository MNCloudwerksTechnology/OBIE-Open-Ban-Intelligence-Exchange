// Command obied is the OBIE node daemon.
package main

import (
	"os"

	"github.com/MNCloudwerksTechnology/obie/internal/cli"
)

func main() {
	os.Exit(cli.RunDaemon(os.Args[1:], os.Stdout, os.Stderr))
}
