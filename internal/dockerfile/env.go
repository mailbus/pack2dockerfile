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

// parseLaunchEnv understands the CNB env file suffixes. .default/.override
// set the value, .prepend/.append extend it, .delim overrides the separator
// (":" by default). This mirrors what the launcher does at runtime.
func parseLaunchEnv(raw string) []Env {
	type entry struct {
		def, override, delim string
		prepends, appends    []string
	}
	entries := map[string]*entry{}
	var path, value string
	flush := func() {
		if path == "" {
			return
		}
		base := path[strings.LastIndex(path, "/")+1:]
		name, kind, ok := strings.Cut(base, ".")
		if !ok || isNoiseEnv(name) {
			return
		}
		e := entries[name]
		if e == nil {
			e = &entry{delim: ":"}
			entries[name] = e
		}
		v := strings.TrimRight(value, "\r\n")
		switch kind {
		case "default":
			e.def = v
		case "override":
			e.override = v
		case "prepend":
			e.prepends = append(e.prepends, v)
		case "append":
			e.appends = append(e.appends, v)
		case "delim":
			if v != "" {
				e.delim = v
			}
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

	out := make([]Env, 0, len(entries))
	for name, e := range entries {
		v := e.def
		if e.override != "" {
			v = e.override
		}
		for _, p := range e.prepends {
			v = p + e.delim + v
		}
		for _, a := range e.appends {
			v += e.delim + a
		}
		out = append(out, Env{Key: name, Value: v})
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
