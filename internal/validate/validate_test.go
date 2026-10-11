package validate

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// writeNDP builds a minimal valid .ndp in dir.
func writeNDP(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "test.ndp")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("manifest.json")
	w.Write([]byte(`{"name":"Test","author":"me","version":"1.0.0"}`))
	w, _ = zw.Create("plugin.wasm")
	// wat magic + one page
	w.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	zw.Close()
	f.Close()
	return p
}

func TestValidPlugin(t *testing.T) {
	m, err := Plugin(writeNDP(t, t.TempDir()))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	if m.Name != "Test" || m.Version != "1.0.0" {
		t.Errorf("manifest not parsed: %+v", m)
	}
}

func TestNotAZip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.ndp")
	os.WriteFile(p, []byte("this is not a zip"), 0o644)
	if _, err := Plugin(p); err == nil {
		t.Fatal("expected error for non-zip")
	}
}

func TestMissingWasm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.ndp")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("manifest.json")
	w.Write([]byte(`{"name":"X","author":"y","version":"1"}`))
	zw.Close()
	f.Close()
	if _, err := Plugin(p); err == nil {
		t.Fatal("expected error for missing plugin.wasm")
	}
}

func TestWasmNotBinary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.ndp")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("manifest.json")
	w.Write([]byte(`{"name":"X","author":"y","version":"1"}`))
	w, _ = zw.Create("plugin.wasm")
	w.Write([]byte("hello world, definitely not wasm but long"))
	zw.Close()
	f.Close()
	if _, err := Plugin(p); err == nil {
		t.Fatal("expected error for text plugin.wasm")
	}
}

func TestMissingManifest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.ndp")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("plugin.wasm")
	w.Write([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	zw.Close()
	f.Close()
	if _, err := Plugin(p); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}
