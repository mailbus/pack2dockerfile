package imageinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSBOM(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRuntimeVersionCDX(t *testing.T) {
	dir := t.TempDir()
	writeSBOM(t, dir, "layers/sbom/launch/paketo-buildpacks_node-engine/node/sbom.cdx.json",
		`{"components":[{"name":"Node Engine","version":"24.22.1"}]}`)
	got, err := findRuntimeVersion(dir, "Node Engine")
	if err != nil {
		t.Fatalf("findRuntimeVersion: %v", err)
	}
	if got != "24.22.1" {
		t.Errorf("version = %q, want 24.22.1", got)
	}
}

func TestFindCPythonVersion(t *testing.T) {
	dir := t.TempDir()
	writeSBOM(t, dir, "layers/sbom/launch/paketo-buildpacks_cpython/cpython/sbom.cdx.json",
		`{"components":[{"name":"CPython","version":"3.11.18"}]}`)
	got, err := findRuntimeVersion(dir, "CPython", "Python")
	if err != nil {
		t.Fatalf("findRuntimeVersion: %v", err)
	}
	if got != "3.11.18" {
		t.Errorf("version = %q, want 3.11.18", got)
	}
}

func TestFindRuntimeVersionLegacy(t *testing.T) {
	dir := t.TempDir()
	writeSBOM(t, dir, "layers/sbom/launch/sbom.legacy.json",
		`[{"name":"Node Engine","metadata":{"version":"20.11.0"}}]`)
	got, err := findRuntimeVersion(dir, "Node Engine")
	if err != nil {
		t.Fatalf("findRuntimeVersion: %v", err)
	}
	if got != "20.11.0" {
		t.Errorf("version = %q, want 20.11.0", got)
	}
}

func TestFindRuntimeVersionMissing(t *testing.T) {
	_, err := findRuntimeVersion(t.TempDir(), "Node Engine")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "BP_DISABLE_SBOM") {
		t.Errorf("error should mention BP_DISABLE_SBOM, got %v", err)
	}
}
