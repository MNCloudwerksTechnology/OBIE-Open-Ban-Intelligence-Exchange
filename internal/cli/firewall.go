package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
)

// teardownFirewall removes the nftables table inet obie; tests replace it.
var teardownFirewall = func(ctx context.Context) error {
	return nft.New(nft.Options{}, slog.New(slog.DiscardHandler)).Teardown(ctx)
}

// runTeardownFirewall removes the table inet obie with every block. With
// --on-stop, as the systemd unit's ExecStopPost, it only does so when the
// configuration uses the nftables backend with
// enforce.nftables.teardown_on_stop; otherwise the blocks stay until
// their timeout.
func runTeardownFirewall(args []string, stdout, stderr io.Writer) int {
	const program = "obied teardown-firewall"
	fs := newFlagSet(program)
	configPath := fs.String("config", config.DefaultPath, "path to the YAML configuration `file`, read with --on-stop")
	onStop := fs.Bool("on-stop", false, "only remove the table if enforce.nftables.teardown_on_stop is true (for ExecStopPost)")
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	if *onStop {
		cfg, err := config.Load(*configPath)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: %s: %v\n", program, *configPath, err)
			return ExitInvalidConfig
		}
		if cfg.Enforce.Backend != config.BackendNFTables || !cfg.Enforce.NFTables.TeardownOnStop {
			_, _ = fmt.Fprintf(stderr, "%s: keeping table inet %s: enforce.nftables.teardown_on_stop is not set\n", program, nft.Table)
			return ExitOK
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), enforce.PassTimeout)
	defer cancel()
	if err := teardownFirewall(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
		return ExitFailure
	}
	_, _ = fmt.Fprintf(stderr, "%s: table inet %s removed; nothing is blocked by OBIE anymore\n", program, nft.Table)
	return ExitOK
}
