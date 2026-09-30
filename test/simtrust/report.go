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
	publishersFile    = "publishers.csv.gz"
	corroborationFile = "corroboration.csv"
)

// ReportInfo is what the report's header states besides the results.
type ReportInfo struct {
	// Version names the OBIE build, e.g. from git describe.
	Version   string
	Generated time.Time
	// ADR is the path of ADR 0034 relative to the report's directory, for
	// a link; the report names the ADR without a link if it is empty.
	ADR string
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
		{publishersFile, writePublishers},
		{reportFile, func(w io.Writer, rep *Report) error { return writeMarkdown(w, rep, info) }},
	}
	for _, f := range files {
		if err := writeFileWith(filepath.Join(dir, f.name), rep, f.write); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return nil
}

// csvDigits are the significant digits of a value in a CSV file: more
// than the intervals over 20 seeds resolve, and few enough to keep the
// committed baseline small.
const csvDigits = 4

// csvFloat formats x for a CSV file, rounded to csvDigits significant
// digits and without an exponent; NaN is empty.
func csvFloat(x float64) string {
	if math.IsNaN(x) {
		return ""
	}
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'g', csvDigits, 64), 64) // a formatted float always parses
	return strconv.FormatFloat(rounded, 'f', -1, 64)
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
// the run, cumulatively, and the false bans by the class of their victim.
func writeSummary(w io.Writer, rep *Report) error {
	return writeCSV(w, []string{"model", "fraction", "profile", "metric", "n", "mean", "ci_low", "ci_high"}, func(add func(...[]string) error) error {
		for _, a := range rep.Aggregates {
			end := a.End()
			for i := range numMetrics {
				if err := add(configFields(a.Config), []string{Metric(i).String()}, estimateFields(end[i])); err != nil {
					return err
				}
			}
			for _, class := range benignClasses {
				if err := add(configFields(a.Config), []string{falseBansOf(class)}, estimateFields(a.FalseBansByClass[class])); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// falseBansOf names the summary's metric of the false bans of class.
func falseBansOf(class Class) string {
	return MetricFalseBans.String() + "_" + string(class)
}

// writeGzipped writes the output of write gzipped, without name and
// time, so that the same report gives the same file.
func writeGzipped(w io.Writer, write func(io.Writer) error) error {
	gz := gzip.NewWriter(w)
	if err := write(gz); err != nil {
		return err
	}
	return gz.Close()
}

// writePublishers writes the feed metrics of every publisher key of every
// run, gzipped.
func writePublishers(w io.Writer, rep *Report) error {
	return writeGzipped(w, func(w io.Writer) error {
		header := []string{"model", "fraction", "profile", "seed", "slot", "key", "role", "volume", "exclusive", "latency_minutes", "bound", "accuracy"}
		return writeCSV(w, header, func(add func(...[]string) error) error {
			for i, a := range rep.Aggregates {
				for seed, pubs := range rep.Publishers[i] {
					for _, p := range pubs {
						if err := add(configFields(a.Config), []string{strconv.Itoa(seed + 1), strconv.Itoa(p.Slot), strconv.Itoa(p.Key), string(p.Role),
							csvFloat(p.Volume), csvFloat(p.Exclusive), csvFloat(p.LatencyMinutes), csvFloat(p.Bound), csvFloat(p.Accuracy)}); err != nil {
							return err
						}
					}
				}
			}
			return nil
		})
	})
}

// writeHourly writes every metric of every configuration in both windows
// of every hour, gzipped; a metric defined in no seed is left out.
func writeHourly(w io.Writer, rep *Report) error {
	return writeGzipped(w, func(gz io.Writer) error { return writeHourlyCSV(gz, rep) })
}

func writeHourlyCSV(gz io.Writer, rep *Report) error {
	return writeCSV(gz, []string{"model", "fraction", "profile", "hour", "window", "metric", "n", "mean", "ci_low", "ci_high"}, func(add func(...[]string) error) error {
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
