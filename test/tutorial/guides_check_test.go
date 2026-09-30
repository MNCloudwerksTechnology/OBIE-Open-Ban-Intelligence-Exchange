//go:build tutorial && linux

package tutorial

import (
	"flag"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// guidesName names the guides' check, its server and its image, so that
// it runs next to the tutorial's check.
const guidesName = "obie-guides-check"

var keep = flag.Bool("guides.keep", false, "leave the server of the last guide and its image, to look at it (docker exec -it "+guidesName+" bash); the next run removes the server")

// scenarios are what the reader of a guide brings along beyond the
// tutorial's steps, played by the check before the guide runs. The guides'
// index says what they stand for.
var scenarios = map[string]func(c *check){
	// The friend's node reports an address before the reader connects to
	// it, and sends the verdict once connected.
	"connect-a-peer.md":               func(c *check) { c.peerReports(friendReports) },
	"stop-trusting-a-peer.md":         func(c *check) { c.peerReports(friendReports) },
	"review-what-would-be-blocked.md": func(c *check) { c.peerReports(friendReports) },
	// A customer mistypes their password five times, and the node blocks
	// them.
	"unblock-an-address.md": func(c *check) {
		c.failLogins(customer)
		c.waitForBlock(customer)
	},
	// The reader reports the wrong address.
	"withdraw-a-verdict.md": func(c *check) {
		c.asRoot("obiectl report --protocol ssh --reason password_bruteforce " + mistake)
	},
}

const (
	// customer is the address of a customer that Fail2Ban bans by mistake.
	customer = "85.10.4.12"
	// mistake is the address the reader reports by mistake.
	mistake = "85.10.0.19"
)

// friendReports is the address the friend's node reports.
const friendReports = "85.10.0.66"

// TestGuides runs every how-to guide of documentation/guides (ADR 0031),
// each on a server of its own: the tutorial's commands up to the step the
// guide starts from, what the reader brings along, and then every command
// of the guide in the order Steps, Check that it worked, Undo, the output
// compared with what the page shows and every console page checked for
// what the page says it shows.
func TestGuides(t *testing.T) {
	tutorial := parsePage(readRepoFile(t, tutorialPath))
	guides := readGuides(t)
	for _, g := range guides {
		if problems := g.problemsOf(); len(problems) > 0 {
			t.Fatalf("%s/%s is not in the form the check reads: %q", guidesDir, g.file, problems)
		}
	}
	if *cyclonedx == "" {
		t.Fatal("-tutorial.cyclonedx is not set; run the check with make guides-check")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	c := &check{t: t, root: root, name: guidesName, version: releaseOf(t, tutorial), host: guidesName}
	t.Cleanup(func() {
		if !*keep {
			c.remove()
		}
	})
	c.removeContainers() // what an interrupted run left
	c.release = t.TempDir()
	for _, v := range versionsOf(tutorial, guides) {
		c.buildRelease(v)
	}
	c.buildHostImage()
	for _, g := range guides {
		t.Run(strings.TrimSuffix(g.file, ".md"), func(t *testing.T) {
			gc := &check{t: t, root: c.root, name: c.name, version: c.version, release: c.release, host: c.host}
			gc.removeContainers()
			if !*keep {
				t.Cleanup(gc.removeContainers)
			}
			gc.runGuide(tutorial, g)
		})
	}
	if *keep {
		t.Logf("the server of the last guide still runs: docker exec -it %s bash; docker rm -f %s removes it", c.host, c.host)
	}
}

// versionsOf returns the versions of the releases the tutorial and the
// guides download, the tutorial's first.
func versionsOf(tutorial page, guides []guide) []string {
	versions := []string{}
	add := func(p page) {
		for _, s := range p.steps {
			for _, cmd := range s.commands {
				for _, m := range pageRelease.FindAllStringSubmatch(cmd, -1) {
					if !slices.Contains(versions, m[1]) {
						versions = append(versions, m[1])
					}
				}
			}
		}
	}
	add(tutorial)
	for _, g := range guides {
		add(g.page)
	}
	return versions
}

// runGuide runs a guide on a server of its own.
func (c *check) runGuide(tutorial page, g guide) {
	c.startServer()
	c.startPeer()
	c.runTutorialUpTo(tutorial, g.prerequisite)
	if s := scenarios[g.file]; s != nil {
		s(c)
	}
	if len(g.hints) > 0 {
		c.switchConsoleOn()
	}
	for _, title := range runOrder {
		c.runSection(g, title)
	}
}

// runTutorialUpTo runs the tutorial's steps up to the end of the section
// last, with the attack before the step that waits for it.
func (c *check) runTutorialUpTo(p page, last string) {
	n := slices.Index(story, last)
	if n < 0 {
		c.t.Fatalf("%q is no step of the tutorial", last)
	}
	attacked := false
	for _, s := range p.steps {
		if i := slices.Index(story, s.section); i < 0 || i > n {
			continue
		}
		s.file = tutorialPath
		if s.section == attackSection && !attacked {
			c.attack()
			attacked = true
		}
		for _, cmd := range s.commands {
			c.run(s, cmd)
		}
	}
}

// runSection runs the commands of a guide's section in order, and checks
// each console hint once the section's steps before it have run.
func (c *check) runSection(g guide, title string) {
	var steps []int // the indexes of the section's steps in g.steps
	for i, s := range g.steps {
		if s.section == title {
			steps = append(steps, i)
		}
	}
	hints := func(after int) {
		for _, h := range g.hints {
			if h.section == title && h.after == after {
				c.checkConsole(g, h)
			}
		}
	}
	if len(steps) == 0 {
		for _, h := range g.hints {
			if h.section == title {
				c.checkConsole(g, h)
			}
		}
		return
	}
	hints(steps[0])
	for _, i := range steps {
		for _, cmd := range g.steps[i].commands {
			c.run(g.steps[i], cmd)
		}
		hints(i + 1)
	}
}

// switchConsoleOn switches the web console on, as its page says, for a
// guide that shows it.
func (c *check) switchConsoleOn() {
	c.asRoot(`printf '\nconsole:\n  enabled: true\n' >>/etc/obie/obie.yaml
obied --config /etc/obie/obie.yaml --check-config
systemctl reload obied`)
}

// consoleScript prints a page of the web console, signed in as root with
// the token of obiectl console, after signing in again if the session
// ended.
const consoleScript = `set -eu
base=` + consoleAddress + ` jar=/root/guides-check.cookies
get() { curl -sS -b "$jar" -c "$jar" -o /root/guides-check.page -w '%{http_code} %{redirect_url}' "$base$1"; }
res=$(get "$1")
case "$res" in
*/login*)
	token=$(obiectl console --json | python3 -c 'import json, sys; print(json.load(sys.stdin)["token"])')
	curl -sS -o /dev/null -b "$jar" -c "$jar" -H "Origin: $base" -H 'Sec-Fetch-Site: same-origin' \
		--data-urlencode "token=$token" "$base/login"
	res=$(get "$1")
	;;
esac
case "$res" in
200*) cat /root/guides-check.page ;;
*) echo "the console answered $res" >&2; exit 1 ;;
esac`

// checkConsole waits until the console page of a hint shows every phrase
// the hint sets in bold.
func (c *check) checkConsole(g guide, h consoleHint) {
	c.t.Helper()
	c.t.Logf("%s/%s:%d: the console page %s", guidesDir, g.file, h.line, h.path)
	deadline := time.Now().Add(settle)
	for {
		out, err := c.command(stepBound, "docker", "exec", c.host, "bash", "-c", consoleScript, "console", h.path)
		text := pageText(out)
		missing := slices.DeleteFunc(slices.Clone(h.phrases), func(p string) bool { return strings.Contains(text, p) })
		if err == nil && len(missing) == 0 {
			return
		}
		if time.Now().After(deadline) {
			c.diagnose()
			c.t.Errorf("%s/%s:%d: the console page %s: %v; it does not show %q:\n%s", guidesDir, g.file, h.line, h.path, err, missing, text)
			return
		}
		time.Sleep(time.Second)
	}
}

var (
	scriptOrStyle = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	htmlTag       = regexp.MustCompile(`<[^>]*>`)
)

// pageText returns the text of an HTML page, whitespace collapsed.
func pageText(page string) string {
	page = scriptOrStyle.ReplaceAllString(page, " ")
	page = htmlTag.ReplaceAllString(page, " ")
	return strings.Join(strings.Fields(html.UnescapeString(page)), " ")
}

// peerReports lets the peer's node publish a verdict on the address, as
// the friend's Fail2Ban would.
func (c *check) peerReports(address string) {
	c.asRoot(fmt.Sprintf("ip netns exec peer /srv/peer/bin/obiectl --socket /run/obie-peer/obie.sock report --protocol ssh --reason bruteforce --events 5 %s", address))
}

// waitForBlock waits until the node blocks the address, or would in
// observe mode.
func (c *check) waitForBlock(address string) {
	c.until("sudo obiectl explain "+address, "the node does not block "+address,
		func(out string) bool { return strings.Contains(out, "\nDecision:              block until ") })
}
