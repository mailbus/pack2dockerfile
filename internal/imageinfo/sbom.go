package imageinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// NodeEngineVersion downloads the image SBOM and returns the Node Engine
// component version (the version the buildpack installed, which is not
// recorded in image labels).
func NodeEngineVersion(ctx context.Context, r Runner, image string) (string, error) {
	dir, err := os.MkdirTemp("", "p2df-sbom-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	if _, err := r.Output(ctx, "pack", "sbom", "download", image, "--output-dir", dir); err != nil {
		return "", fmt.Errorf("pack sbom download %s: %w", image, err)
	}
	return findNodeEngineVersion(dir)
}

func findNodeEngineVersion(dir string) (string, error) {
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
		if v := nodeVersionFromCDX(path); v != "" {
			return v, nil
		}
	}
	for _, path := range legacy {
		if v := nodeVersionFromLegacy(path); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("Node Engine version not found in image SBOM (was the image built with BP_DISABLE_SBOM set?)")
}

func nodeVersionFromCDX(path string) string {
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
		if c.Name == "Node Engine" && c.Version != "" {
			return c.Version
		}
	}
	return ""
}

func nodeVersionFromLegacy(path string) string {
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
		if c.Name == "Node Engine" && c.Metadata.Version != "" {
			return c.Metadata.Version
		}
	}
	return ""
}
