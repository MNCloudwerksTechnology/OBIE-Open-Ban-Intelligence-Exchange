// Package cli provides the command-line handling shared by the OBIE binaries.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/daemon"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

// Exit codes returned by RunCtl and RunDaemon. Diagnostics written to stderr are
// best effort: a failing stderr cannot be reported anywhere else.
const (
	ExitOK            = 0
	ExitInvalidConfig = 1
	ExitFailure       = 1
	ExitUsage         = 2
	ExitIOError       = 3
)

// RunDaemon runs obied with args and returns the process exit code. It loads
// and validates the configuration file; with --check-config it stops there.
// Otherwise it runs the node until SIGTERM or SIGINT and then shuts down
// gracefully; a second signal terminates the process immediately.
func RunDaemon(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // Restore default signal handling for a second signal.
	}()
	return runDaemon(ctx, args, stdout, stderr)
}

// runDaemon is RunDaemon with the shutdown signal delivered as ctx.
func runDaemon(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	const program = "obied"
	fs := newFlagSet(program, stderr)
	configPath := fs.String("config", config.DefaultPath, "path to the YAML configuration `file`")
	checkOnly := fs.Bool("check-config", false, "validate the configuration and exit 0 (valid) or 1 (invalid)")
	if code, done := parse(fs, program, args, false, stdout, stderr); done {
		return code
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %s: %v\n", program, *configPath, err)
		return ExitInvalidConfig
	}
	if *checkOnly {
		if _, err := fmt.Fprintf(stdout, "%s: configuration %s is valid\n", program, *configPath); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: writing result: %v\n", program, err)
			return ExitIOError
		}
		return ExitOK
	}

	// Validation guarantees a known level.
	level, _ := logging.ParseLevel(cfg.Log.Level)
	logs := logging.New(stderr, level)
	log := logs.Logger(daemon.Component)
	log.Info("configuration loaded", "path", *configPath, "mode", cfg.Node.Mode)
	if err := daemon.Run(ctx, cfg, logs); err != nil {
		log.Error("obied failed", "error", err)
		return ExitFailure
	}
	return ExitOK
}

func newFlagSet(program string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(program, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parse adds --version to fs and parses args; positional arguments are a
// usage error unless allowArgs. It returns done when the program must exit
// with code: on --help, usage errors and --version.
func parse(fs *flag.FlagSet, program string, args []string, allowArgs bool, stdout, stderr io.Writer) (code int, done bool) {
	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK, true
		}
		return ExitUsage, true
	}
	if fs.NArg() > 0 && !allowArgs {
		_, _ = fmt.Fprintf(stderr, "%s: unexpected argument %q\n", program, fs.Arg(0))
		fs.Usage()
		return ExitUsage, true
	}
	if *showVersion {
		if _, err := fmt.Fprintln(stdout, version.String(program)); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: writing version: %v\n", program, err)
			return ExitIOError, true
		}
		return ExitOK, true
	}
	return 0, false
}
