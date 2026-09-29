package selfcheck

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func testReport() Report {
	return Report{
		Status: Problem, Config: "/etc/obie/obie.yaml", Version: "1.2.3", User: "alice",
		Checks: []Check{
			{ID: "config", Name: "Configuration", Status: OK, Summary: "valid"},
			{ID: "peers", Name: "Peers", Status: Warning, Summary: "stand-alone", NextSteps: []string{"connect"}},
			{ID: "node", Name: "Node", Status: Problem, Summary: "not running", Details: []string{"why"}, NextSteps: []string{"start it"}},
		},
		Summary: map[Status]int{OK: 1, Warning: 1, Problem: 1},
	}
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteText(&buf, testReport()); err != nil {
		t.Fatal(err)
	}
	want := `OBIE self-check of /etc/obie/obie.yaml (obied 1.2.3, as alice)

Note: this runs as alice, not as root, so some checks cannot look everywhere and
say so. For a complete check: sudo obied self-check

OK       Configuration  valid
WARNING  Peers          stand-alone
                        Next: connect
PROBLEM  Node           not running
                        - why
                        Next: start it

Result: 1 problem, 1 warning, 1 OK. Fix the problems, then run the self-check again.
`
	if got := buf.String(); got != want {
		t.Errorf("text report =\n%s\nwant\n%s", got, want)
	}

	r := testReport()
	r.Root, r.Status, r.Checks, r.Summary = true, OK, r.Checks[:1], map[Status]int{OK: 1}
	buf.Reset()
	if err := WriteText(&buf, r); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); strings.Contains(got, "Note:") || !strings.Contains(got, "(obied 1.2.3, as root)") ||
		!strings.HasSuffix(got, "Result: 0 problems, 0 warnings, 1 OK. Everything checked is fine.\n") {
		t.Errorf("root report:\n%s", got)
	}

	r = testReport()
	r.Config = "/srv/obie.yaml"
	buf.Reset()
	if err := WriteText(&buf, r); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "For a complete check: sudo obied self-check --config /srv/obie.yaml\n") {
		t.Errorf("report of another file:\n%s", got)
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	r := testReport()
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, r) {
		t.Errorf("JSON round trip = %+v, want %+v", got, r)
	}
	for _, want := range []string{`"status": "problem"`, `"id": "node"`, `"next_steps": [`, `"summary": {`, `"warning": 1`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("JSON lacks %s:\n%s", want, buf.String())
		}
	}
}
