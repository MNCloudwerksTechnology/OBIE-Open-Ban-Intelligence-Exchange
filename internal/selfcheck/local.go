package selfcheck

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"syscall"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/statedir"
)

// checkConfig: the configuration file exists, can be read and is valid,
// with its allow-list files.
func (r *run) checkConfig() Check {
	const id, name = "config", "Configuration"
	path := r.env.ConfigPath
	recheck := fmt.Sprintf("fix it, then check the file: sudo obied --config %s --check-config", path)
	var cfgErr *config.Error
	switch err := r.cfgErr; {
	case err == nil:
		return newCheck(id, name, ok(fmt.Sprintf("%s is valid; the node runs in %s mode", path, r.cfg.Node.Mode)))
	case errors.Is(err, fs.ErrNotExist) && r.cfg == nil:
		return newCheck(id, name, problem(fmt.Sprintf("there is no configuration file at %s", path),
			"create one: sudo obied setup"))
	case errors.Is(err, fs.ErrPermission) && r.cfg == nil:
		return newCheck(id, name, problem(fmt.Sprintf("cannot read %s as user %s", path, r.me()), asRoot))
	case errors.As(err, &cfgErr):
		c := newCheck(id, name, problem(fmt.Sprintf("%s is invalid: %s", path, plural(len(cfgErr.Problems), "problem")),
			fmt.Sprintf("fix the keys named here, then check the file: sudo obied --config %s --check-config", path)))
		for _, p := range cfgErr.Problems {
			c.Details = append(c.Details, p.String())
		}
		return c
	default:
		return newCheck(id, name, problem(fmt.Sprintf("%s: %v", path, err), recheck))
	}
}

// plural returns "1 problem" or "n problems".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// started reports whether the node has run on its state directory: obied
// stamps the directory's FORMAT file at its first start. known is false
// when the directory cannot be looked into.
func (r *run) started() (started, known bool) {
	_, err := os.Stat(statedir.Path(r.cfg.Node.StateDir))
	switch {
	case err == nil:
		return true, true
	case errors.Is(err, fs.ErrNotExist):
		return false, true
	default:
		return false, false
	}
}

// serviceUID returns the user ID of the service user.
func (r *run) serviceUID() (int, error) {
	u, err := r.env.LookupUser(r.env.ServiceUser)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(u.Uid)
}

// checkIdentity: the node key exists, belongs to the service user and
// nobody else can read it.
func (r *run) checkIdentity() Check {
	const id, name = "identity", "Identity"
	if r.cfg == nil {
		return notChecked(id, name)
	}
	svc := r.env.ServiceUser
	uid, err := r.serviceUID()
	if err != nil {
		return newCheck(id, name, problem(fmt.Sprintf("the service user %s does not exist", svc),
			"run install.sh from the release, which creates it, or name the user obied runs as: obied self-check --service-user <user>"))
	}
	stateDir := r.cfg.Node.StateDir
	path := identity.Path(stateDir)
	key, err := identity.Verify(stateDir, uid)
	switch {
	case err == nil:
		return newCheck(id, name, ok(fmt.Sprintf("%s exists and only %s can read it; the node's peer ID is %s", path, svc, key.PeerID())))
	case errors.Is(err, fs.ErrPermission):
		return newCheck(id, name, warn(fmt.Sprintf("cannot look into %s as user %s", stateDir, r.me()), asRoot))
	case errors.Is(err, fs.ErrNotExist):
		started, known := r.started()
		if known && !started {
			return newCheck(id, name, warn(fmt.Sprintf("there is no identity yet: obied creates %s at its first start", path),
				"start the node: sudo systemctl enable --now obied"))
		}
		return newCheck(id, name, problem(
			fmt.Sprintf("%s is missing: the next start creates a new identity, which the node's peers do not know", path),
			fmt.Sprintf("restore node.key from your backup to %s (owned by %s, mode 0600), or tell your peers the new peer ID after the start", stateDir, svc)))
	case errors.Is(err, identity.ErrCorrupt):
		return newCheck(id, name, problem(err.Error(),
			fmt.Sprintf("restore node.key from your backup, or create a new identity (a new peer ID): sudo -u %s obied keygen --force", svc)))
	default:
		return newCheck(id, name, problem(err.Error(), fmt.Sprintf(
			"give the key to %s and nobody else: sudo chown %s %s; sudo chmod 600 %s; sudo chmod 700 %s",
			svc, svc, path, path, stateDir)))
	}
}

// checkAdmin: the admin socket's group exists, and the socket, once the
// node created it, is open to root, the service user and that group only.
func (r *run) checkAdmin() Check {
	const id, name = "admin", "Admin access"
	if r.cfg == nil {
		return notChecked(id, name)
	}
	socket, group := r.cfg.Admin.Socket, r.cfg.Admin.SocketGroup
	g, err := r.env.LookupGroup(group)
	if err != nil {
		return newCheck(id, name, problem(
			fmt.Sprintf("the group %s (admin.socket_group) does not exist, so only root and %s can use obiectl", group, r.env.ServiceUser),
			fmt.Sprintf("create it: sudo groupadd --system %s (install.sh does), or set admin.socket_group to the group obied runs in", group)))
	}
	findings := []finding{r.socketFinding(socket, g.Gid, group), r.operatorFinding(g.Gid, group)}
	return newCheck(id, name, findings...)
}

// socketFinding checks the admin socket's type, mode and group.
func (r *run) socketFinding(socket, gid, group string) finding {
	info, err := os.Lstat(socket)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ok(fmt.Sprintf("the admin socket %s appears when the node runs; only root, %s and the group %s may use it",
			socket, r.env.ServiceUser, group))
	case errors.Is(err, fs.ErrPermission):
		return warn(fmt.Sprintf("cannot look at the admin socket %s as user %s", socket, r.me()), asRoot)
	case err != nil:
		return warn(fmt.Sprintf("cannot look at the admin socket %s: %v", socket, err), asRoot)
	case info.Mode().Type() != fs.ModeSocket:
		return problem(fmt.Sprintf("%s is not a socket, so obied cannot start its admin interface", socket),
			fmt.Sprintf("remove it: obied creates the socket at its start; check admin.socket in %s", r.env.ConfigPath))
	}
	perm := info.Mode().Perm()
	if perm&0o007 != 0 {
		return problem(fmt.Sprintf("the admin socket %s has mode %04o: every user of this server can control the node", socket, perm),
			"restart the node, which makes it 0660: sudo systemctl restart obied")
	}
	if st, isStat := info.Sys().(*syscall.Stat_t); isStat && strconv.FormatUint(uint64(st.Gid), 10) != gid {
		return problem(fmt.Sprintf("the admin socket %s does not belong to the group %s, so its members cannot use obiectl", socket, group),
			"make admin.socket_group the group obied runs in (the unit's Group=), then restart: sudo systemctl restart obied")
	}
	return ok(fmt.Sprintf("the admin socket %s is open to root, %s and the group %s only (mode %04o)", socket, r.env.ServiceUser, group, perm))
}

// operatorFinding says whether the operator may use obiectl without sudo.
func (r *run) operatorFinding(gid, group string) finding {
	op := r.env.Operator()
	if op == "root" {
		return ok("you work as root and may use obiectl")
	}
	member, err := r.env.InGroup(op, gid)
	switch {
	case err != nil:
		return ok(fmt.Sprintf("members of the group %s may use obiectl; whether %s is one could not be determined: %v", group, op, err))
	case member:
		return ok(fmt.Sprintf("%s is in the group %s and may use obiectl without sudo", op, group))
	}
	return ok(fmt.Sprintf("%s is not in the group %s: use sudo obiectl, or join the group with sudo usermod -aG %s %s and log in again",
		op, group, group, op))
}
