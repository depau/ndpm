// Package validate checks that a downloaded file is a well-formed Navidrome
// plugin package before it is put in front of the watcher.
package validate

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Manifest is the subset of a Navidrome plugin manifest we validate.
type Manifest struct {
	Name    string `json:"name"`
	Author  string `json:"author"`
	Version string `json:"version"`
}

// ErrNotPlugin signals that the file is not a usable .ndp package.
var ErrNotPlugin = errors.New("not a valid .ndp package")

// Plugin verifies the file at path is a zip containing manifest.json and
// plugin.wasm, and that the manifest has the required fields. It returns the
// manifest when valid.
func Plugin(path string) (*Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%w: not a zip archive: %v", ErrNotPlugin, err)
	}
	defer zr.Close()

	var manifest *Manifest
	var hasWasm bool
	for _, f := range zr.File {
		switch f.Name {
		case "manifest.json":
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("%w: opening manifest.json: %v", ErrNotPlugin, err)
			}
			raw, err := io.ReadAll(io.LimitReader(rc, 1<<20))
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("%w: reading manifest.json: %v", ErrNotPlugin, err)
			}
			var m Manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, fmt.Errorf("%w: manifest.json is not valid JSON: %v", ErrNotPlugin, err)
			}
			if m.Name == "" || m.Author == "" || m.Version == "" {
				return nil, fmt.Errorf("%w: manifest.json missing required field (name, author, version)", ErrNotPlugin)
			}
			manifest = &m
		case "plugin.wasm":
			// Must be non-empty; must start with the wasm magic (a cheap
			// sanity check that catches accidental text archives).
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("%w: opening plugin.wasm: %v", ErrNotPlugin, err)
			}
			buf := make([]byte, 8)
			n, _ := io.ReadFull(rc, buf)
			rc.Close()
			if n >= 4 && bytes.Equal(buf[:4], []byte{0x00, 0x61, 0x73, 0x6d}) {
				hasWasm = true
			}
		}
	}
	if manifest == nil {
		return nil, fmt.Errorf("%w: missing manifest.json", ErrNotPlugin)
	}
	if !hasWasm {
		return nil, fmt.Errorf("%w: missing plugin.wasm", ErrNotPlugin)
	}
	return manifest, nil
}
