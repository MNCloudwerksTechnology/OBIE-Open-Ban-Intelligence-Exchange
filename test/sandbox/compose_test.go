package sandbox

import (
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// composeService is what the hardening test reads of a Compose service.
type composeService struct {
	Image       string   `yaml:"image"`
	CapDrop     []string `yaml:"cap_drop"`
	CapAdd      []string `yaml:"cap_add"`
	SecurityOpt []string `yaml:"security_opt"`
	Privileged  bool     `yaml:"privileged"`
	NetworkMode string   `yaml:"network_mode"`
	Ports       []string `yaml:"ports"`
	User        string   `yaml:"user"`
	Volumes     []string `yaml:"volumes"`
	Devices     []string `yaml:"devices"`
	Pid         string   `yaml:"pid"`
	Ipc         string   `yaml:"ipc"`
	UsernsMode  string   `yaml:"userns_mode"`
	Cgroup      string   `yaml:"cgroup"`
}

// initCaps are the only capabilities a container of the sandbox may keep:
// the init container needs them to hand the node directories to nonroot.
var initCaps = []string{"CHOWN", "DAC_OVERRIDE", "FOWNER"}

// projectImage is how every image of the sandbox is named: after the
// Compose project, so that ./sandbox down removes the images of its own
// sandbox and no other.
const projectImage = "${OBIE_SANDBOX_PROJECT:-obie-sandbox}:"

// TestComposeCannotTouchTheHost checks packaging/sandbox/compose.yaml: no
// container shares the host's network or another namespace of the host,
// mounts a host path or device, is privileged or keeps a capability
// beyond what the init container needs, every container runs with
// no-new-privileges, the consoles are published on 127.0.0.1 only, and
// every image is named after the project.
func TestComposeCannotTouchTheHost(t *testing.T) {
	var file struct {
		Services map[string]composeService `yaml:"services"`
		Volumes  map[string]any            `yaml:"volumes"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "packaging/sandbox/compose.yaml")), &file); err != nil {
		t.Fatal(err)
	}
	want := []string{"console1", "console2", "console3", "init", "node1", "node2", "node3", "stranger"}
	var names []string
	for name := range file.Services {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Fatalf("services %q, want %q", names, want)
	}
	for name, s := range file.Services {
		if s.Privileged || s.NetworkMode == "host" {
			t.Errorf("%s: privileged %v, network_mode %q; want neither privileged nor host networking", name, s.Privileged, s.NetworkMode)
		}
		if s.Pid == "host" || s.Ipc == "host" || s.UsernsMode == "host" || s.Cgroup == "host" || len(s.Devices) > 0 {
			t.Errorf("%s: pid %q, ipc %q, userns_mode %q, cgroup %q, devices %q; want no namespace and no device of the host",
				name, s.Pid, s.Ipc, s.UsernsMode, s.Cgroup, s.Devices)
		}
		for _, v := range s.Volumes {
			if _, named := file.Volumes[strings.SplitN(v, ":", 2)[0]]; !named {
				t.Errorf("%s: volume %q is no named volume of the sandbox; a host path must not be mounted", name, v)
			}
		}
		if !slices.Equal(s.CapDrop, []string{"ALL"}) {
			t.Errorf("%s: cap_drop %q, want [ALL]", name, s.CapDrop)
		}
		allowed := []string{}
		if name == "init" {
			allowed = initCaps
		}
		for _, c := range s.CapAdd {
			if !slices.Contains(allowed, c) {
				t.Errorf("%s: keeps the capability %s", name, c)
			}
		}
		if !slices.Contains(s.SecurityOpt, "no-new-privileges:true") {
			t.Errorf("%s: security_opt %q lacks no-new-privileges:true", name, s.SecurityOpt)
		}
		if !strings.HasPrefix(s.Image, projectImage) {
			t.Errorf("%s: image %q is not named %s…", name, s.Image, projectImage)
		}
		for _, p := range s.Ports {
			if !strings.HasPrefix(p, "127.0.0.1:") {
				t.Errorf("%s: port %q is published beyond 127.0.0.1", name, p)
			}
		}
		if strings.HasPrefix(name, "console") && (s.User != "65532:65532" || s.NetworkMode != "service:node"+strings.TrimPrefix(name, "console")) {
			t.Errorf("%s: user %q in network %q; want nonroot in its node's network namespace", name, s.User, s.NetworkMode)
		}
	}
	if file.Services["init"].NetworkMode != "none" {
		t.Errorf("init: network_mode %q, want none", file.Services["init"].NetworkMode)
	}
}
