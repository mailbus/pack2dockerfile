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

func TestFindNodeEngineVersionCDX(t *testing.T) {
	dir := t.TempDir()
	writeSBOM(t, dir, "layers/sbom/launch/paketo-buildpacks_node-engine/node/sbom.cdx.json",
		`{"components":[{"name":"Node Engine","version":"24.22.1"}]}`)
	got, err := findNodeEngineVersion(dir)
	if err != nil {
		t.Fatalf("findNodeEngineVersion: %v", err)
	}
	if got != "24.22.1" {
		t.Errorf("version = %q, want 24.22.1", got)
	}
}

func TestFindNodeEngineVersionLegacy(t *testing.T) {
	dir := t.TempDir()
	writeSBOM(t, dir, "layers/sbom/launch/sbom.legacy.json",
		`[{"name":"Node Engine","metadata":{"version":"20.11.0"}}]`)
	got, err := findNodeEngineVersion(dir)
	if err != nil {
		t.Fatalf("findNodeEngineVersion: %v", err)
	}
	if got != "20.11.0" {
		t.Errorf("version = %q, want 20.11.0", got)
	}
}

func TestFindNodeEngineVersionMissing(t *testing.T) {
	_, err := findNodeEngineVersion(t.TempDir())
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "BP_DISABLE_SBOM") {
		t.Errorf("error should mention BP_DISABLE_SBOM, got %v", err)
	}
}
