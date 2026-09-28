package console

import (
	"fmt"
	"net/http"
	"time"
)

// When a changed setting takes effect, as config.Setting names it.
const (
	appliedReload  = "reload"
	appliedRestart = "restart"
)

// configurationPage is the data of the configuration view: how the
// running configuration was loaded, how the file on disk differs, then
// every setting by section (ADR 0024). It is read when the page opens.
type configurationPage struct {
	ReadAt timestamp
	// Err says why the configuration could not be read.
	Err string
	// Path is the configuration file; empty if the node read none.
	Path string
	// LoadedAt is when the running configuration was loaded, LoadedBy how.
	LoadedAt timestamp
	LoadedBy string
	// Reload says how the last reload went; Disk how the file on disk
	// differs from the running configuration.
	Reload, Disk statusLine
	// Pending and Restart name the settings changed on disk that a reload
	// applies, and those that wait for a restart.
	Pending, Restart []string
	// Defaults counts the settings the file does not set.
	Defaults int
	Sections []settingsSection
}

// statusLine is a condition in one line: State is ok, changed, warning or
// none for the stylesheet.
type statusLine struct {
	State, Text string
}

// settingsSection is the settings of one section of the file.
type settingsSection struct {
	// Name is the section's key, e.g. "mesh"; Text says what it is about.
	Name, Text string
	Rows       []settingRow
}

// settingRow is one setting.
type settingRow struct {
	// Key is its key path; Summary explains it.
	Key, Summary string
	// Value is its running value; Default is set if the file does not set
	// it.
	Value   valueView
	Default bool
	// Applied is reload or restart; AppliedText says what applies it.
	Applied, AppliedText string
	// Disk is its value in the file on disk if that differs; Pending says
	// what that means.
	Disk    *valueView
	Pending string
}

// valueView is a setting's value for the page.
type valueView struct {
	// Text is a value that is no list; Items the entries of a list, which
	// Empty marks as having none.
	Text  string
	List  bool
	Items []string
	Empty bool
	// Secret is set for a value never shown; Text then says whether it is
	// set.
	Secret bool
}

// sectionTexts say what each section of the file is about.
var sectionTexts = map[string]string{
	"node":      "The node itself: where it keeps its state and whether it blocks.",
	"admin":     "The local admin API that obiectl and this console use.",
	"mesh":      "How the node joins the mesh of other nodes, and how many events it accepts from them.",
	"store":     "The local store of verdicts.",
	"trust":     "Whose verdicts count in decisions, and how much.",
	"decision":  "When the node blocks an address, and for how long.",
	"allowlist": "Addresses and networks the node never blocks, besides the built-in ones.",
	"enforce":   "How blocks reach the firewall in enforce mode.",
	"metrics":   "The Prometheus metrics and health endpoints.",
	"console":   "This web console.",
	"audit":     "The audit log of decision changes.",
	"log":       "The log obied writes.",
}

// configurationInput is what the configuration view is built from.
type configurationInput struct {
	now time.Time
	cfg Configuration
	err error
}

// buildConfiguration builds the configuration view.
func buildConfiguration(in configurationInput) configurationPage {
	c := &in.cfg
	p := configurationPage{ReadAt: stamp(in.now), Path: c.Path, LoadedAt: stamp(c.Load.LoadedAt), LoadedBy: "at start"}
	if in.err != nil {
		p.Err = in.err.Error()
		return p
	}
	if c.Load.Reloaded {
		p.LoadedBy = "by a reload"
	}
	p.Reload = reloadLine(&c.Load)
	for i := range c.Settings {
		s := &c.Settings[i]
		row := newSettingRow(s)
		if row.Default {
			p.Defaults++
		}
		if row.Disk != nil {
			if s.Applied == appliedReload {
				p.Pending = append(p.Pending, s.Key)
			} else {
				p.Restart = append(p.Restart, s.Key)
			}
		}
		if n := len(p.Sections); n == 0 || p.Sections[n-1].Name != s.Section {
			p.Sections = append(p.Sections, settingsSection{Name: s.Section, Text: sectionTexts[s.Section]})
		}
		p.Sections[len(p.Sections)-1].Rows = append(p.Sections[len(p.Sections)-1].Rows, row)
	}
	p.Disk = diskLine(c, len(p.Pending), len(p.Restart))
	return p
}

// reloadLine says how the last reload went.
func reloadLine(l *ConfigFacts) statusLine {
	switch {
	case l.Rejected != "":
		return statusLine{State: "warning", Text: fmt.Sprintf("The last reload, at %s, was rejected: %s. The configuration "+
			"loaded at %s is still active.", stamp(l.RejectedAt).Text, l.Rejected, stamp(l.LoadedAt).Text)}
	case l.Reloaded:
		return statusLine{State: "ok", Text: fmt.Sprintf("The last reload, at %s, succeeded.", stamp(l.LoadedAt).Text)}
	default:
		return statusLine{State: "none", Text: "The configuration was not reloaded since obied started."}
	}
}

// diskLine says how the file on disk differs from the running
// configuration: in pending settings a reload applies, and in restart
// settings only a restart applies, which a reload leaves waiting.
func diskLine(c *Configuration, pending, restart int) statusLine {
	switch {
	case c.Path == "":
		return statusLine{State: "none", Text: "The node runs with a configuration it did not read from a file."}
	case c.DiskErr != "":
		return statusLine{State: "warning", Text: "The file on disk cannot be loaded now: " + c.DiskErr +
			". A reload would be rejected, and the running configuration kept."}
	case pending > 0:
		differ := "differ from the running configuration and are"
		if pending+restart == 1 {
			differ = "differs from the running configuration and is"
		}
		return statusLine{State: "changed", Text: fmt.Sprintf("The file on disk changed since it was loaded: %s %s not "+
			"active yet.", plural(pending+restart, "setting", "settings"), differ)}
	case restart > 0:
		return statusLine{State: "changed", Text: fmt.Sprintf("The file on disk differs from the running configuration "+
			"in %s that only a restart of obied applies.", plural(restart, "setting", "settings"))}
	default:
		return statusLine{State: "ok", Text: "The file on disk matches the running configuration."}
	}
}

// newSettingRow describes the setting s.
func newSettingRow(s *Setting) settingRow {
	r := settingRow{Key: s.Key, Summary: s.Summary, Value: newValueView(&s.Value), Default: s.Default, Applied: s.Applied,
		AppliedText: "Reload"}
	if s.Applied != appliedReload {
		r.Applied, r.AppliedText = appliedRestart, "Restart"
	}
	if s.Disk != nil {
		disk := newValueView(s.Disk)
		r.Disk = &disk
		r.Pending = "Not active until a reload."
		if r.Applied == appliedRestart {
			r.Pending = "Waits for a restart."
		}
	}
	return r
}

// newValueView shows the value v; a secret only as set or not set.
func newValueView(v *SettingValue) valueView {
	switch {
	case v.Secret && v.IsSet:
		return valueView{Secret: true, Text: "Set, not shown"}
	case v.Secret:
		return valueView{Secret: true, Text: "Not set"}
	case v.List:
		return valueView{List: true, Items: v.Items, Empty: len(v.Items) == 0}
	default:
		return valueView{Text: v.Text}
	}
}

// configurationContent reads the running configuration and the file on
// disk.
func (c *Console) configurationContent(*http.Request) any {
	in := configurationInput{now: c.now()}
	if c.node.Rules == nil {
		in.err = errNoRules
	} else {
		in.cfg = c.node.Rules.Configuration()
	}
	return buildConfiguration(in)
}
