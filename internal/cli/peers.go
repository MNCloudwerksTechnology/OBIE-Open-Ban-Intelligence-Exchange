package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

func runPeers(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl peers"
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the peers as JSON, for scripts")
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	peers, err := client.Peers(ctx)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, peers)
	} else {
		err = writePeersTable(stdout, peers.Peers)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing peers: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

// writePeersTable prints one row per peer, in the order the daemon sends
// them (sorted by peer ID).
func writePeersTable(w io.Writer, peers []admin.PeerResponse) error {
	if len(peers) == 0 {
		_, err := fmt.Fprintln(w, "No peers connected.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "PEER ID\tNAME\tTRUST\tBOOTSTRAP\tCONNECTED SINCE\tLATENCY\tADDRESSES\n")
	for _, p := range peers {
		latency := "-"
		if d := p.Latency(); d > 0 {
			latency = d.Round(100 * time.Microsecond).String()
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.PeerID, orDash(p.Name),
			strconv.FormatFloat(p.TrustWeight, 'g', -1, 64), yesNo(p.Bootstrap),
			p.ConnectedSince.UTC().Format(time.RFC3339), latency, orDash(strings.Join(p.Addresses, ",")))
	}
	return tw.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
