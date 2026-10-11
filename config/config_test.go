package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeTemp(t, `
plugins_dir: /data/plugins
interval: 30m
sources:
  - repo: owner/nd-rating-sync
    include_prereleases: true
  - repo: owner/heddohon-navidrome-plugin
    slug: heddohon
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PluginsDir != "/data/plugins" || cfg.Interval != "30m" {
		t.Errorf("plumb: %+v", cfg)
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(cfg.Sources))
	}
	if !cfg.Sources[0].IncludePrereleases {
		t.Errorf("prerelease flag lost")
	}
	if got := cfg.Sources[1].SourceSlug(); got != "heddohon" {
		t.Errorf("slug = %q, want heddohon", got)
	}
}

func TestLoadMissingPluginsDir(t *testing.T) {
	if _, err := Load(writeTemp(t, "sources: []")); err == nil {
		t.Fatal("expected error for missing plugins_dir")
	}
}

func TestLoadUnknownField(t *testing.T) {
	if _, err := Load(writeTemp(t, "plugins_dir: /x\nnope: 1\nsources:\n  - repo: a/b\n")); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestLoadBadRepo(t *testing.T) {
	if _, err := Load(writeTemp(t, "plugins_dir: /x\nsources:\n  - repo: not-a-repo\n")); err == nil {
		t.Fatal("expected error for malformed repo")
	}
}

func TestSourceSlugDefault(t *testing.T) {
	got := Source{Repo: "owner/nd-rating-sync"}.SourceSlug()
	if got != "nd-rating-sync" {
		t.Errorf("default slug = %q, want nd-rating-sync", got)
	}
}
