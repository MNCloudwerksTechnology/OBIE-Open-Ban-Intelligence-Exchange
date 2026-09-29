package console

import (
	"net/netip"
	"time"
)

// Override is an operator override for the overrides view (ADR 0024).
type Override struct {
	// Range is the address or network it is set on.
	Range netip.Prefix
	// Action is force_allow or force_block.
	Action string
	Note   string
	// CreatedAt is when it was set; ExpiresAt when it ends, zero if never.
	CreatedAt, ExpiresAt time.Time
	// Overruled is the rule that beats a force-block in effect, which so
	// has no effect: an overlapping force-allow override or a protected
	// allow-list entry. Nil if it takes effect, and for other overrides.
	Overruled *Ruling
}

// AllowEntry is an entry of the running allow-list.
type AllowEntry struct {
	Range netip.Prefix
	// Source is builtin, self, bootstrap, config or file; Label describes
	// the entry: the class of a built-in range, the listen or bootstrap
	// address it comes from, or the file and line.
	Source, Label string
}

// Allowlist is the running allow-list, and what its files hold now.
type Allowlist struct {
	// Entries are every entry, the protected ones first, each group in the
	// order it was loaded.
	Entries []AllowEntry
	// LoadedAt is when the running configuration, which built it, was
	// loaded.
	LoadedAt time.Time
	// Files are the allowlist.files it loaded, in their order.
	Files []AllowFile
	// Warnings are the addresses it should hold but could not determine.
	Warnings []AllowWarning
}

// AllowFile is one of the allowlist.files: what the allow-list loaded
// from it, and what it holds now.
type AllowFile struct {
	Path string
	// Loaded counts the entries loaded from it.
	Loaded int
	// Err says why the file cannot be read now; the fields below are then
	// unset.
	Err string
	// Changed is set if its content differs from what was loaded;
	// Entries counts the entries it holds now.
	Changed bool
	Entries int
	// Rejected are the first of the lines the node rejects now;
	// RejectedLines counts them all.
	Rejected      []RejectedLine
	RejectedLines int
}

// RejectedLine is a line of an allow-list file that holds no valid entry.
type RejectedLine struct {
	// Line is its number, from 1; Text is its entry without the comment,
	// and Err says what is wrong with it.
	Line      int
	Text, Err string
}

// AllowWarning is an address the allow-list should hold but could not
// determine, so it is not protected until the next reload.
type AllowWarning struct {
	// Source is self for the interface addresses of an unspecified listen
	// address, bootstrap for a bootstrap peer's DNS name; Subject is that
	// listen or bootstrap address and Err what went wrong.
	Source, Subject, Err string
}

// Protection is what the running allow-list and the overrides do to one
// address or network, as the decision engine judges it.
type Protection struct {
	Range netip.Prefix
	// Ruling names the rule that decides; its Effect is empty if none
	// does.
	Ruling Ruling
	// Overlapping are the allow-list entries that overlap the range, the
	// protected ones first.
	Overlapping []AllowEntry
	// Decidable is set if the node can decide on the range, so that it
	// has an explanation.
	Decidable bool
}

// Configuration is the configuration the node runs with, and the file on
// disk now.
type Configuration struct {
	// Path is the file the running configuration was read from; empty if
	// it was read from none.
	Path string
	// Load is when the running configuration was loaded and how the last
	// reload went.
	Load ConfigFacts
	// Settings are every setting, in the order of the configuration
	// reference.
	Settings []Setting
	// DiskErr says why the file on disk, or an allow-list file it names,
	// cannot be loaded now: a reload would be rejected. Empty if they load,
	// or without a file.
	DiskErr string
}

// Setting is one key of the running configuration.
type Setting struct {
	// Key is its key path, e.g. "mesh.rate_limit.peer.burst"; Section is
	// its first part.
	Key, Section string
	// Summary explains it in one line.
	Summary string
	// Applied is "reload" if a reload applies a change, else "restart".
	Applied string
	// Value is its running value; Default is set if the file does not set
	// it.
	Value   SettingValue
	Default bool
	// Disk is its value in the file on disk now if that differs from the
	// running one; nil if it does not or the file cannot be loaded.
	Disk *SettingValue
}

// SettingValue is the value of a setting as the file spells it.
type SettingValue struct {
	// List is set for a list, whose entries are Items; Text is any other
	// value.
	List  bool
	Items []string
	Text  string
	// Secret is set for a value that must never be shown: Text and Items
	// are then empty, and IsSet says whether it holds anything.
	Secret, IsSet bool
}

// RuleSource reads the operator's rules and the configuration for the
// overrides, allow-list and configuration views (ADR 0024). The daemon
// implements it with cheap reads only, and every read works while its
// subsystem is stopped.
type RuleSource interface {
	// Overrides reads the overrides in effect, or with expired those that
	// expired and the store still keeps.
	Overrides(expired bool) ([]Override, error)
	// OverrideRetention is how long the store keeps an override after its
	// expiry.
	OverrideRetention() time.Duration
	// Allowlist reads the running allow-list, and each of its files again.
	Allowlist() Allowlist
	// Protection judges the address or network p with the running
	// allow-list and the overrides in effect.
	Protection(p netip.Prefix) (Protection, error)
	// Configuration reads the running configuration, and the file on disk
	// to compare them.
	Configuration() Configuration
}
