// Command trace-import turns the Fail2Ban logs of cooperating operators
// into a pseudonymized trace for the trust simulation (ADR 0034):
//
//	go run ./test/simtrust/cmd/trace-import -key KEY -benign RANGES -o TRACE NAME:BANTIME:LOG ...
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/MNCloudwerksTechnology/obie/test/simtrust"
)

func main() {
	if err := simtrust.RunImport(os.Args[1:], os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "trace-import:", err)
		}
		os.Exit(2)
	}
}
