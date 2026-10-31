package dockerfile

import (
	"strings"
	"testing"

	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
)

func sampleImage() *imageinfo.Image {
	return &imageinfo.Image{
		Name:       "node-hello:latest",
		ImageID:    "sha256:abc123",
		Stack:      "io.buildpacks.stacks.jammy",
		RunImage:   "index.docker.io/paketobuildpacks/run-jammy-full:latest",
		User:       "1002:1000",
		WorkingDir: "/workspace",
		Buildpacks: []imageinfo.Buildpack{{ID: "paketo-buildpacks/node-engine", Version: "8.11.1"}},
		Processes: []imageinfo.Process{
			{Type: "web", Command: "node", Args: []string{"server.js"}, Direct: true, Default: true, WorkingDir: "/workspace"},
			{Type: "worker", Command: "node", Args: []string{"worker.js"}, Direct: true},
		},
		Languages:      []string{"node"},
		DefaultProcess: nil, // set below
	}
}

func renderSample(t *testing.T) string {
	t.Helper()
	img := sampleImage()
	img.DefaultProcess = &img.Processes[0]
	out, err := Render(Input{
		Image:       img,
		NodeVersion: "24.22.1",
		LaunchEnv:   []Env{{Key: "NODE_PROJECT_PATH", Value: "/workspace"}, {Key: "NPM_CONFIG_LOGLEVEL", Value: "error"}},
		Builder:     "paketobuildpacks/builder-jammy-full",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return string(out)
}

func TestRenderNode(t *testing.T) {
	out := renderSample(t)
	for _, want := range []string{
		"FROM node:24.22.1 AS build",
		"FROM index.docker.io/paketobuildpacks/run-jammy-full:latest",
		"USER 1002:1000",
		"WORKDIR /workspace",
		"COPY --from=build --chown=1002:1000 /workspace /workspace",
		"ENV NODE_PROJECT_PATH=\"/workspace\"",
		"ENV NPM_CONFIG_LOGLEVEL=\"error\"",
		"CMD []",
		"ENTRYPOINT [\"node\",\"server.js\"]",
		"# Builder: paketobuildpacks/builder-jammy-full",
		"# Buildpacks: paketo-buildpacks/node-engine 8.11.1;",
		"# Node: 24.22.1 (from image SBOM)",
		"#   web (default): node server.js",
		"#   worker: node worker.js",
		"#   docker run --rm -it --entrypoint node node-hello:latest worker.js",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Dockerfile missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "buildpack 内部") {
		t.Error("unexpected marker")
	}
}

func TestRenderRejectsShellProcess(t *testing.T) {
	img := sampleImage()
	img.Processes[0].Direct = false
	img.DefaultProcess = &img.Processes[0]
	_, err := Render(Input{Image: img, NodeVersion: "24.22.1"})
	if err == nil {
		t.Fatal("want error for shell-form process")
	}
}

func TestTranslateNodePaths(t *testing.T) {
	got := translateNodePaths([]Env{
		{Key: "NODE_HOME", Value: "/layers/paketo-buildpacks_node-engine/node"},
		{Key: "LD_LIBRARY_PATH", Value: "/layers/paketo-buildpacks_node-engine/node/lib"},
		{Key: "NODE_ENV", Value: "production"},
	})
	want := []Env{
		{Key: "NODE_HOME", Value: "/usr/local"},
		{Key: "LD_LIBRARY_PATH", Value: "/usr/local/lib"},
		{Key: "NODE_ENV", Value: "production"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("env[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseLaunchEnv(t *testing.T) {
	raw := strings.Join([]string{
		"@@FILE /layers/paketo-buildpacks_npm-install/launch-modules/env.launch/NPM_CONFIG_LOGLEVEL.default",
		"error",
		"@@FILE /layers/paketo-buildpacks_npm-install/launch-modules/env.launch/NODE_PROJECT_PATH.default",
		"/workspace",
		"@@FILE /layers/x/y/env.launch/NODE_PROJECT_PATH.override",
		"/app",
		"@@FILE /layers/x/y/env.launch/PATH.append",
		"/layers/x/y/node_modules/.bin",
		"@@FILE /layers/x/y/env.launch/CNB_FOO.default",
		"ignored",
	}, "\n")
	got := parseLaunchEnv(raw)
	want := []Env{{Key: "NODE_PROJECT_PATH", Value: "/app"}, {Key: "NPM_CONFIG_LOGLEVEL", Value: "error"}}
	if len(got) != len(want) {
		t.Fatalf("env = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("env[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
