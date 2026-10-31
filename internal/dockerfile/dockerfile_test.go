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
		Image:          img,
		RuntimeVersion: "24.22.1",
		LaunchEnv:      []Env{{Key: "NODE_PROJECT_PATH", Value: "/workspace"}, {Key: "NPM_CONFIG_LOGLEVEL", Value: "error"}},
		Builder:        "paketobuildpacks/builder-jammy-full",
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
		"COPY --from=build /usr/local /usr/local",
		"ENV NODE_PROJECT_PATH=\"/workspace\"",
		"ENV NPM_CONFIG_LOGLEVEL=\"error\"",
		"CMD []",
		"ENTRYPOINT [\"node\",\"server.js\"]",
		"# Builder: paketobuildpacks/builder-jammy-full",
		"# Buildpacks: paketo-buildpacks/node-engine 8.11.1;",
		"# Runtime: node:24.22.1 (from image SBOM)",
		"#   web (default): node server.js",
		"#   worker: node worker.js",
		"#   docker run --rm -it --entrypoint node node-hello:latest worker.js",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Dockerfile missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderPythonShellProcess(t *testing.T) {
	img := &imageinfo.Image{
		Name:       "python-hello:latest",
		ImageID:    "sha256:def456",
		Stack:      "io.buildpacks.stacks.jammy",
		RunImage:   "paketobuildpacks/run-jammy-full:latest",
		User:       "1002:1000",
		WorkingDir: "/workspace",
		Buildpacks: []imageinfo.Buildpack{{ID: "paketo-buildpacks/cpython", Version: "1.18.50"}},
		Processes: []imageinfo.Process{
			{Type: "web", Command: "python app.py", Shell: "bash", Default: true, WorkingDir: "/workspace"},
		},
		Languages:      []string{"python"},
		DefaultProcess: nil,
	}
	img.DefaultProcess = &img.Processes[0]
	out, err := Render(Input{
		Image:          img,
		RuntimeVersion: "3.11.18",
		LaunchEnv: []Env{{
			Key:   "PYTHONPATH",
			Value: "/layers/paketo-buildpacks_pip-install/packages/lib/python3.11/site-packages:/layers/paketo-buildpacks_cpython/cpython",
		}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"FROM python:3.11.18-bookworm AS build",
		"RUN pip install --no-cache-dir -r requirements.txt",
		"COPY --from=build /usr/local /usr/local",
		"ENV PYTHONPATH=\"/usr/local/lib/python3.11/site-packages:/usr/local\"",
		"ENTRYPOINT [\"/bin/bash\",\"-c\",\"python app.py\"]",
		"#   docker run --rm -it --entrypoint /bin/bash python-hello:latest -c \"python app.py\"",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Dockerfile missing %q\n---\n%s", want, s)
		}
	}
}

func TestRenderGo(t *testing.T) {
	img := &imageinfo.Image{
		Name:       "go-hello:latest",
		ImageID:    "sha256:789abc",
		Stack:      "io.buildpacks.stacks.jammy",
		RunImage:   "paketobuildpacks/run-jammy-full:latest",
		User:       "1002:1000",
		WorkingDir: "/workspace",
		Buildpacks: []imageinfo.Buildpack{{ID: "paketo-buildpacks/go-build", Version: "2.4.47"}},
		Processes: []imageinfo.Process{
			{Type: "go-hello", Command: "/layers/paketo-buildpacks_go-build/targets/bin/go-hello", Direct: true, Default: true, WorkingDir: "/workspace"},
		},
		Languages:      []string{"go"},
		DefaultProcess: nil,
	}
	img.DefaultProcess = &img.Processes[0]
	out, err := Render(Input{Image: img, RuntimeVersion: "1.27.3"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"FROM golang:1.27.3-bookworm AS build",
		"RUN go build -trimpath -o go-hello .",
		"#   go-hello (default): go-hello",
		"#   docker run --rm -it --entrypoint go-hello go-hello:latest",
		"ENV PATH=\"/workspace:$PATH\"",
		"ENTRYPOINT [\"go-hello\"]",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Dockerfile missing %q\n---\n%s", want, s)
		}
	}
	if strings.Contains(s, "COPY --from=build /usr/local /usr/local") {
		t.Error("Go image should not copy /usr/local")
	}
}

func TestRenderRejectsUnknownLanguage(t *testing.T) {
	img := sampleImage()
	img.Languages = []string{"ruby"}
	img.DefaultProcess = &img.Processes[0]
	_, err := Render(Input{Image: img, RuntimeVersion: "3.3"})
	if err == nil {
		t.Fatal("want error for unsupported language")
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
		"@@FILE /layers/paketo-buildpacks_cpython/cpython/env/PYTHONPATH.default",
		"/layers/paketo-buildpacks_cpython/cpython",
		"@@FILE /layers/paketo-buildpacks_pip-install/packages/env/PYTHONPATH.prepend",
		"/layers/paketo-buildpacks_pip-install/packages/lib/python3.11/site-packages",
	}, "\n")
	got := parseLaunchEnv(raw)
	want := []Env{
		{Key: "NODE_PROJECT_PATH", Value: "/app"},
		{Key: "NPM_CONFIG_LOGLEVEL", Value: "error"},
		{Key: "PYTHONPATH", Value: "/layers/paketo-buildpacks_pip-install/packages/lib/python3.11/site-packages:/layers/paketo-buildpacks_cpython/cpython"},
	}
	if len(got) != len(want) {
		t.Fatalf("env = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("env[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
