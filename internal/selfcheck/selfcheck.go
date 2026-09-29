// Package selfcheck checks an OBIE node and the host it runs on, and says
// for every check whether all is well, what needs attention or what is
// wrong, always with the next step (`obied self-check`, ADR 0027). It
// changes nothing, never prints the node key, and works before the node's
// first start as well as on a running node.
package selfcheck

import (
	"context"
	"net"
	"net/netip"
	"os/user"
	"slices"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// Status is the outcome of a check.
type Status string

// Statuses, from best to worst.
const (
	// OK: all is well.
	OK Status = "ok"
	// Warning: something needs attention, or could not be checked.
	Warning Status = "warning"
	// Problem: something is wrong, or the node cannot work as configured.
	Problem Status = "problem"
)

// rank orders the statuses from best to worst.
func (s Status) rank() int {
	switch s {
	case Problem:
		return 2
	case Warning:
		return 1
	default:
		return 0
	}
}

// Check is the result of one check.
type Check struct {
	// ID names the check in machine-readable output; it never changes.
	ID string `json:"id"`
	// Name names the check for people.
	Name   string `json:"name"`
	Status Status `json:"status"`
	// Summary is the most important finding, in one sentence.
	Summary string `json:"summary"`
	// Details are the other findings.
	Details []string `json:"details,omitempty"`
	// NextSteps say what to do about every warning and problem.
	NextSteps []string `json:"next_steps,omitempty"`
}

// Report is the result of every check.
type Report struct {
	// Status is the worst status of any check.
	Status Status `json:"status"`
	// Config is the configuration file checked.
	Config string `json:"config"`
	// Version is the version of the obied that checked.
	Version string `json:"version"`
	// User is the user the check ran as; Root is set for root.
	User   string  `json:"user"`
	Root   bool    `json:"root"`
	Checks []Check `json:"checks"`
	// Summary counts the checks of each status.
	Summary map[Status]int `json:"summary"`
	// lockout is the operator's session address when it is not protected,
	// for the banner of the text report; enforcing says whether the node
	// blocks.
	lockout   netip.Addr
	enforcing bool
}

// finding is one observation of a check.
type finding struct {
	status Status
	text   string
	next   string
}

func ok(text string) finding            { return finding{status: OK, text: text} }
func warn(text, next string) finding    { return finding{status: Warning, text: text, next: next} }
func problem(text, next string) finding { return finding{status: Problem, text: text, next: next} }

// newCheck builds a check from its findings: its status is the worst of
// them, its summary the first finding of that status, and the others are
// its details; the next steps of every warning and problem are kept once,
// in order.
func newCheck(id, name string, findings ...finding) Check {
	c := Check{ID: id, Name: name, Status: OK}
	lead, worst := 0, OK
	for i, f := range findings {
		if f.status.rank() > worst.rank() {
			lead, worst = i, f.status
		}
	}
	for i, f := range findings {
		if i == lead {
			c.Status, c.Summary = f.status, f.text
		} else {
			c.Details = append(c.Details, f.text)
		}
		if f.status != OK && f.next != "" && !slices.Contains(c.NextSteps, f.next) {
			c.NextSteps = append(c.NextSteps, f.next)
		}
	}
	return c
}

// Env is how the checks see the node and the host; HostEnv returns the
// real one, tests a fake.
type Env struct {
	// ConfigPath is the configuration file to check.
	ConfigPath string
	// ServiceUser is the user obied runs as.
	ServiceUser string
	// Version is the version of this obied.
	Version string

	// Euid returns the effective user ID the check runs as.
	Euid func() int
	// UserName returns the name of the user with uid, or the uid.
	UserName func(uid int) string
	// Operator names the person running the check: the user who called
	// sudo, else the user it runs as.
	Operator    func() string
	LookupUser  func(name string) (*user.User, error)
	LookupGroup func(name string) (*user.Group, error)
	// InGroup reports whether the named user is a member of the group
	// with gid.
	InGroup func(userName, gid string) (bool, error)

	// Node returns a client of the admin API on socket.
	Node func(socket string) NodeClient
	// Dial connects to a peer, to see whether it answers.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// Now returns the current time.
	Now func() time.Time
	// Clock returns the kernel's view of the clock (KernelClock).
	Clock func() (ClockState, error)

	// LookPath finds a program on the PATH.
	LookPath func(file string) (string, error)
	// Command runs a program and returns its standard output.
	Command func(ctx context.Context, name string, args ...string) ([]byte, error)
	// Fail2BanDir is Fail2Ban's configuration directory.
	Fail2BanDir string
	// NFTables checks whether nftables can be used (nft.Probe).
	NFTables func(ctx context.Context) error
	// Session returns the address of the operator's SSH session.
	Session func() (netip.Addr, bool)
	// Allowlist is how the allow-list of the configuration learns about
	// the host.
	Allowlist sovereignty.Env
}

// run is one self-check: the environment and what the checks share.
type run struct {
	ctx context.Context
	env Env
	// cfg is the configuration, nil if it could not be read or is
	// invalid; cfgErr is why the configuration check fails.
	cfg    *config.Config
	cfgErr error
	// socket is the admin socket asked; node its client; status the
	// node's answer, statusErr why there is none.
	socket    string
	node      NodeClient
	status    *admin.StatusResponse
	statusErr error
	// lockout is the operator's session address when it is not protected;
	// enforcing says whether the node blocks.
	lockout   netip.Addr
	enforcing bool
}

// Run runs every check and returns the report.
func Run(ctx context.Context, env Env) Report {
	r := &run{ctx: ctx, env: env}
	r.loadConfig()
	r.queryNode()
	euid := env.Euid()
	report := Report{Status: OK, Config: env.ConfigPath, Version: env.Version, User: env.UserName(euid), Root: euid == 0,
		Summary: map[Status]int{OK: 0, Warning: 0, Problem: 0}}
	for _, check := range []func() Check{
		r.checkConfig, r.checkIdentity, r.checkAdmin, r.checkNode, r.checkPeers, r.checkClock,
		r.checkFail2Ban, r.checkFirewall, r.checkSession,
	} {
		c := check()
		report.Checks = append(report.Checks, c)
		report.Summary[c.Status]++
		if c.Status.rank() > report.Status.rank() {
			report.Status = c.Status
		}
	}
	report.lockout, report.enforcing = r.lockout, r.enforcing
	return report
}

// loadConfig loads the configuration as obied --check-config does: the
// file and its allow-list files.
func (r *run) loadConfig() {
	file, err := config.LoadFile(r.env.ConfigPath)
	if err != nil {
		r.cfgErr = err
		return
	}
	r.cfg = file.Config
	_, r.cfgErr = sovereignty.ReadFiles(r.cfg.Allowlist.Files)
}

// me names the user the check runs as.
func (r *run) me() string { return r.env.UserName(r.env.Euid()) }

// asRoot is the next step when the check lacks the privileges to look.
const asRoot = "run the self-check as root: sudo obied self-check"

// notChecked is the result of a check that needs the configuration when
// it could not be loaded.
func notChecked(id, name string) Check {
	return newCheck(id, name, warn("not checked: the configuration could not be loaded",
		"fix the configuration first, then run the self-check again"))
}
