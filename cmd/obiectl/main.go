// Command obiectl is the OBIE operator CLI; it talks to obied over the local admin API.
package main

import (
	"os"

	"github.com/MNCloudwerksTechnology/obie/internal/cli"
)

func main() {
	os.Exit(cli.RunCtl(os.Args[1:], os.Stdout, os.Stderr))
}
