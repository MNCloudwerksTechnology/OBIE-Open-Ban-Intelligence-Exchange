package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// reportClientError explains on stderr why program could not do what it
// was asked through client.
func reportClientError(stderr io.Writer, program string, client *admin.Client, err error) {
	clientProblem(program, client.Socket(), err).write(stderr, program)
}

// clientProblem explains a failed call of program to the node's admin
// socket: what went wrong, why and what to do next.
func clientProblem(program, socket string, err error) problem {
	ctl := ctlCommandLine(socket)
	var apiErr *admin.APIError
	switch {
	case errors.Is(err, admin.ErrDaemonNotRunning):
		return notRunningProblem(program, socket)
	case errors.Is(err, fs.ErrPermission):
		return permissionProblem(socket)
	case errors.Is(err, context.DeadlineExceeded):
		return problem{id: "node-timeout", what: "obied did not answer in time (--timeout)",
			why: "the node is busy, still starting, or stuck",
			next: []string{"try again with more time: " + ctl + " --timeout 30s " + commandOf(program),
				"see whether the node is ready and what it logs: " + ctl + " --timeout 30s status; sudo journalctl -u obied -n 50"}}
	case errors.Is(err, admin.ErrNoOverride):
		return problem{id: "no-override", what: err.Error(), next: []string{"see which overrides there are: " + ctl + " overrides"}}
	case errors.As(err, &apiErr):
		return apiProblem(program, socket, apiErr)
	}
	return problem{id: "node-unreachable", what: fmt.Sprintf("cannot talk to obied: %v", err),
		next: []string{"check the node and its admin socket: sudo obied self-check"}}
}

// ctlCommandLine is how a next step runs obiectl against socket: with
// --socket unless it is the default.
func ctlCommandLine(socket string) string {
	if socket == config.Default().Admin.Socket {
		return "sudo obiectl"
	}
	return "sudo obiectl --socket " + config.QuotePath(socket)
}

// notRunningProblem explains that nothing answers on socket.
func notRunningProblem(program, socket string) problem {
	p := problem{id: "node-not-running",
		what: "obied is not running: nothing answers on the admin socket " + socket,
		why:  "the node has stopped, or is just starting or stopping",
		next: []string{"start it: sudo systemctl start obied",
			"if it does not stay up, see why: sudo journalctl -u obied -n 20, or sudo obied self-check",
			"if it listens on another socket (admin.socket), name that: obiectl --socket <path> " + commandOf(program)}}
	if _, err := os.Stat(socket); errors.Is(err, fs.ErrNotExist) {
		p.what = "obied is not running: there is no admin socket " + socket
		p.why = "the node was not started, has stopped, or uses another socket"
	}
	return p
}

// permissionProblem explains that this user may not use socket.
func permissionProblem(socket string) problem {
	me, group := currentUserName(), socketGroup(socket)
	join := "or join the group that owns " + socket + " and log in again"
	if group != "" {
		join = fmt.Sprintf("or join the group: sudo usermod -aG %s %s, then log out and in again", group, me)
	} else {
		group = "that owns the socket"
	}
	return problem{id: "admin-permission-denied",
		what: fmt.Sprintf("permission denied: user %s may not use the admin socket %s", me, socket),
		why:  "only root and members of the group " + group + " may control the node",
		next: []string{"run the command as root, with sudo in front of it", join}}
}

// apiProblem explains a refusal of the admin API.
func apiProblem(program, socket string, e *admin.APIError) problem {
	msg, ctl := e.Message, ctlCommandLine(socket)
	switch e.StatusCode {
	case http.StatusForbidden:
		return permissionProblem(socket)
	case http.StatusUnprocessableEntity:
		return protectedProblem(ctl, strings.TrimPrefix(msg, "refused: "))
	case http.StatusNotFound:
		p := problem{id: "not-found", what: strings.TrimPrefix(msg, "not found: ")}
		if commandOf(program) == "revoke" {
			p.next = []string{"see what this node has reported: " + ctl + " indicators --mine"}
		} else {
			p.next = []string{"check the address or ID; " + ctl + " decisions lists what the node knows"}
		}
		return p
	case http.StatusBadRequest:
		return problem{id: "request-invalid", what: "the node refused the request: " + strings.TrimPrefix(msg, "invalid request: "),
			next: []string{"check the arguments: " + program + " --help"}}
	case http.StatusServiceUnavailable:
		return problem{id: "node-unavailable", what: msg, why: "the node is still starting, or one of its parts failed",
			next: []string{"see which part is not ready and why: " + ctl + " status; sudo journalctl -u obied -n 50"}}
	case http.StatusInternalServerError:
		return problem{id: "node-failed", what: msg, next: []string{"see why in the node's log: sudo journalctl -u obied -n 50"}}
	}
	return problem{id: "node-error", what: fmt.Sprintf("the node answered %s: %s", e.Status, msg),
		next: []string{"see the node's log: sudo journalctl -u obied -n 50"}}
}

// protectedProblem explains that the node refused to report an address
// because it is protected or allow-listed. msg is the node's refusal, the
// address and the rule, then after "; " why it refuses, which the problem
// says itself. ctl is how the next steps run obiectl.
func protectedProblem(ctl, msg string) problem {
	what, _, _ := strings.Cut(msg, "; ")
	p := problem{id: "address-protected", what: "nothing was reported: " + what,
		why: "OBIE never reports private, loopback, link-local and other special-purpose addresses, " +
			"nor the networks on your allow-list, so that no node blocks them because of you",
		next: []string{"nothing needs to be done if this is right; " + ctl + " explain <address> shows the rule that protects it"}}
	if strings.Contains(msg, "allow-listed") {
		p.next = append(p.next, "if the address should not be protected, remove it from allowlist.cidrs and reload: sudo systemctl reload obied")
	}
	for _, r := range documentationRanges {
		if strings.Contains(msg, "special-purpose range "+r) {
			p.next = []string{"this address is reserved for examples, like those in the help; report the attacking address from your log instead"}
		}
	}
	return p
}

// documentationRanges are reserved for examples (RFC 5737, RFC 3849): an
// address in them was most likely copied from an example.
var documentationRanges = []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"}

// commandOf returns the command of program, e.g. "status" for "obiectl
// status".
func commandOf(program string) string {
	_, name, _ := strings.Cut(program, " ")
	return name
}

// currentUserName returns the name of the user running the program.
func currentUserName() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return strconv.Itoa(os.Getuid())
}
