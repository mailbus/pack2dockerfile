package imageinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// NodeEngineVersion downloads the image SBOM and returns the Node Engine
// component version (the version the buildpack installed, which is not
// recorded in image labels).
func NodeEngineVersion(ctx context.Context, r Runner, image string) (string, error) {
	return runtimeVersion(ctx, r, image, "Node Engine")
}

// CPythonVersion returns the CPython interpreter version from the image SBOM.
// Paketo records it as "CPython"; "Python" is accepted as a fallback.
func CPythonVersion(ctx context.Context, r Runner, image string) (string, error) {
	return runtimeVersion(ctx, r, image, "CPython", "Python")
}

func runtimeVersion(ctx context.Context, r Runner, image string, names ...string) (string, error) {
	dir, err := os.MkdirTemp("", "p2df-sbom-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	if _, err := r.Output(ctx, "pack", "sbom", "download", image, "--output-dir", dir); err != nil {
		return "", fmt.Errorf("pack sbom download %s: %w", image, err)
	}
	return findRuntimeVersion(dir, names...)
}

func findRuntimeVersion(dir string, names ...string) (string, error) {
	var cdx, legacy []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch d.Name() {
		case "sbom.cdx.json":
			cdx = append(cdx, path)
		case "sbom.legacy.json":
			legacy = append(legacy, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	for _, path := range cdx {
		if v := versionFromCDX(path, names); v != "" {
			return v, nil
		}
	}
	for _, path := range legacy {
		if v := versionFromLegacy(path, names); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s version not found in image SBOM (was the image built with BP_DISABLE_SBOM set?)", strings.Join(names, "/"))
}

func versionFromCDX(path string, names []string) string {
	var doc struct {
		Components []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"components"`
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	for _, c := range doc.Components {
		if c.Version == "" {
			continue
		}
		for _, n := range names {
			if c.Name == n {
				return c.Version
			}
		}
	}
	return ""
}

func versionFromLegacy(path string, names []string) string {
	var doc []struct {
		Name     string `json:"name"`
		Metadata struct {
			Version string `json:"version"`
		} `json:"metadata"`
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	for _, c := range doc {
		if c.Metadata.Version == "" {
			continue
		}
		for _, n := range names {
			if c.Name == n {
				return c.Metadata.Version
			}
		}
	}
	return ""
}
