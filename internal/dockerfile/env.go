package dockerfile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
)

// launchEnvScript prints every /layers/<buildpack>/<layer>/env and env.launch
// file with a marker header. Empirically the launcher injects both directories
// into the process env at runtime (e.g. Paketo node-engine writes NODE_ENV and
// NODE_HOME under env/), so both are part of the runtime contract.
const launchEnvScript = `for d in /layers/*/*/env /layers/*/*/env.launch; do
  [ -d "$d" ] || continue
  for f in "$d"/*; do
    [ -f "$f" ] || continue
    printf '@@FILE %s\n' "$f"
    cat "$f"
    printf '\n'
  done
done`

// ReadLaunchEnv reads the static launch env baked into the image layers.
func ReadLaunchEnv(ctx context.Context, r imageinfo.Runner, image string) ([]Env, error) {
	out, err := r.Output(ctx, "docker", "run", "--rm", "--entrypoint", "/bin/sh", image, "-c", launchEnvScript)
	if err != nil {
		return nil, fmt.Errorf("read /layers env.launch: %w", err)
	}
	return parseLaunchEnv(string(out)), nil
}

// parseLaunchEnv understands the CNB env.launch file suffixes: .default and
// .override carry literal values; .append/.prepend/.delim modify an existing
// variable and are not reconstructed (they only affect PATH-like vars today).
func parseLaunchEnv(raw string) []Env {
	defaults := map[string]string{}
	overrides := map[string]string{}
	var path, value string
	flush := func() {
		if path == "" {
			return
		}
		base := path[strings.LastIndex(path, "/")+1:]
		name, kind, ok := strings.Cut(base, ".")
		if !ok {
			return
		}
		if isNoiseEnv(name) {
			return
		}
		switch kind {
		case "default":
			defaults[name] = strings.TrimRight(value, "\r\n")
		case "override":
			overrides[name] = strings.TrimRight(value, "\r\n")
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		if rest, ok := strings.CutPrefix(line, "@@FILE "); ok {
			flush()
			path, value = rest, ""
			continue
		}
		if path != "" {
			if value != "" {
				value += "\n"
			}
			value += line
		}
	}
	flush()

	values := map[string]string{}
	for k, v := range defaults {
		values[k] = v
	}
	for k, v := range overrides {
		values[k] = v
	}

	out := make([]Env, 0, len(values))
	for k, v := range values {
		out = append(out, Env{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// isNoiseEnv mirrors the verifier's noise whitelist (spec 通过线): variables
// the generated image is not expected to reproduce.
func isNoiseEnv(key string) bool {
	switch key {
	case "PATH", "HOME", "HOSTNAME", "LANG", "PWD", "SHLVL", "_",
		"MEMORY_AVAILABLE", "JAVA_TOOL_OPTIONS":
		return true
	}
	for _, p := range []string{"LC_", "CNB_", "BP_", "BPL_"} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}
