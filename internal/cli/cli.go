// Package cli provides the command-line handling shared by the OBIE binaries.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/daemon"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
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

// RunDaemon runs obied with args and returns the process exit code. The
// first argument names the command; without one it prints the overview of
// the commands. "run", or flags such as --config in place of a command, run
// the node: it loads and validates the configuration file and the
// allow-list files, and with --check-config it stops there. Otherwise it
// runs the node until SIGTERM or SIGINT and then shuts down gracefully; a
// second signal terminates the process immediately. SIGHUP reloads the
// configuration.
func RunDaemon(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // Restore default signal handling for a second signal.
	}()
	return runDaemonWith(ctx, reloadSignals(ctx), args, stdout, stderr)
}

// reloadSignals delivers SIGHUP until ctx is done; signals arriving while
// a reload is pending are coalesced.
func reloadSignals(ctx context.Context) <-chan struct{} {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	reload := make(chan struct{}, 1)
	go func() {
		defer signal.Stop(hup)
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				select {
				case reload <- struct{}{}:
				default: // a reload is already pending
				}
			}
		}
	}()
	return reload
}

// runDaemon is RunDaemon with the shutdown signal delivered as ctx and
// without reloads.
func runDaemon(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runDaemonWith(ctx, nil, args, stdout, stderr)
}

// daemonName is the name of the node daemon.
const daemonName = "obied"

// runDaemonWith is RunDaemon with the shutdown signal delivered as ctx and
// the reload signal as reload.
func runDaemonWith(ctx context.Context, reload <-chan struct{}, args []string, stdout, stderr io.Writer) int {
	t := daemonTool()
	switch {
	case len(args) == 0:
		t.overview(stderr)
		return ExitUsage
	case isHelpFlag(args[0]):
		return t.runHelp(nil, stdout, stderr)
	case args[0] == "help":
		return t.runHelp(args[1:], stdout, stderr)
	case strings.HasPrefix(args[0], "-"):
		// obied --config <file>: how the systemd unit and the container
		// image run the node.
		return runNode(ctx, reload, daemonName, args, stdout, stderr)
	}
	for _, cmd := range daemonCommands() {
		if cmd.name == args[0] {
			return cmd.run(ctx, reload, args[1:], stdout, stderr)
		}
	}
	t.unknownCommand(stderr, args[0])
	return ExitUsage
}

// isHelpFlag reports whether arg asks for help.
func isHelpFlag(arg string) bool {
	switch arg {
	case "-h", "--h", "-help", "--help":
		return true
	}
	return false
}

// runNode loads and validates the configuration file and the allow-list
// files; with --check-config it stops there. Otherwise it runs the node
// until ctx is done, reloading the configuration on every value of reload.
func runNode(ctx context.Context, reload <-chan struct{}, program string, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet(program)
	configPath := fs.String("config", config.DefaultPath, "path to the YAML configuration `file`")
	checkOnly := fs.Bool("check-config", false, "only check the configuration: exit 0 if it is valid, 1 if not")
	if code, done := parse(fs, args, stdout, stderr); done {
		return code
	}
	if fs.NArg() > 0 {
		usageProblem(program, fmt.Sprintf("unexpected argument %q", fs.Arg(0))).write(stderr, program)
		return ExitUsage
	}

	file, err := config.LoadFile(*configPath)
	if err != nil {
		configProblem(*configPath, err).write(stderr, program)
		return ExitInvalidConfig
	}
	if _, err := sovereignty.ReadFiles(file.Config.Allowlist.Files); err != nil {
		allowlistProblem(*configPath, err).write(stderr, program)
		return ExitInvalidConfig
	}
	if *checkOnly {
		if _, err := fmt.Fprintf(stdout, "%s: configuration %s is valid\n", program, *configPath); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: writing result: %v\n", program, err)
			return ExitIOError
		}
		return ExitOK
	}

	cfg := file.Config
	// Validation guarantees a known level.
	level, _ := logging.ParseLevel(cfg.Log.Level)
	logs := logging.New(stderr, level)
	log := logs.Logger(daemon.Component)
	log.Info("configuration loaded", "path", *configPath, "mode", cfg.Node.Mode)
	opts := daemon.Options{Reload: reload, File: file, LoadConfig: func() (*config.File, error) { return config.LoadFile(*configPath) }}
	if err := daemon.Run(ctx, cfg, logs, opts); err != nil {
		log.Error("obied failed", "error", err, "next", startNext(err))
		return ExitFailure
	}
	return ExitOK
}

// parse adds --version to fs and parses args up to the first argument that
// is not a flag. It returns done when the program must exit with code:
// after --help, a usage error or --version.
func parse(fs *flag.FlagSet, args []string, stdout, stderr io.Writer) (code int, done bool) {
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return flagError(fs, err, stdout, stderr), true
	}
	if *showVersion {
		if _, err := fmt.Fprintln(stdout, version.String(fs.Name())); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: writing version: %v\n", fs.Name(), err)
			return ExitIOError, true
		}
		return ExitOK, true
	}
	return 0, false
}
