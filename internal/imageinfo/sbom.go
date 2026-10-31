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

// GoVersion returns the Go toolchain version from the image SBOM. Paketo does
// not ship a launch-layer Go SBOM; the version is embedded in the compiled
// binary SBOM as the stdlib component (version "go1.27.3", cpe golang:go).
// The lifecycle launcher has its own stdlib entry, so pick the SBOM that also
// lists the application binary (processCommand's basename).
func GoVersion(ctx context.Context, r Runner, image, processCommand string) (string, error) {
	dir, err := os.MkdirTemp("", "p2df-sbom-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	if _, err := r.Output(ctx, "pack", "sbom", "download", image, "--output-dir", dir); err != nil {
		return "", fmt.Errorf("pack sbom download %s: %w", image, err)
	}
	return findGoVersion(dir, filepath.Base(processCommand))
}

type cdxComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	CPE     string `json:"cpe"`
}

func findGoVersion(dir, binary string) (string, error) {
	paths, err := walkSBOMs(dir)
	if err != nil {
		return "", err
	}
	var fallback string
	for _, path := range paths.cdx {
		var doc struct {
			Components []cdxComponent `json:"components"`
		}
		raw, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(raw, &doc) != nil {
			continue
		}
		hasApp := false
		goVersion := ""
		for _, c := range doc.Components {
			if c.Name == binary {
				hasApp = true
			}
			if goVersion == "" && strings.HasPrefix(c.Version, "go") &&
				(c.Name == "stdlib" || strings.Contains(c.CPE, "golang:go:")) {
				goVersion = strings.TrimPrefix(c.Version, "go")
			}
		}
		if goVersion == "" {
			continue
		}
		if hasApp {
			return goVersion, nil
		}
		if fallback == "" {
			fallback = goVersion
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("Go version not found in image SBOM (was the image built with BP_DISABLE_SBOM set?)")
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

type sbomPaths struct{ cdx, legacy []string }

func walkSBOMs(dir string) (sbomPaths, error) {
	var paths sbomPaths
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch d.Name() {
		case "sbom.cdx.json":
			paths.cdx = append(paths.cdx, path)
		case "sbom.legacy.json":
			paths.legacy = append(paths.legacy, path)
		}
		return nil
	})
	return paths, err
}

func findRuntimeVersion(dir string, names ...string) (string, error) {
	paths, err := walkSBOMs(dir)
	if err != nil {
		return "", err
	}
	for _, path := range paths.cdx {
		if v := versionFromCDX(path, names); v != "" {
			return v, nil
		}
	}
	for _, path := range paths.legacy {
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
