package simtrust

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ReportFormat is the version of the report's files (ADR 0034).
const ReportFormat = 1

// Report file names.
const (
	reportFile        = "README.md"
	summaryFile       = "summary.csv"
	hourlyFile        = "hourly.csv.gz"
	feedsFile         = "feeds.csv"
	corroborationFile = "corroboration.csv"
)

// ReportInfo is what the report's header states besides the results.
type ReportInfo struct {
	// Version names the OBIE build, e.g. from git describe.
	Version   string
	Generated time.Time
}

// WriteReport writes the report of rep into dir: the Markdown report and
// its CSV files.
func WriteReport(dir string, rep *Report, info ReportInfo) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	files := []struct {
		name  string
		write func(io.Writer, *Report) error
	}{
		{summaryFile, writeSummary},
		{feedsFile, writeFeeds},
		{corroborationFile, writeCorroboration},
		{hourlyFile, writeHourly},
		{reportFile, func(w io.Writer, rep *Report) error { return writeMarkdown(w, rep, info) }},
	}
	for _, f := range files {
		if err := writeFileWith(filepath.Join(dir, f.name), rep, f.write); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return nil
}

// csvFloat formats x for a CSV file; NaN is empty.
func csvFloat(x float64) string {
	if math.IsNaN(x) {
		return ""
	}
	return strconv.FormatFloat(x, 'g', 6, 64)
}

// estimateFields are the CSV fields of an estimate.
func estimateFields(e Estimate) []string {
	return []string{strconv.Itoa(e.N), csvFloat(e.Mean), csvFloat(e.Low), csvFloat(e.High)}
}

func configFields(c Config) []string {
	return []string{string(c.Model), strconv.FormatFloat(c.Fraction, 'f', 2, 64), string(c.Profile)}
}

func writeCSV(w io.Writer, header []string, rows func(add func(...[]string) error) error) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		return err
	}
	if err := rows(func(parts ...[]string) error { return cw.Write(slices.Concat(parts...)) }); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

// writeSummary writes every metric of every configuration at the end of
// the run, cumulatively.
func writeSummary(w io.Writer, rep *Report) error {
	return writeCSV(w, []string{"model", "fraction", "profile", "metric", "n", "mean", "ci_low", "ci_high"}, func(add func(...[]string) error) error {
		for _, a := range rep.Aggregates {
			end := a.End()
			for i := range numMetrics {
				if err := add(configFields(a.Config), []string{Metric(i).String()}, estimateFields(end[i])); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// writeHourly writes every metric of every configuration in both windows
// of every hour, gzipped; a metric defined in no seed is left out.
func writeHourly(w io.Writer, rep *Report) error {
	gz := gzip.NewWriter(w) // no name, no time: the same report gives the same file
	err := writeCSV(gz, []string{"model", "fraction", "profile", "hour", "window", "metric", "n", "mean", "ci_low", "ci_high"}, func(add func(...[]string) error) error {
		for _, a := range rep.Aggregates {
			for h := range a.Hours {
				for win := range numWindows {
					for i := range numMetrics {
						e := a.Hours[h][win][i]
						if e.N == 0 {
							continue
						}
						if err := add(configFields(a.Config), []string{strconv.Itoa(h), Window(win).String(), Metric(i).String()}, estimateFields(e)); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return gz.Close()
}

// feedFields are the feed metrics by name.
func feedFields(f FeedEstimate) []struct {
	name string
	e    Estimate
} {
	return []struct {
		name string
		e    Estimate
	}{{"volume", f.Volume}, {"exclusive", f.Exclusive}, {"latency_minutes", f.LatencyMinutes}, {"bound", f.Bound}, {"accuracy", f.Accuracy}}
}

// sortedRoles returns the roles of feeds in report order.
func sortedRoles(feeds map[Role]FeedEstimate) []Role {
	order := []Role{RoleObserver, RoleHonest, RoleNewcomer}
	var roles []Role
	for r := range feeds {
		roles = append(roles, r)
	}
	slices.SortFunc(roles, func(a, b Role) int {
		ia, ib := slices.Index(order, a), slices.Index(order, b)
		if ia < 0 {
			ia = len(order)
		}
		if ib < 0 {
			ib = len(order)
		}
		if ia != ib {
			return ia - ib
		}
		return strings.Compare(string(a), string(b))
	})
	return roles
}

// writeFeeds writes the feed metrics of every role of every configuration.
func writeFeeds(w io.Writer, rep *Report) error {
	return writeCSV(w, []string{"model", "fraction", "profile", "role", "metric", "n", "mean", "ci_low", "ci_high"}, func(add func(...[]string) error) error {
		for _, a := range rep.Aggregates {
			for _, r := range sortedRoles(a.Feeds) {
				for _, f := range feedFields(a.Feeds[r]) {
					if err := add(configFields(a.Config), []string{string(r), f.name}, estimateFields(f.e)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// supportLabel names a support bucket.
func supportLabel(b int) string {
	if b == supportBuckets-1 {
		return strconv.Itoa(b) + "+"
	}
	return strconv.Itoa(b)
}

// writeCorroboration writes the attackers by support and whether they
// were banned.
func writeCorroboration(w io.Writer, rep *Report) error {
	header := []string{"model", "fraction", "profile", "support", "local", "attackers_total", "banned_total",
		"attackers_per_seed", "banned_share_n", "banned_share_mean", "banned_share_ci_low", "banned_share_ci_high"}
	return writeCSV(w, header, func(add func(...[]string) error) error {
		for _, a := range rep.Aggregates {
			for b := range supportBuckets {
				for l, local := range []string{"no", "yes"} {
					c := a.Corroboration[b][l]
					if err := add(configFields(a.Config), []string{supportLabel(b), local, strconv.Itoa(c.TotalAttackers),
						strconv.Itoa(c.Total), csvFloat(c.Attackers.Mean)}, estimateFields(c.Share)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
