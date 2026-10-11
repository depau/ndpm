// Package github is a minimal GitHub REST client, using only the endpoints
// the manager needs (releases).
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Client talks to the GitHub REST API.
type Client struct {
	http *http.Client
	// base handles API URL resolution; overridden by tests.
	base  string
	token string

	// throttleUntil is set when GitHub reports the API or a resource is
	// rate-limited. The poll loop reads it to back off instead of hammering.
	throttleUntil time.Time
}

// New creates a Client, honoring the GITHUB_TOKEN environment variable for
// private repositories and a higher rate limit.
func New() (*Client, error) {
	return &Client{
		http:  &http.Client{Timeout: 30 * time.Second},
		base:  "https://api.github.com",
		token: os.Getenv("GITHUB_TOKEN"),
	}, nil
}

// SetToken overrides the client token (usually from config).
func (c *Client) SetToken(t string) { c.token = t }

// SetBase overrides the API base URL (used by tests).
func (c *Client) SetBase(base string) { c.base = base }

// ThrottledUntil returns the time the current rate-limit window resets, if
// GitHub throttled us on the last request. Zero time when not throttled.
func (c *Client) ThrottledUntil() time.Time { return c.throttleUntil }

// Release is a GitHub release with the fields we care about.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	// Draft and Prerelease are both hidden unless we opt them in; the caller
	// filters on Prerelease to honor include_prereleases.
	Prerelease bool    `json:"prerelease,omitempty"`
	Draft      bool    `json:"draft,omitempty"`
	Assets     []Asset `json:"assets"`
}

// Asset is a single release asset.
type Asset struct {
	Name       string `json:"name"`
	BrowserURL string `json:"browser_download_url"`
	Size       int64  `json:"size"`
}

// LatestRelease returns the newest release for owner/repo. When
// includePrereleases is false, prereleases and drafts are skipped.
func (c *Client) LatestRelease(owner, repo string, includePrereleases bool) (*Release, error) {
	releases, err := c.listReleases(owner, repo)
	if err != nil {
		return nil, err
	}
	for _, r := range releases {
		if r.Draft {
			continue
		}
		if !includePrereleases && r.Prerelease {
			continue
		}
		return &r, nil
	}
	return nil, fmt.Errorf("no acceptable release for %s/%s", owner, repo)
}

// listReleases returns all releases, newest first.
func (c *Client) listReleases(owner, repo string) ([]Release, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=10", c.base, url.PathEscape(owner), url.PathEscape(repo))
	body, err := c.get(u)
	if err != nil {
		return nil, err
	}
	var out []Release
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding releases for %s/%s: %w", owner, repo, err)
	}
	return out, nil
}

// DownloadURL streams the asset at the given URL to path (temp file). The
// token is always sent, which makes private-release assets and higher rate
// weighpoints work (asset downloads without auth fall under the tiny
// unauthenticated blob limit).
func (c *Client) DownloadURL(assetURL, path string) error {
	req, err := http.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.recordThrottle(resp)
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("downloading %s: %s", assetURL, resp.Status)
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return out.Sync()
}

// get performs a GET under the API root, adding the token when present.
func (c *Client) get(u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		c.recordThrottle(resp)
		// Clear guidance for the by-far most common throttle case.
		if resp.StatusCode == http.StatusForbidden && c.token == "" {
			return nil, fmt.Errorf("GitHub API rate limit exceeded (reset %s); set GITHUB_TOKEN or wait", c.throttleUntil.Format(time.RFC3339))
		}
		return nil, fmt.Errorf("GitHub API %s: %s: %s", resp.Status, u, strings.TrimSpace(string(body)))
	}
	// Even on success, record when the window resets so a near-empty budget
	// backs the poll loop off instead of bursting into a 403.
	c.recordThrottle(resp)
	return io.ReadAll(resp.Body)
}

// recordThrottle inspects a response for rate-limit pressure and, when GitHub
// is telling us to back off (403/429 or X-RateLimit-Remaining: 0/low), records
// when the window resets. Safe to call on every response.
func (c *Client) recordThrottle(resp *http.Response) {
	if resp == nil {
		return
	}
	retry := resp.Header.Get("Retry-After")
	if retry != "" {
		if secs, err := strconv.Atoi(retry); err == nil && secs > 0 {
			if until := time.Now().Add(time.Duration(secs) * time.Second); until.After(c.throttleUntil) {
				c.throttleUntil = until
			}
			return
		}
	}
	// A 429 is always a throttling signal; 403 only when we're truly out
	// (GitHub also 403s on auth errors). Without a token, remaining:0 is the
	// unauthenticated limit.
	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
		if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
			if unix, err := strconv.ParseInt(reset, 10, 64); err == nil && unix > 0 {
				until := time.Unix(unix, 0)
				if until.After(c.throttleUntil) {
					c.throttleUntil = until
				}
			}
		}
	}
}
