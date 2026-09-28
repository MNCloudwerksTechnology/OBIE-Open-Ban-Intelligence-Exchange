package daemon

import (
	"net/netip"
	"slices"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// consoleRules reads the overrides, the allow-list and the configuration
// for the console's overrides, allow-list and configuration views: the
// store's overrides, the engine's allow-list judged as the engine judges,
// the running configuration of the load record, and on request the
// configuration file and the allow-list files on disk (ADR 0024). Every
// read works while its subsystem is stopped. The engine is set before
// anything starts.
type consoleRules struct {
	store  *store.DB
	engine *decision.Engine
	loads  *configLoads
	now    func() time.Time
	// loadFile reads the configuration file at a path, checkFile an
	// allow-list file.
	loadFile  func(path string) (*config.File, error)
	checkFile func(path string) sovereignty.FileCheck
}

var _ console.RuleSource = (*consoleRules)(nil)

// Overrides reads the overrides in effect, each force-block with the rule
// that beats it if one does; or with expired those the store keeps.
func (r *consoleRules) Overrides(expired bool) ([]console.Override, error) {
	now := r.now()
	if expired {
		list, err := r.store.ExpiredOverrides(now)
		if err != nil {
			return nil, err
		}
		out := make([]console.Override, len(list))
		for i := range list {
			out[i] = consoleOverride(&list[i])
		}
		return out, nil
	}
	list, err := r.store.Overrides(now)
	if err != nil {
		return nil, err
	}
	allow, overrides := r.engine.Allowlist(), sovereignty.NewOverrides(list)
	out := make([]console.Override, len(list))
	for i := range list {
		o := &list[i]
		out[i] = consoleOverride(o)
		if o.Action != store.ForceBlock {
			continue
		}
		if ruling := sovereignty.Judge(o.Indicator, allow, overrides, now); ruling.Rule != sovereignty.RuleForceBlock {
			beaten := consoleRuling(&ruling)
			out[i].Overruled = &beaten
		}
	}
	return out, nil
}

// OverrideRetention is how long the store keeps an expired override.
func (r *consoleRules) OverrideRetention() time.Duration { return store.OverrideRetention }

// Allowlist reads the running allow-list, and each of its files again.
func (r *consoleRules) Allowlist() console.Allowlist {
	allow := r.engine.Allowlist()
	out := console.Allowlist{LoadedAt: r.loads.record().LoadedAt}
	for _, e := range allow.Entries() {
		out.Entries = append(out.Entries, consoleAllowEntry(e))
	}
	for _, f := range allow.Files() {
		out.Files = append(out.Files, allowFile(f, r.checkFile(f.Path)))
	}
	for _, w := range allow.Warnings() {
		out.Warnings = append(out.Warnings, console.AllowWarning{Source: string(w.Source), Subject: w.Subject, Err: w.Err.Error()})
	}
	return out
}

// allowFile compares what the allow-list loaded from a file with what the
// file holds now.
func allowFile(loaded sovereignty.FileLoad, now sovereignty.FileCheck) console.AllowFile {
	f := console.AllowFile{Path: loaded.Path, Loaded: loaded.Entries}
	if now.Err != nil {
		f.Err = now.Err.Error()
		return f
	}
	f.Changed, f.Entries, f.RejectedLines = now.Digest != loaded.Digest, now.Entries, now.RejectedLines
	for _, l := range now.Rejected {
		f.Rejected = append(f.Rejected, console.RejectedLine{Line: l.Line, Text: l.Text, Err: l.Err.Error()})
	}
	return f
}

// Protection judges the address or network p like the decision engine:
// with the running allow-list and the overrides in effect. It takes any
// range, also one the node cannot decide on.
func (r *consoleRules) Protection(p netip.Prefix) (console.Protection, error) {
	now := r.now()
	list, err := r.store.Overrides(now)
	if err != nil {
		return console.Protection{}, err
	}
	allow, overrides := r.engine.Allowlist(), sovereignty.NewOverrides(list)
	ruling := sovereignty.Judge(rangeIndicator(p), allow, overrides, now)
	if ruling.Rule == "" || (ruling.Rule == sovereignty.RuleAllowlist && !ruling.Source.Protected()) {
		if block, ok := enclosingBlock(p, allow, overrides, now); ok {
			ruling = block
		}
	}
	_, err = indicatorOfRange(p)
	pr := console.Protection{Range: p.Masked(), Ruling: consoleRuling(&ruling), Decidable: err == nil}
	for _, e := range allow.Overlapping(p) {
		pr.Overlapping = append(pr.Overlapping, consoleAllowEntry(e))
	}
	return pr, nil
}

// enclosingBlock returns the ruling of the first force-block on a network
// around p that takes effect: the firewall blocks that network, p with it,
// even if one of the operator's own allow-list entries covers p. Judge only
// weighs a force-block on p itself.
func enclosingBlock(p netip.Prefix, allow *sovereignty.Allowlist, ov *sovereignty.Overrides, now time.Time) (sovereignty.Ruling, bool) {
	p = p.Masked()
	for _, o := range ov.ForceBlocks() {
		q, err := sovereignty.PrefixOf(o.Indicator)
		if err != nil || q.Bits() >= p.Bits() || !q.Contains(p.Addr()) {
			continue
		}
		if ruling := sovereignty.Judge(o.Indicator, allow, ov, now); ruling.Rule == sovereignty.RuleForceBlock {
			return ruling, true
		}
	}
	return sovereignty.Ruling{}, false
}

// Configuration reads the running configuration and the keys its file
// set from the load record, and the file on disk to compare them.
func (r *consoleRules) Configuration() console.Configuration {
	rec := r.loads.record()
	c := console.Configuration{Load: configFacts(rec)}
	running := rec.File
	if running == nil {
		return c
	}
	c.Path = running.Path
	var disk *config.Config
	var changed []string
	if c.Path != "" {
		if f, err := r.loadFile(c.Path); err != nil {
			c.DiskErr = err.Error()
		} else {
			disk, changed = f.Config, config.Changed(running.Config, f.Config)
			c.DiskErr = r.filesErr(f.Config.Allowlist.Files)
		}
	}
	for _, s := range config.Settings() {
		v, _ := running.Config.Value(s.Key)
		setting := console.Setting{Key: s.Key, Section: s.Section(), Summary: s.Summary, Applied: string(s.Applied),
			Value: settingValue(s, v), Default: !running.Sets(s.Key)}
		if slices.Contains(changed, s.Key) {
			v, _ := disk.Value(s.Key)
			value := settingValue(s, v)
			setting.Disk = &value
		}
		c.Settings = append(c.Settings, setting)
	}
	return c
}

// filesErr says why one of the allow-list files at paths cannot be loaded
// now, as a reload would reject it; empty if they all load.
func (r *consoleRules) filesErr(paths []string) string {
	for _, path := range paths {
		check := r.checkFile(path)
		if err := check.LoadErr(path); err != nil {
			return "allow-list: " + err.Error()
		}
	}
	return ""
}

// settingValue converts the value v of the setting s; a secret's value
// never leaves the daemon, only whether it is set.
func settingValue(s config.Setting, v config.Value) console.SettingValue {
	if s.Secret {
		return console.SettingValue{Secret: true, IsSet: len(v.Items) > 0 || (v.Text != "" && v.Text != `""`)}
	}
	return console.SettingValue{List: v.List, Items: v.Items, Text: v.Text}
}

// consoleOverride converts a stored override for the console.
func consoleOverride(o *store.Override) console.Override {
	p, _ := sovereignty.PrefixOf(o.Indicator)
	return console.Override{Range: p, Action: string(o.Action), Note: o.Note, CreatedAt: o.CreatedAt, ExpiresAt: o.ExpiresAt}
}

// consoleAllowEntry converts an allow-list entry for the console.
func consoleAllowEntry(e sovereignty.Entry) console.AllowEntry {
	return console.AllowEntry{Range: e.Prefix, Source: string(e.Source), Label: e.Label}
}

// rangeIndicator returns the indicator naming the range p as the engine
// keys it, without checking that the node may decide on it.
func rangeIndicator(p netip.Prefix) obieproto.Indicator {
	p = p.Masked()
	switch {
	case p.Bits() < p.Addr().BitLen():
		return obieproto.Indicator{Kind: obieproto.KindCIDR, Value: p.String()}
	case p.Addr().Is4():
		return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: p.Addr().String()}
	default:
		return obieproto.Indicator{Kind: obieproto.KindIPv6, Value: p.Addr().String()}
	}
}
