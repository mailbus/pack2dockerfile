package verify

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
)

func TestGeneratedCommandLine(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config
		origCmd string
		want    string
	}{
		{
			name: "exec form direct",
			cfg:  config{Entrypoint: []string{"node", "server.js"}},
			want: "node server.js",
		},
		{
			name: "entrypoint plus cmd",
			cfg:  config{Entrypoint: []string{"java"}, Cmd: []string{"-jar", "app.jar"}},
			want: "java -jar app.jar",
		},
		{
			name: "cmd only",
			cfg:  config{Cmd: []string{"node", "server.js"}},
			want: "node server.js",
		},
		{
			name:    "launcher process symlink keeps original process",
			cfg:     config{Entrypoint: []string{"/cnb/process/web"}},
			origCmd: "node server.js",
			want:    "node server.js",
		},
		{
			name:    "launcher binary keeps original process",
			cfg:     config{Entrypoint: []string{"/cnb/lifecycle/launcher"}},
			origCmd: "node server.js",
			want:    "node server.js",
		},
		{
			name: "shell form process",
			cfg:  config{Entrypoint: []string{"/bin/bash", "-c", "python app.py"}},
			want: "python app.py",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := generatedCommandLine(tc.cfg, tc.origCmd); got != tc.want {
				t.Errorf("generatedCommandLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommandLine(t *testing.T) {
	p := &imageinfo.Process{Command: "node", Args: []string{"server.js"}}
	if got, want := commandLine(p), "node server.js"; got != want {
		t.Errorf("commandLine() = %q, want %q", got, want)
	}
	if got := commandLine(nil); got != "" {
		t.Errorf("commandLine(nil) = %q, want empty", got)
	}
}

func TestDiffEnv(t *testing.T) {
	orig := map[string]string{
		"NODE_ENV":            "production",
		"PATH":                "/cnb/process:/usr/bin",
		"MEMORY_AVAILABLE":    "1024",
		"CNB_APP_DIR":         "/workspace",
		"LANG":                "C.UTF-8",
		"NODE_HOME":           "/layers/paketo-buildpacks_node-engine/node",
		"LD_LIBRARY_PATH":     "/layers/paketo-buildpacks-node-engine/node/lib",
		"PYTHONPATH":          "/layers/paketo-buildpacks_cpython/cpython",
		"PYTHONPYCACHEPREFIX": "/home/cnb/.pycache",
		"OLDPWD":              "/workspace",
	}
	gen := map[string]string{
		"NODE_ENV": "production",
		"PATH":     "/usr/local/bin:/usr/bin",
	}
	if diffs := diffEnv(orig, gen); len(diffs) != 0 {
		t.Errorf("noise should be filtered, got diffs: %v", diffs)
	}

	gen["NODE_ENV"] = "development"
	gen["P2DF_EXTRA"] = "1"
	diffs := diffEnv(orig, gen)
	if len(diffs) != 2 {
		t.Fatalf("want 2 diffs, got %v", diffs)
	}
	if !strings.Contains(strings.Join(diffs, "\n"), "NODE_ENV") || !strings.Contains(strings.Join(diffs, "\n"), "P2DF_EXTRA") {
		t.Errorf("unexpected diffs: %v", diffs)
	}
}

func TestParseEnv(t *testing.T) {
	raw := "A=1\nB=two=three\n\nC=\n"
	got := parseEnv(raw)
	want := map[string]string{"A": "1", "B": "two=three", "C": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseEnv() = %v, want %v", got, want)
	}
}

func TestResultPassed(t *testing.T) {
	pass := &Result{Checks: []Check{{Name: "a", OK: true}, {Name: "b", OK: true}}}
	if !pass.Passed() {
		t.Error("all-pass result should pass")
	}
	fail := &Result{Checks: []Check{{Name: "a", OK: true}, {Name: "b", OK: false}}}
	if fail.Passed() {
		t.Error("result with a failed check should not pass")
	}
	if !strings.Contains(fail.String(), "[FAIL] b") {
		t.Errorf("report missing failure line: %s", fail.String())
	}
}
