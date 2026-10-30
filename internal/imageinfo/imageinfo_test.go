package imageinfo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

type fakeRunner struct {
	files map[string]string // "command args..." -> fixture path
}

func (f fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	path, ok := f.files[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s", key)
	}
	return os.ReadFile(path)
}

func runnerFor(image, inspectFixture string) fakeRunner {
	return fakeRunner{files: map[string]string{
		"pack inspect-image " + image + " --output json": "testdata/" + inspectFixture,
		"docker image inspect " + image:                  "testdata/docker_inspect_node.json",
	}}
}

func TestLoadNode(t *testing.T) {
	img, err := Load(context.Background(), runnerFor("my-app", "pack_inspect_node.json"), "my-app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := strings.Join(img.Languages, ","), "node"; got != want {
		t.Errorf("languages = %q, want %q", got, want)
	}
	if img.DefaultProcess == nil {
		t.Fatal("default process is nil")
	}
	if got, want := img.DefaultProcess.Type, "web"; got != want {
		t.Errorf("default process type = %q, want %q", got, want)
	}
	if got, want := img.DefaultProcess.Command+" "+strings.Join(img.DefaultProcess.Args, " "), "node server.js"; got != want {
		t.Errorf("default process = %q, want %q", got, want)
	}
	if !img.DefaultProcess.Direct {
		t.Error("default process should be direct")
	}
	if got, want := img.RunImage, "index.docker.io/paketobuildpacks/run-jammy-full:latest"; got != want {
		t.Errorf("run image = %q, want %q", got, want)
	}
	if got, want := img.User, "1002:1000"; got != want {
		t.Errorf("user = %q, want %q", got, want)
	}
}

func TestLoadRemoteFallback(t *testing.T) {
	img, err := Load(context.Background(), runnerFor("my-app", "pack_inspect_remote.json"), "my-app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := strings.Join(img.Languages, ","), "node"; got != want {
		t.Errorf("languages = %q, want %q", got, want)
	}
}

func TestLoadUnsupported(t *testing.T) {
	_, err := Load(context.Background(), runnerFor("utility-app", "pack_inspect_unsupported.json"), "utility-app")
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want UnsupportedError", err)
	}
	if len(unsupported.Buildpacks) != 2 {
		t.Errorf("buildpacks listed = %v, want both utility buildpacks", unsupported.Buildpacks)
	}
}

func TestLoadAmbiguous(t *testing.T) {
	_, err := Load(context.Background(), runnerFor("mixed-app", "pack_inspect_ambiguous.json"), "mixed-app")
	var ambiguous *AmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("error = %v, want AmbiguousError", err)
	}
	if got, want := strings.Join(ambiguous.Languages, ","), "node,python"; got != want {
		t.Errorf("languages = %q, want %q", got, want)
	}
}
