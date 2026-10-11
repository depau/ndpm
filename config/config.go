// Package config loads and validates the manager's YAML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root of the YAML configuration file.
type Config struct {
	// PluginsDir is the directory where .ndp files are staged, and where the
	// final <slug>.ndp files land. Navidrome watches this exact directory.
	PluginsDir string `yaml:"plugins_dir"`

	// Interval is how often sources are polled, as a Go duration string
	// (e.g. "30m", "1h"). Defaults to 1h when empty.
	Interval string `yaml:"interval"`

	// GitHubToken optionally authenticates API requests to raise the rate
	// limit (and to fetch from private repositories). Usually set via the
	// GITHUB_TOKEN environment variable instead.
	GitHubToken string `yaml:"github_token"`

	// Sources is the list of plugin repositories to track.
	Sources []Source `yaml:"sources"`
}

// Source is one plugin repository to track. A minimal entry is:
//
//   - repo: owner/repo
type Source struct {
	// Repo is the GitHub repository in "owner/name" form.
	Repo string `yaml:"repo"`

	// IncludePrereleases makes the manager prefer the latest release even
	// when it is marked as a prerelease or draft.
	IncludePrereleases bool `yaml:"include_prereleases"`

	// Filename optionally pins the asset within the release to install.
	// When empty, the first asset whose filename ends in ".ndp" (sorted
	// alphabetically) is chosen.
	Filename string `yaml:"filename"`

	// Slug is the base name the plugin is stored under, also its Navidrome
	// plugin ID. Defaults to the "owner" part of the repo split at a hyphen
	// (e.g. "owner/nd-rating-sync" -> "nd-rating-sync"), else the repo name.
	Slug string `yaml:"slug"`

	// SubsPath, only applied to source builds (Draft=false), is the
	// subdirectory inside the source archive that holds the .ndp file.
	SubsPath string `yaml:"subs_path"`
}

// Load reads the YAML file at path and validates it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg := &Config{}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks the config for obvious mistakes.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.PluginsDir) == "" {
		return errors.New("missing plugins_dir")
	}
	if len(c.Sources) == 0 {
		return errors.New("no sources configured")
	}
	for i, s := range c.Sources {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("source %d (%s): %w", i, s.Repo, err)
		}
	}
	return nil
}

// SourceSlug returns the stable storage name for a source: the explicit Slug,
// else the repo name. Some repos publish assets under a name that differs
// from the repo name; set slug: to match your plugin's ID precisely.
func (s Source) SourceSlug() string {
	if strings.TrimSpace(s.Slug) != "" {
		return s.Slug
	}
	return s.Repo[strings.IndexByte(s.Repo, '/')+1:]
}

// Validate checks one source.
func (s Source) Validate() error {
	if strings.TrimSpace(s.Repo) == "" {
		return errors.New("missing repo (expected owner/name)")
	}
	parts := strings.Split(s.Repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("repo %q must be in owner/name form", s.Repo)
	}
	return nil
}
