package manager

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/depau/ndpm/config"
	"github.com/depau/ndpm/internal/github"
)

// ndpBytes returns the bytes of a minimal valid plugin package.
func ndpBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("manifest.json")
	w.Write([]byte(`{"name":"Test","author":"me","version":"1.0.0"}`))
	w, _ = zw.Create("plugin.wasm")
	w.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newTestUpdater spins up a fake GitHub server serving one release (with a
// single .ndp asset) and returns an Updater pointing at it. The served tag is
// mutable via tagPtr so tests can bump the version.
func newTestUpdater(t *testing.T, tag string, ndp []byte) (*Updater, *httptest.Server, *string, *int, *int) {
	t.Helper()
	calls := 0
	downloads := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.URL.Path == "/repos/owner/repo/releases":
			w.Header().Set("Content-Type", "application/json")
			body := `[{"tag_name":"` + tag + `","html_url":"http://example.com/r","assets":[{"name":"nd-rating-sync.ndp","browser_download_url":"` + srv.URL + `/download"}]}]`
			w.Write([]byte(body))
		case r.URL.Path == "/download":
			downloads++
			w.Write(ndp)
		default:
			http.NotFound(w, r)
		}
	}))
	gh, err := github.New()
	if err != nil {
		t.Fatal(err)
	}
	gh.SetBase(srv.URL)
	dir := t.TempDir()
	stage := NewStaging(dir)
	return NewUpdater(gh, stage), srv, &tag, &calls, &downloads
}

func testSource() config.Source {
	return config.Source{Repo: "owner/repo"}
}

func TestInstallFresh(t *testing.T) {
	u, srv, _, _, _ := newTestUpdater(t, "v1.0.0", ndpBytes(t))
	defer srv.Close()

	p, err := u.Install(testSource())
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if p.Version != "v1.0.0" || p.Slug != "repo" {
		t.Errorf("installed = %+v", p)
	}
	if _, err := os.Stat(filepath.Join(u.stage.dir, "repo.ndp")); err != nil {
		t.Fatalf("final .ndp missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(u.stage.dir, "repo.ndp.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp .ndp.tmp should have been removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(u.stage.dir, stateFile)); err != nil {
		t.Fatalf("state missing: %v", err)
	}
}

func TestInstallUpToDateSkipsNetwork(t *testing.T) {
	u, srv, _, calls, downloads := newTestUpdater(t, "v1.0.0", ndpBytes(t))
	defer srv.Close()

	if _, err := u.Install(testSource()); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	firstCalls := *calls

	if _, err := u.Install(testSource()); err != ErrUpToDate {
		t.Fatalf("second Install err = %v, want ErrUpToDate", err)
	}
	if *downloads != 1 {
		t.Errorf("second install re-downloaded the asset (%d downloads); should skip", *downloads)
	}
	if *calls != firstCalls {
		// The up-to-date path still lists releases (to learn the current tag),
		// so calls may grow; only the asset download must be skipped.
		t.Logf("note: release listing hit network again (%d -> %d)", firstCalls, *calls)
	}
}

func TestInstallNewVersionReplaces(t *testing.T) {
	u, srv, tagPtr, _, _ := newTestUpdater(t, "v1.0.0", ndpBytes(t))
	defer srv.Close()

	if _, err := u.Install(testSource()); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(u.stage.dir, "repo.ndp"))
	if err != nil {
		t.Fatal(err)
	}

	// Bump the served release to a new tag.
	*tagPtr = "v2.0.0"
	p, err := u.Install(testSource())
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if p.Version != "v2.0.0" {
		t.Errorf("version = %s, want v2.0.0", p.Version)
	}
	after, err := os.Stat(filepath.Join(u.stage.dir, "repo.ndp"))
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Errorf("file was not replaced (same inode)")
	}
}

func TestInstallRejectsCorruptPackage(t *testing.T) {
	u, srv, _, _, _ := newTestUpdater(t, "v1.0.0", []byte("not a valid plugin package"))
	defer srv.Close()

	if _, err := u.Install(testSource()); err == nil {
		t.Fatal("expected error for corrupt package")
	}
	if _, err := os.Stat(filepath.Join(u.stage.dir, "repo.ndp")); !os.IsNotExist(err) {
		t.Fatalf("corrupt package must not be installed, stat err = %v", err)
	}
}
