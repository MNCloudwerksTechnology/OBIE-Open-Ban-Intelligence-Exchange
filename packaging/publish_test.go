package packaging_test

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeForge is a minimal release API of GitHub or Gitea: one repository,
// releases looked up by tag, created, and given assets.
type fakeForge struct {
	gitea bool

	mu sync.Mutex
	// lookupStatus, if set, is the status of every release lookup.
	lookupStatus int
	url          string
	release      map[string]any // nil until created
	created      map[string]any // body of the create request
	uploaded     map[string]string
}

func newFakeForge(t *testing.T, gitea bool) *fakeForge {
	f := &fakeForge{gitea: gitea, uploaded: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.mu.Lock() // the server may already be serving
	f.url = srv.URL
	f.mu.Unlock()
	return f
}

func (f *fakeForge) apiPrefix() string {
	if f.gitea {
		return "/api/v1"
	}
	return "/api"
}

func (f *fakeForge) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "token secret" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo := f.apiPrefix() + "/repos/acme/obie/releases"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == repo+"/tags/v1.2.3":
		if f.lookupStatus != 0 {
			http.Error(w, "lookup failed", f.lookupStatus)
			return
		}
		if f.release == nil {
			http.NotFound(w, r)
			return
		}
		f.writeRelease(w)
	case r.Method == http.MethodPost && r.URL.Path == repo:
		if err := json.NewDecoder(r.Body).Decode(&f.created); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.release = map[string]any{"id": 42, "upload_url": f.url + "/uploads/42/assets{?name,label}"}
		w.WriteHeader(http.StatusCreated)
		f.writeRelease(w)
	case f.gitea && r.Method == http.MethodPost && r.URL.Path == repo+"/42/assets":
		f.uploadMultipart(w, r)
	case !f.gitea && r.Method == http.MethodPost && r.URL.Path == "/uploads/42/assets":
		data, _ := io.ReadAll(r.Body)
		f.uploaded[r.URL.Query().Get("name")] = string(data)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusTeapot)
	}
}

func (f *fakeForge) uploadMultipart(w http.ResponseWriter, r *http.Request) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
	if err != nil || part.FormName() != "attachment" {
		http.Error(w, fmt.Sprintf("no attachment: %v", err), http.StatusBadRequest)
		return
	}
	data, _ := io.ReadAll(part)
	f.uploaded[r.URL.Query().Get("name")] = string(data)
}

// state returns copies of the create request and the uploaded assets; the
// handlers run on other goroutines.
func (f *fakeForge) state() (created map[string]any, uploaded map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.created), maps.Clone(f.uploaded)
}

func (f *fakeForge) writeRelease(w http.ResponseWriter) {
	assets := []map[string]string{}
	for name := range f.uploaded {
		assets = append(assets, map[string]string{"name": name})
	}
	f.release["assets"] = assets
	_ = json.NewEncoder(w).Encode(f.release)
}

// publish runs publish-release.sh for tag v1.2.3 against forge.
func publish(t *testing.T, forge *fakeForge, dir string) string {
	t.Helper()
	out, err := runPublish(forge, dir)
	if err != nil {
		t.Fatalf("publish-release.sh: %v\n%s", err, out)
	}
	return out
}

// runPublish runs publish-release.sh for tag v1.2.3 against forge.
func runPublish(forge *fakeForge, dir string) (string, error) {
	env := []string{"TOKEN=secret", "SERVER_URL=" + forge.url, "REPO=acme/obie", "TAG=v1.2.3",
		"GITHUB_API_URL=" + forge.url + "/api", "GITEA_ACTIONS="}
	if forge.gitea {
		env = append(env, "GITEA_ACTIONS=true")
	}
	cmd := exec.Command("/bin/sh", "publish-release.sh", dir) // #nosec G204 -- test script.
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// requireTools skips the test unless curl and jq work.
func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"curl", "jq"} {
		if err := exec.Command(tool, "--version").Run(); err != nil { // #nosec G204 -- fixed tool names.
			t.Skipf("%s not usable: %v", tool, err)
		}
	}
}

func TestPublishReleaseStopsOnLookupFailure(t *testing.T) {
	requireTools(t)
	forge := newFakeForge(t, false)
	forge.mu.Lock()
	forge.lookupStatus = http.StatusInternalServerError
	forge.mu.Unlock()
	out, err := runPublish(forge, t.TempDir())
	if err == nil || !strings.Contains(out, "failed with HTTP 500") {
		t.Errorf("publish-release.sh = %v, want an HTTP 500 error:\n%s", err, out)
	}
	if created, _ := forge.state(); created != nil {
		t.Errorf("release created although the lookup failed: %v", created)
	}
}

func TestPublishRelease(t *testing.T) {
	requireTools(t)
	files := []string{"obie-1.2.3-linux-amd64.tar.gz", "obie-1.2.3-linux-amd64.obied.cdx.json", "SHA256SUMS"}
	dir := t.TempDir()
	for _, name := range files {
		writeFile(t, filepath.Join(dir, name), "content of "+name, 0o644)
	}
	for name, gitea := range map[string]bool{"github": false, "gitea": true} {
		t.Run(name, func(t *testing.T) {
			forge := newFakeForge(t, gitea)
			out := publish(t, forge, dir)
			if !strings.Contains(out, "created release v1.2.3") {
				t.Errorf("no release created:\n%s", out)
			}
			created, uploaded := forge.state()
			if created["tag_name"] != "v1.2.3" || created["prerelease"] != false {
				t.Errorf("create request = %v", created)
			}
			for _, name := range files {
				if got := uploaded[name]; got != "content of "+name {
					t.Errorf("asset %s = %q", name, got)
				}
			}

			// A re-run reuses the release and uploads nothing again.
			out = publish(t, forge, dir)
			if strings.Contains(out, "created release") || strings.Contains(out, "attached ") {
				t.Errorf("re-run created or uploaded again:\n%s", out)
			}
			if _, uploaded := forge.state(); len(uploaded) != len(files) {
				t.Errorf("assets = %v", uploaded)
			}
		})
	}
}
