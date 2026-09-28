package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// daemonCommand is an offline obied subcommand; it never talks to a running
// daemon.
type daemonCommand struct {
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

var daemonCommands = map[string]daemonCommand{
	"keygen":            {summary: "create the node identity key", run: runKeygen},
	"identity":          {summary: "show the node identity from its key file", run: runIdentity},
	"teardown-firewall": {summary: "remove the nftables table inet obie with every block", run: runTeardownFirewall},
}

// stateDirFlags registers the flags that locate the state directory.
func stateDirFlags(fs *flag.FlagSet) (configPath, stateDir *string) {
	configPath = fs.String("config", config.DefaultPath, "path to the YAML configuration `file` naming node.state_dir")
	stateDir = fs.String("state-dir", "", "state `directory` holding "+identity.FileName+" (overrides --config)")
	return configPath, stateDir
}

// resolveStateDir returns stateDir if set, else node.state_dir of the
// configuration file.
func resolveStateDir(configPath, stateDir string) (string, error) {
	if stateDir != "" {
		return stateDir, nil
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", fmt.Errorf("%s: %w", configPath, err)
	}
	return cfg.Node.StateDir, nil
}

// parseCommand parses the flags of a subcommand, which takes no positional
// arguments. It returns done when the program must exit with code.
func parseCommand(fs *flag.FlagSet, program string, args []string, stderr io.Writer) (code int, done bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK, true
		}
		return ExitUsage, true
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "%s: unexpected argument %q\n", program, fs.Arg(0))
		fs.Usage()
		return ExitUsage, true
	}
	return 0, false
}

func runKeygen(args []string, stdout, stderr io.Writer) int {
	const program = "obied keygen"
	fs := newFlagSet(program, stderr)
	configPath, stateDirFlag := stateDirFlags(fs)
	force := fs.Bool("force", false, "replace an existing key; this changes the node's peer ID")
	if code, done := parseCommand(fs, program, args, stderr); done {
		return code
	}
	stateDir, err := resolveStateDir(*configPath, *stateDirFlag)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
		return ExitInvalidConfig
	}

	key, err := identity.Create(stateDir, *force)
	if errors.Is(err, identity.ErrKeyExists) {
		_, _ = fmt.Fprintf(stderr, "%s: %s already exists; pass --force to replace it (this changes the node's peer ID)\n",
			program, identity.Path(stateDir))
		return ExitFailure
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
		return ExitFailure
	}
	_, _ = fmt.Fprintf(stderr, "%s: wrote a new node key to %s\n", program, identity.Path(stateDir))
	if *force {
		_, _ = fmt.Fprintf(stderr, "%s: a running obied keeps its old key until it is restarted\n", program)
	}
	return printIdentity(stdout, stderr, program, admin.NewIdentityResponse(key), false)
}

func runIdentity(args []string, stdout, stderr io.Writer) int {
	const program = "obied identity"
	fs := newFlagSet(program, stderr)
	configPath, stateDirFlag := stateDirFlags(fs)
	asJSON := fs.Bool("json", false, "print the identity as JSON")
	if code, done := parseCommand(fs, program, args, stderr); done {
		return code
	}
	stateDir, err := resolveStateDir(*configPath, *stateDirFlag)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
		return ExitInvalidConfig
	}

	key, err := identity.Load(stateDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(stderr, "%s: create the key with obied keygen, or start obied once\n", program)
		}
		return ExitFailure
	}
	return printIdentity(stdout, stderr, program, admin.NewIdentityResponse(key), *asJSON)
}

func runCtlIdentity(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl identity"
	fs := newFlagSet(program, stderr)
	asJSON := fs.Bool("json", false, "print the identity as JSON")
	if code, done := parseCommand(fs, program, args, stderr); done {
		return code
	}
	id, err := client.Identity(ctx)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	return printIdentity(stdout, stderr, "obiectl", *id, *asJSON)
}

// printIdentity prints the peer ID and fingerprint — never the private key.
func printIdentity(stdout, stderr io.Writer, program string, id admin.IdentityResponse, asJSON bool) int {
	var err error
	if asJSON {
		err = writeJSON(stdout, id)
	} else {
		err = writeIdentityTable(stdout, id)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: writing identity: %v\n", program, err)
		return ExitIOError
	}
	return ExitOK
}

func writeIdentityTable(w io.Writer, id admin.IdentityResponse) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Peer ID:\t%s\n", id.PeerID)
	_, _ = fmt.Fprintf(tw, "Fingerprint:\t%s\n", id.Fingerprint)
	return tw.Flush()
}
