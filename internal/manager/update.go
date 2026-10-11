package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/depau/ndpm/config"
	"github.com/depau/ndpm/internal/github"
	"github.com/depau/ndpm/internal/validate"
)

// stateFile records what is installed for each slug, so we can skip downloads
// when nothing changed. Kept in the plugins dir next to the .ndp files.
const stateFile = ".state.json"

// ErrUpToDate reports that the current release is already installed.
var ErrUpToDate = errors.New("release already installed")

// Updater checks releases and installs them.
type Updater struct {
	gh    *github.Client
	stage *Staging
}

// NewUpdater wires an Updater to a GitHub client and a staging dir.
func NewUpdater(gh *github.Client, stage *Staging) *Updater {
	return &Updater{gh: gh, stage: stage}
}

// Install reconciles one source with its configured release. It returns the
// installed Plugin, or ErrUpToDate wrapped when nothing changed.
func (u *Updater) Install(src config.Source) (*Plugin, error) {
	slug := src.SourceSlug()

	release, err := u.gh.LatestRelease(owner(src), name(src), src.IncludePrereleases)
	if err != nil {
		return nil, err
	}
	asset, err := pickAsset(release, src.Filename)
	if err != nil {
		return nil, err
	}

	installed, err := loadState(u.stage.dir, slug)
	if err == nil && installed.Version == release.TagName {
		return installed, ErrUpToDate
	}

	tmp, err := u.downloadInto(asset.BrowserURL, slug)
	if err != nil {
		return nil, err
	}

	// Trust boundary: never hand a corrupt or non-plugin file to Navidrome.
	if _, err := validate.Plugin(tmp); err != nil {
		os.Remove(tmp)
		return nil, err
	}

	final := u.stage.FinalPath(slug)
	if err := os.Rename(tmp, final); err != nil { // atomic on same filesystem
		os.Remove(tmp)
		return nil, fmt.Errorf("installing %s: %w", slug, err)
	}
	if err := u.stage.FsyncDir(); err != nil {
		return nil, err
	}

	sum, err := fileSHA256(final)
	if err != nil {
		return nil, err
	}
	installed = &Plugin{Source: src.Repo, Version: release.TagName, Sha256: sum, Slug: slug}
	if err := saveState(u.stage.dir, slug, installed); err != nil {
		return nil, err
	}
	return installed, nil
}

// downloadInto writes the asset to the same directory as the final file but
// under a .ndp.tmp suffix, which Navidrome ignores. Downloading in place
// guarantees the final rename never crosses a filesystem boundary.
func (u *Updater) downloadInto(url, slug string) (string, error) {
	tmp := u.stage.TempPath(slug)
	if err := u.gh.DownloadURL(url, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// pickAsset selects the asset to install: the one named by filename, or the
// alphabetically-first .ndp asset.
func pickAsset(r *github.Release, filename string) (*github.Asset, error) {
	if filename != "" {
		for i := range r.Assets {
			if r.Assets[i].Name == filename {
				return &r.Assets[i], nil
			}
		}
		return nil, fmt.Errorf("release %s has no asset %q", r.TagName, filename)
	}
	var first *github.Asset
	for i := range r.Assets {
		name := r.Assets[i].Name
		if strings.HasSuffix(name, ".ndp") && (first == nil || name < first.Name) {
			first = &r.Assets[i]
		}
	}
	if first == nil {
		return nil, fmt.Errorf("release %s has no .ndp asset", r.TagName)
	}
	return first, nil
}

// owner and name split "owner/repo"; the config guarantees two parts.
func owner(s config.Source) string { return strings.SplitN(s.Repo, "/", 2)[0] }
func name(s config.Source) string  { return strings.SplitN(s.Repo, "/", 2)[1] }

type pluginState struct {
	Source  string `json:"source"`
	Version string `json:"version"`
	Sha256  string `json:"sha256"`
	Slug    string `json:"slug"`
}

func loadState(dir, slug string) (*Plugin, error) {
	raw, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		return nil, err
	}
	var all map[string]pluginState
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	s, ok := all[slug]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &Plugin{Source: s.Source, Version: s.Version, Sha256: s.Sha256, Slug: s.Slug}, nil
}

func saveState(dir, slug string, p *Plugin) error {
	all := map[string]pluginState{}
	if raw, err := os.ReadFile(filepath.Join(dir, stateFile)); err == nil {
		_ = json.Unmarshal(raw, &all)
	}
	all[slug] = pluginState{Source: p.Source, Version: p.Version, Sha256: p.Sha256, Slug: p.Slug}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, stateFile))
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
