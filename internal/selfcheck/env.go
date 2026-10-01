package selfcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"slices"
	"strconv"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
	"github.com/MNCloudwerksTechnology/obie/internal/session"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// Fail2BanDir is where Fail2Ban keeps its configuration.
const Fail2BanDir = "/etc/fail2ban"

// HostEnv returns the environment of a self-check of this host: of the
// node configured in configPath, run as serviceUser, by this obied of
// version.
func HostEnv(configPath, serviceUser, version string) Env {
	var dialer net.Dialer
	return Env{
		ConfigPath:  configPath,
		ServiceUser: serviceUser,
		Version:     version,
		Euid:        os.Geteuid,
		UserName:    userName,
		Operator:    operator,
		LookupUser:  user.Lookup,
		LookupGroup: user.LookupGroup,
		InGroup:     inGroup,
		Node:        func(socket string) NodeClient { return admin.NewClient(socket) },
		Dial:        dialer.DialContext,
		Now:         time.Now,
		Clock:       KernelClock,
		LookPath:    exec.LookPath,
		Command:     output,
		Fail2BanDir: Fail2BanDir,
		NFTables:    nft.Probe,
		Session:     func() (netip.Addr, bool) { return session.ClientAddr(session.Env{}) },
		Allowlist:   sovereignty.Env{},
	}
}

// userName returns the name of the user with uid, or the uid.
func userName(uid int) string {
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		return u.Username
	}
	return strconv.Itoa(uid)
}

// operator names the person running the check: under sudo the user who
// called it, else the user it runs as.
func operator() string {
	if name := os.Getenv("SUDO_USER"); name != "" && os.Geteuid() == 0 {
		return name
	}
	return userName(os.Geteuid())
}

// inGroup reports whether the named user is a member of the group with
// gid, as its primary group or a supplementary one.
func inGroup(name, gid string) (bool, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return false, err
	}
	if u.Gid == gid {
		return true, nil
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false, err
	}
	return slices.Contains(ids, gid), nil
}

// output runs a program and returns its standard output; a failure carries
// the first line of what the program wrote to its standard error.
func output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed programs of the checks (fail2ban-client, journalctl).
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if line, _, _ := bytes.Cut(bytes.TrimSpace(exitErr.Stderr), []byte("\n")); len(line) > 0 {
			return out, fmt.Errorf("%w: %s", err, line)
		}
	}
	return out, err
}
