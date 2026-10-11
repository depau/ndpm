package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newThrottleTest returns a client against a server that always answers the
// releases endpoint with the given status + headers.
func newThrottleTest(t *testing.T, status int, headers map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			fmt.Fprint(w, `[{"tag_name":"v1.0.0","assets":[]}]`)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	c.SetBase(srv.URL)
	return c
}

func TestThrottleViaRetryAfter(t *testing.T) {
	c := newThrottleTest(t, http.StatusTooManyRequests, map[string]string{
		"Retry-After": "5",
	})
	if _, err := c.LatestRelease("o", "r", true); err == nil {
		t.Fatal("expected error on 429")
	}
	until := c.ThrottledUntil()
	if until.IsZero() {
		t.Fatal("expected throttleUntil to be set from Retry-After")
	}
	if d := time.Until(until); d < 4*time.Second || d > 6*time.Second {
		t.Errorf("reset in %s, want ~5s", d.Round(time.Second))
	}
}

func TestThrottleViaResetHeader(t *testing.T) {
	reset := time.Now().Add(42 * time.Second).Unix()
	c := newThrottleTest(t, http.StatusForbidden, map[string]string{
		"X-RateLimit-Remaining": "0",
		"X-RateLimit-Reset":     fmt.Sprint(reset),
	})
	if _, err := c.LatestRelease("o", "r", true); err == nil {
		t.Fatal("expected error on forbidden")
	}
	until := c.ThrottledUntil()
	if until.IsZero() {
		t.Fatal("expected throttleUntil from X-RateLimit-Reset")
	}
	if d := time.Until(until); d < 41*time.Second || d > 43*time.Second {
		t.Errorf("reset in %s, want ~42s", d.Round(time.Second))
	}
}

func TestNoThrottleOnSuccess(t *testing.T) {
	c := newThrottleTest(t, http.StatusOK, map[string]string{})
	if _, err := c.LatestRelease("o", "r", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.ThrottledUntil().IsZero() {
		t.Errorf("throttleUntil set on clean success: %s", c.ThrottledUntil())
	}
}

func TestThrottleIgnoresUnrelated403(t *testing.T) {
	// A 403 that is NOT rate-limit (e.g. auth problem) must not set throttle.
	c := newThrottleTest(t, http.StatusForbidden, map[string]string{})
	if _, err := c.LatestRelease("o", "r", true); err == nil {
		t.Fatal("expected error on forbidden")
	}
	if !c.ThrottledUntil().IsZero() {
		t.Errorf("throttleUntil set on non-rate-limit 403")
	}
}

// Sanity that the JSON model still decodes the real GitHub payload shape.
func TestReleaseDecode(t *testing.T) {
	var r []Release
	err := json.Unmarshal([]byte(`[{"tag_name":"v1","html_url":"http://x","prerelease":true,"assets":[{"name":"a.ndp","browser_download_url":"http://x/a.ndp","size":12}]}]`), &r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 || r[0].TagName != "v1" || !r[0].Prerelease || len(r[0].Assets) != 1 {
		t.Errorf("decoded: %+v", r)
	}
}
