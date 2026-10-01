package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/statedir"
)

// configReference says where every setting is described.
const configReference = "every setting is described in /etc/obie/obie.yaml.example and in the configuration reference, " +
	"documentation/operations/configuration.md"

// configProblem explains why the configuration file at path cannot be
// used: every mistake with its file, line and setting, a missing file or
// one this user may not read.
func configProblem(path string, err error) problem {
	check := "check the file again: sudo obied --check-config" + config.PathFlag(path)
	var cfgErr *config.Error
	switch {
	case errors.As(err, &cfgErr):
		p := problem{id: "config-invalid", what: fmt.Sprintf("the configuration %s is invalid:", path),
			next: []string{"fix these settings, then " + check, configReference}}
		for _, pr := range cfgErr.Problems {
			p.details = append(p.details, configMistake(path, pr))
		}
		return p
	case errors.Is(err, fs.ErrNotExist):
		return problem{id: "config-missing", what: "the configuration file " + path + " does not exist",
			next: []string{"write it after a few questions: sudo obied setup" + config.PathFlag(path),
				"or name the file the node uses: --config <file>"}}
	case errors.Is(err, fs.ErrPermission):
		return problem{id: "config-unreadable",
			what: fmt.Sprintf("cannot read the configuration file %s as user %s: permission denied", path, currentUserName()),
			why:  "only root and the group obie may read it, because it may name internal networks",
			next: []string{"run the command as root, with sudo in front of it"}}
	}
	return problem{id: "config-invalid", what: fmt.Sprintf("the configuration %s cannot be read: %v", path, err),
		next: []string{"fix the file, then " + check, configReference}}
}

// configMistake writes one mistake in a configuration file as
// "file:line: setting: message", the form editors and grep understand.
func configMistake(path string, p config.Problem) string {
	if p.Line > 0 {
		return fmt.Sprintf("%s:%d: %s: %s", path, p.Line, p.Path, p.Message)
	}
	return fmt.Sprintf("%s: %s: %s", path, p.Path, p.Message)
}

// allowlistProblem explains why an allow-list file of the configuration at
// path cannot be used.
func allowlistProblem(path string, err error) problem {
	return problem{id: "allowlist-file-invalid", what: err.Error(),
		why: "the node reads every file in allowlist.files at start and on every reload; each line holds one address or network, # starts a comment",
		next: []string{"fix or create the file, or remove it from allowlist.files in " + path +
			"; then check again: sudo obied --check-config" + config.PathFlag(path)}}
}

// identityProblem explains why the node's identity key in stateDir cannot
// be read. where are the flags that located stateDir, e.g. " --state-dir
// ./node-a", for the commands it suggests.
func identityProblem(stateDir, where string, err error) problem {
	keyFile := identity.Path(stateDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		restore := "or restore the key from your backup to " + keyFile + " (see Back up the node key in documentation/operations/operations.md)"
		if _, statErr := os.Stat(stateDir); errors.Is(statErr, fs.ErrNotExist) {
			return problem{id: "identity-missing", what: "this node has no identity yet: its state directory " + stateDir + " does not exist",
				why: "the node creates its state directory and identity key the first time it starts",
				next: []string{"start the node once: sudo systemctl enable --now obied", restore,
					"if the node keeps its state elsewhere, name that: --state-dir <directory>, or node.state_dir in --config"}}
		}
		return problem{id: "identity-missing", what: "this node has no identity yet: " + keyFile + " does not exist",
			why: "the node creates its identity key the first time it starts",
			next: []string{"start the node once: sudo systemctl enable --now obied",
				"or create the key now, as the user the node runs as: sudo -u obie obied keygen" + where, restore}}
	case errors.Is(err, fs.ErrPermission):
		return problem{id: "identity-unreadable",
			what: fmt.Sprintf("cannot read the identity key in %s as user %s: permission denied", stateDir, currentUserName()),
			why:  "the key belongs to the user the node runs as, and nobody else may read it",
			next: []string{"run the command as root, with sudo in front of it"}}
	case errors.Is(err, identity.ErrInsecure), errors.Is(err, identity.ErrCorrupt):
		// These errors end in their remedy after "; ".
		what, next, _ := strings.Cut(err.Error(), "; ")
		p := problem{id: "identity-unusable", what: what, next: []string{"restore the key from a backup"}}
		if next != "" {
			p.next = []string{next}
		}
		return p
	}
	return problem{id: "identity-unusable", what: err.Error(), next: []string{"check the state directory: sudo obied self-check"}}
}

// keygenProblem explains why the identity key could not be created in
// stateDir; where are the flags that located it, as for identityProblem.
func keygenProblem(stateDir, where string, err error) problem {
	if errors.Is(err, identity.ErrKeyExists) {
		return problem{id: "identity-exists", what: identity.Path(stateDir) + " exists already: this node has an identity",
			next: []string{"keep it; obied identity" + where + " shows its peer ID",
				"to replace it, which gives the node a new peer ID that its peers must be told: obied keygen" + where + " --force"}}
	}
	return problem{id: "keygen-failed", what: fmt.Sprintf("cannot create the identity key in %s: %v", stateDir, err),
		why:  "the key and its directory must belong to the user the node runs as",
		next: []string{"create it as that user: sudo -u obie obied keygen" + where}}
}

// stateDirProblem explains why the state directory cannot be used.
func stateDirProblem(err error) problem {
	p := problem{id: "state-dir-unusable", what: err.Error(), next: []string{"check the state directory: sudo obied self-check"}}
	if errors.Is(err, statedir.ErrNewerFormat) {
		what, next, _ := strings.Cut(err.Error(), "; ")
		p.what, p.next = what, []string{next}
	}
	return p
}

// teardownProblem explains why the table inet obie could not be removed.
func teardownProblem(err error) problem {
	p := problem{id: "teardown-failed", what: fmt.Sprintf("cannot remove the table inet %s: %v", nft.Table, err),
		next: []string{"remove it by hand: sudo nft delete table inet " + nft.Table}}
	if errors.Is(err, nft.ErrPermission) || errors.Is(err, fs.ErrPermission) {
		p.what = fmt.Sprintf("cannot remove the table inet %s: the kernel refused", nft.Table)
		p.why = "changing the firewall needs root (CAP_NET_ADMIN)"
		p.next = []string{"run it as root: sudo obied teardown-firewall"}
	}
	return p
}

// isListenError reports whether err is a failure to listen on a TCP or UDP
// port with errno; not on a Unix socket, which is a file.
func isListenError(err error, errno syscall.Errno) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "listen" && errors.Is(err, errno) {
		return !strings.HasPrefix(opErr.Net, "unix")
	}
	// The mesh reports why it could not listen as text only, e.g. "failed to
	// listen on any addresses: [listen tcp4 0.0.0.0:443: bind: permission
	// denied]", as libp2p does.
	return regexp.MustCompile(`listen (tcp|udp)[46]?( \S+)?: bind: ` + regexp.QuoteMeta(errno.Error())).MatchString(err.Error())
}

// startNext is the next step after obied failed to run with err, for the
// "next" attribute of its last log line. configPath is the configuration
// the node was started with.
func startNext(err error, configPath string) string {
	always := "sudo obied self-check" + config.PathFlag(configPath) + " names what is wrong; " +
		"documentation/operations/troubleshooting.md#obied-does-not-start lists the usual causes"
	switch {
	case isListenError(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EADDRINUSE):
		return "another process uses the address: stop it, or change mesh.listen, metrics.listen or console.listen; " + always
	case isListenError(err, syscall.EACCES):
		return "only root may listen on a port below 1024, and the service runs as the user obie: use a port of 1024 " +
			"or higher in mesh.listen, metrics.listen or console.listen; " + always
	case errors.Is(err, fs.ErrPermission):
		return "obied lacks a permission for what the error names: run it as the service, sudo systemctl start obied, " +
			"which runs as the user obie with the rights it needs; if the service fails, what the error names must belong " +
			"to obie and lie below /var/lib/obie, /var/log/obie or /run/obie, the only places the service may write; " + always
	}
	return always
}
