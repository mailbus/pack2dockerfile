//go:build e2e

package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mailbus/pack2dockerfile/internal/dockerfile"
	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
)

func dockerAvailable(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not available")
	}
}

func imageExists(image string) bool {
	return exec.Command("docker", "image", "inspect", image).Run() == nil
}

func writeDockerfile(t *testing.T, content string) (path, contextDir string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	return path, dir
}

func testImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv("P2DF_TEST_IMAGE")
	if image == "" {
		image = "node-hello"
	}
	if !imageExists(image) {
		t.Skipf("image %q not found; build it first: pack build node-hello --builder paketobuildpacks/builder-jammy-full --path fixtures/node-hello", image)
	}
	return image
}

func TestSelfEquivalence(t *testing.T) {
	dockerAvailable(t)
	orig := testImage(t)

	df, ctxDir := writeDockerfile(t, "FROM "+orig+"\n")
	res, err := Run(context.Background(), Options{
		Original:   orig,
		Dockerfile: df,
		ContextDir: ctxDir,
		Tag:        "p2df-verify-self",
		Timeout:    15 * time.Second,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	t.Logf("report:\n%s", res)
	if !res.Passed() {
		t.Fatalf("self-equivalence should pass")
	}
}

func TestDetectsMismatches(t *testing.T) {
	dockerAvailable(t)
	orig := testImage(t)

	cases := []struct {
		name       string
		dockerfile string
		wantCheck  string
	}{
		{"cmd", "FROM " + orig + "\nENTRYPOINT [\"node\",\"missing.js\"]\n", "process"},
		{"user", "FROM " + orig + "\nUSER root\n", "user"},
		{"env", "FROM " + orig + "\nENV P2DF_EXTRA=1\n", "env"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			df, ctxDir := writeDockerfile(t, tc.dockerfile)
			res, err := Run(context.Background(), Options{
				Original:   orig,
				Dockerfile: df,
				ContextDir: ctxDir,
				Tag:        "p2df-verify-neg-" + tc.name,
				Timeout:    10 * time.Second,
			})
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			t.Logf("report:\n%s", res)
			if res.Passed() {
				t.Fatalf("mismatch %q should fail", tc.name)
			}
			found := false
			for _, c := range res.Checks {
				if c.Name == tc.wantCheck && !c.OK {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected check %q to fail, report:\n%s", tc.wantCheck, res)
			}
			if !strings.Contains(res.String(), "[FAIL]") {
				t.Error("report should contain a FAIL line")
			}
		})
	}
}

// verifyGeneratedImage is the closed loop for one language: read the image,
// render a Dockerfile with p2df's template, rebuild from the fixture source,
// and require all four behavior checks to pass.
func verifyGeneratedImage(t *testing.T, fixture string) {
	t.Helper()
	dockerAvailable(t)
	orig := testImage(t)
	ctx := context.Background()
	runner := imageinfo.ExecRunner{}

	img, err := imageinfo.Load(ctx, runner, orig)
	if err != nil {
		t.Fatalf("load image: %v", err)
	}
	if len(img.Languages) != 1 {
		t.Fatalf("languages = %v, want exactly one", img.Languages)
	}
	var runtimeVersion string
	switch img.Languages[0] {
	case "node":
		runtimeVersion, err = imageinfo.NodeEngineVersion(ctx, runner, orig)
	case "python":
		runtimeVersion, err = imageinfo.CPythonVersion(ctx, runner, orig)
	case "go":
		runtimeVersion, err = imageinfo.GoVersion(ctx, runner, orig, img.DefaultProcess.Command)
	default:
		t.Fatalf("no template for language %q", img.Languages[0])
	}
	if err != nil {
		t.Fatalf("runtime version: %v", err)
	}
	launchEnv, err := dockerfile.ReadLaunchEnv(ctx, runner, orig)
	if err != nil {
		t.Fatalf("read launch env: %v", err)
	}
	rendered, err := dockerfile.Render(dockerfile.Input{Image: img, RuntimeVersion: runtimeVersion, LaunchEnv: launchEnv})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	t.Logf("generated Dockerfile:\n%s", rendered)

	fixtureDir, err := filepath.Abs(filepath.Join("..", "..", "fixtures", fixture))
	if err != nil {
		t.Fatal(err)
	}
	df, _ := writeDockerfile(t, string(rendered))
	res, err := Run(ctx, Options{
		Original:   orig,
		Dockerfile: df,
		ContextDir: fixtureDir,
		Tag:        "p2df-verify-generated",
		Timeout:    60 * time.Second,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	t.Logf("report:\n%s", res)
	if !res.Passed() {
		t.Fatalf("generated Dockerfile should pass the four checks")
	}
}

// M1 closed loop (ticket 03).
func TestGeneratedNodeDockerfile(t *testing.T) {
	verifyGeneratedImage(t, "node-hello")
}

// M2 closed loop (ticket 04).
func TestGeneratedPythonDockerfile(t *testing.T) {
	verifyGeneratedImage(t, "python-hello")
}

// M3 closed loop (ticket 05).
func TestGeneratedGoDockerfile(t *testing.T) {
	verifyGeneratedImage(t, "go-hello")
}
