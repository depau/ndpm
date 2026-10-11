// Package manager orchestrates polling GitHub releases and installing the
// resulting plugin packages into the directory Navidrome watches.
package manager

import (
	"path/filepath"
)

// Plugin represents the on-disk state of one installed plugin.
type Plugin struct {
	// Source is the "owner/repo" this plugin came from.
	Source string
	// Version is the release tag that produced the installed file.
	Version string
	// Sha256 of the installed file (hex).
	Sha256 string
	// Slug is the Navidrome plugin ID / base filename.
	Slug string
}

// Staging is where .ndp files are staged and where the final .ndp files land
// for Navidrome to pick up.
type Staging struct {
	dir string
}

// NewStaging returns a Staging rooted at dir (the plugins folder).
func NewStaging(dir string) *Staging { return &Staging{dir: dir} }

// FinalPath returns the path of the installed plugin for slug.
func (s *Staging) FinalPath(slug string) string {
	return filepath.Join(s.dir, slug+".ndp")
}

// TempPath returns the atomic-write staging path for slug. It sits in the
// same directory so the rename is atomic on the same filesystem, but uses a
// suffix Navidrome ignores (.ndp.tmp, not .ndp) so the watcher never sees it.
func (s *Staging) TempPath(slug string) string {
	return filepath.Join(s.dir, slug+".ndp.tmp")
}

// FsyncDir flushes directory metadata so the rename is durable.
func (s *Staging) FsyncDir() error { return syncDir(s.dir) }
