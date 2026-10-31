// Package verify checks that a generated Dockerfile rebuild of a CNB image is
// behaviorally equivalent to the original image (spec: 通过线).
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
)

// Options configures a verification run.
type Options struct {
	Original      string        // image built by pack
	Dockerfile    string        // generated Dockerfile to rebuild
	ContextDir    string        // docker build context
	Tag           string        // tag for the generated image
	ContainerPort int           // port the app listens on (default 8080)
	Timeout       time.Duration // startup / health-check budget (default 30s)
}

// Check is one equivalence assertion.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// Result aggregates all checks.
type Result struct {
	Checks []Check
}

// Passed reports whether every check passed.
func (r *Result) Passed() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// String renders a human-readable report.
func (r *Result) String() string {
	var b strings.Builder
	for _, c := range r.Checks {
		status := "PASS"
		if !c.OK {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", status, c.Name, c.Detail)
	}
	return b.String()
}

// Run builds the Dockerfile, starts both images, and compares behavior.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.ContainerPort == 0 {
		opts.ContainerPort = 8080
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.Tag == "" {
		opts.Tag = "p2df-verify-generated"
	}

	info, err := imageinfo.Load(ctx, imageinfo.ExecRunner{}, opts.Original)
	if err != nil {
		return nil, fmt.Errorf("read original image metadata: %w", err)
	}
	if info.DefaultProcess == nil {
		return nil, fmt.Errorf("original image %q has no default process", opts.Original)
	}

	if err := docker(ctx, "build", "-t", opts.Tag, "-f", opts.Dockerfile, opts.ContextDir); err != nil {
		return nil, fmt.Errorf("build generated image: %w", err)
	}
	defer func() { _ = docker(ctx, "rmi", "-f", opts.Tag) }()

	cfg, err := inspectConfig(ctx, opts.Tag)
	if err != nil {
		return nil, fmt.Errorf("inspect generated image: %w", err)
	}

	res := &Result{}

	// 1. Process: normalized default process vs generated entrypoint.
	origCmd := commandLine(info.DefaultProcess)
	genCmd := generatedCommandLine(cfg, origCmd)
	res.Checks = append(res.Checks, Check{
		Name:   "process",
		OK:     origCmd == genCmd,
		Detail: fmt.Sprintf("original %q, generated %q", origCmd, genCmd),
	})

	// 2. User: runtime UID must match.
	origUID, origUIDErr := runUID(ctx, opts.Original)
	genUID, genUIDErr := runUID(ctx, opts.Tag)
	res.Checks = append(res.Checks, Check{
		Name:   "user",
		OK:     origUIDErr == nil && genUIDErr == nil && origUID == genUID,
		Detail: uidDetail(origUID, origUIDErr, genUID, genUIDErr),
	})

	// 3. Smoke: /health response and 4. env: runtime PID1 environment.
	origApp, err := startApp(ctx, opts.Original, "p2df-verify-orig", opts.ContainerPort)
	if err != nil {
		return nil, fmt.Errorf("start original image: %w", err)
	}
	defer origApp.stop(ctx)
	origStatus, origBody, err := waitHealth(ctx, origApp.url(), opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("original image never became healthy: %w", err)
	}

	genApp, err := startApp(ctx, opts.Tag, "p2df-verify-gen", opts.ContainerPort)
	if err != nil {
		res.Checks = append(res.Checks,
			Check{Name: "smoke", OK: false, Detail: "generated container failed to start: " + err.Error()},
			Check{Name: "env", OK: false, Detail: "generated container failed to start"},
		)
		return res, nil
	}
	defer genApp.stop(ctx)

	genStatus, genBody, healthErr := waitHealth(ctx, genApp.url(), opts.Timeout)
	if healthErr != nil {
		res.Checks = append(res.Checks, Check{
			Name:   "smoke",
			OK:     false,
			Detail: "generated app not healthy: " + healthErr.Error(),
		})
	} else {
		res.Checks = append(res.Checks, Check{
			Name:   "smoke",
			OK:     origStatus == genStatus && origBody == genBody,
			Detail: fmt.Sprintf("original %d %q, generated %d %q", origStatus, origBody, genStatus, genBody),
		})
	}

	origEnv, origEnvErr := origApp.environ(ctx)
	genEnv, genEnvErr := genApp.environ(ctx)
	switch {
	case origEnvErr != nil || genEnvErr != nil:
		res.Checks = append(res.Checks, Check{
			Name:   "env",
			OK:     false,
			Detail: fmt.Sprintf("read /proc/1/environ: original=%v generated=%v", origEnvErr, genEnvErr),
		})
	default:
		diffs := diffEnv(origEnv, genEnv)
		detail := "runtime env matches (noise filtered)"
		if len(diffs) > 0 {
			detail = "differences: " + strings.Join(diffs, "; ")
		}
		res.Checks = append(res.Checks, Check{Name: "env", OK: len(diffs) == 0, Detail: detail})
	}

	return res, nil
}

func docker(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		return fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return nil
}

func dockerOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

type config struct {
	Entrypoint []string `json:"Entrypoint"`
	Cmd        []string `json:"Cmd"`
	User       string   `json:"User"`
	Env        []string `json:"Env"`
}

func inspectConfig(ctx context.Context, image string) (config, error) {
	out, err := dockerOutput(ctx, "image", "inspect", "--format", "{{json .Config}}", image)
	if err != nil {
		return config{}, err
	}
	var cfg config
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		return config{}, fmt.Errorf("parse image config: %w", err)
	}
	return cfg, nil
}

func commandLine(p *imageinfo.Process) string {
	if p == nil {
		return ""
	}
	return strings.Join(append([]string{p.Command}, p.Args...), " ")
}

// generatedCommandLine normalizes the generated image's entrypoint. Images
// that still use the CNB launcher (e.g. FROM the original image) keep the
// launcher's process selection, so the original default process stands.
func generatedCommandLine(cfg config, origCmd string) string {
	if len(cfg.Entrypoint) > 0 {
		if strings.HasPrefix(cfg.Entrypoint[0], "/cnb/process/") || filepath.Base(cfg.Entrypoint[0]) == "launcher" {
			return origCmd
		}
		// CNB shell-form processes are recorded as a single command string;
		// generated images mirror them as ["/bin/<shell>","-c",command].
		if len(cfg.Entrypoint) >= 3 && cfg.Entrypoint[1] == "-c" && isShell(cfg.Entrypoint[0]) {
			return strings.Join(cfg.Entrypoint[2:], " ")
		}
		return strings.Join(append(append([]string{}, cfg.Entrypoint...), cfg.Cmd...), " ")
	}
	return strings.Join(cfg.Cmd, " ")
}

func isShell(path string) bool {
	base := filepath.Base(path)
	return base == "sh" || base == "bash"
}

func runUID(ctx context.Context, image string) (string, error) {
	return dockerOutput(ctx, "run", "--rm", "--entrypoint", "/bin/sh", image, "-c", "id -u")
}

func uidDetail(orig string, origErr error, gen string, genErr error) string {
	if origErr != nil || genErr != nil {
		return fmt.Sprintf("original=%q(%v) generated=%q(%v)", orig, origErr, gen, genErr)
	}
	return fmt.Sprintf("original uid %s, generated uid %s", orig, gen)
}

type appRun struct {
	name     string
	hostPort string
}

func startApp(ctx context.Context, image, name string, port int) (*appRun, error) {
	_ = docker(ctx, "rm", "-f", name)
	if err := docker(ctx, "run", "-d", "--name", name, "-p", fmt.Sprintf("127.0.0.1::%d", port), image); err != nil {
		return nil, err
	}
	run := &appRun{name: name}
	mapping, err := dockerOutput(ctx, "port", name, fmt.Sprintf("%d/tcp", port))
	if err != nil {
		run.stop(ctx)
		return nil, fmt.Errorf("resolve host port: %w", err)
	}
	_, portStr, err := net.SplitHostPort(mapping)
	if err != nil {
		run.stop(ctx)
		return nil, fmt.Errorf("parse port mapping %q: %w", mapping, err)
	}
	run.hostPort = portStr
	return run, nil
}

func (a *appRun) url() string {
	return "http://127.0.0.1:" + a.hostPort + "/health"
}

func (a *appRun) environ(ctx context.Context) (map[string]string, error) {
	out, err := exec.CommandContext(ctx, "docker", "exec", a.name, "sh", "-c", `tr '\0' '\n' < /proc/1/environ`).Output()
	if err != nil {
		return nil, err
	}
	return parseEnv(string(out)), nil
}

func (a *appRun) stop(ctx context.Context) {
	_ = docker(ctx, "rm", "-f", a.name)
}

func waitHealth(ctx context.Context, url string, budget time.Duration) (int, string, error) {
	deadline := time.Now().Add(budget)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return resp.StatusCode, strings.TrimSpace(string(body)), nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return 0, "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return 0, "", fmt.Errorf("not healthy within %s: %w", budget, lastErr)
}

func parseEnv(raw string) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		k, v, ok := strings.Cut(line, "=")
		if ok && k != "" {
			env[k] = v
		}
	}
	return env
}

// Noise that legitimately differs between a CNB image and a generated one:
// infrastructure vars and exec.d-computed dynamic vars (see spec 已知限制).
var noiseExact = map[string]bool{
	"PATH":              true,
	"HOME":              true,
	"HOSTNAME":          true,
	"LANG":              true,
	"PWD":               true,
	"SHLVL":             true,
	"_":                 true,
	"MEMORY_AVAILABLE":  true, // node-engine exec.d
	"JAVA_TOOL_OPTIONS": true, // Java memory calculator exec.d
	// Layout-derived paths (spec v4/v5): the original points into /layers, the
	// generated image installs the runtime at /usr/local. The Dockerfile
	// translates these values; the literal value still differs by design.
	"NODE_HOME":           true,
	"LD_LIBRARY_PATH":     true,
	"PYTHONPATH":          true,
	"PYTHONPYCACHEPREFIX": true, // cpython exec.d
	"OLDPWD":              true, // shell artifact, same class as PWD
}

var noisePrefixes = []string{"LC_", "CNB_", "BP_", "BPL_"}

func isNoise(key string) bool {
	if noiseExact[key] {
		return true
	}
	for _, p := range noisePrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func diffEnv(orig, gen map[string]string) []string {
	keys := map[string]bool{}
	for k := range orig {
		keys[k] = true
	}
	for k := range gen {
		keys[k] = true
	}
	var diffs []string
	for k := range keys {
		if isNoise(k) {
			continue
		}
		if orig[k] != gen[k] {
			diffs = append(diffs, fmt.Sprintf("%s: original %q, generated %q", k, orig[k], gen[k]))
		}
	}
	sort.Strings(diffs)
	return diffs
}
