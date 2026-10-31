// Package dockerfile renders a CNB image model back into a human-readable,
// rebuildable Dockerfile (spec: 真·语义反推, MVP1 Node/Python).
package dockerfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
)

// Version is stamped into the generated file header.
const Version = "0.1.0-dev"

// Env is one static launch environment variable read from /layers/**/env[.launch].
type Env struct {
	Key   string
	Value string
}

// Input is everything the template needs; it is pure data so rendering is testable.
type Input struct {
	Image          *imageinfo.Image
	RuntimeVersion string
	LaunchEnv      []Env
	Builder        string // optional annotation; the builder is not recorded in the image
}

// runtime describes the language-specific half of the template.
type runtime struct {
	Language   string
	BuildImage string
	Setup      []string // Dockerfile lines for the build stage
	Copy       []string // runtime paths to copy from the build stage
	Note       []string // header comments about distro equivalence
	Translate  func([]Env) []Env
	// NormalizeCommand rewrites the default process command for the generated
	// image (Go points at a /layers path that only exists in CNB images).
	NormalizeCommand func(string) string
	// PathPrepend puts the workdir on PATH so a bare binary name resolves.
	PathPrepend bool
}

func runtimeFor(language, version string, proc *imageinfo.Process) (runtime, error) {
	switch language {
	case "node":
		return runtime{
			Language:   "node",
			BuildImage: "node:" + version,
			Setup:      []string{"RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi"},
			Copy:       []string{"/usr/local"},
			Note: []string{
				"The official Node image is Debian-based; the Paketo stack is Ubuntu jammy.",
				"The node binary itself is the nodejs.org build and runs on jammy's glibc.",
			},
			Translate: translateNodePaths,
		}, nil
	case "python":
		return runtime{
			Language:   "python",
			BuildImage: "python:" + version + "-bookworm",
			Setup:      []string{"RUN pip install --no-cache-dir -r requirements.txt"},
			Copy:       []string{"/usr/local"},
			Note: []string{
				"There is no official Python image for the jammy stack. The default",
				"(trixie) image needs GLIBC_2.38 and does not run on jammy; the bookworm",
				"variant does (verified by the behavior checks).",
			},
			Translate: func(env []Env) []Env { return translatePythonPaths(env, version) },
		}, nil
	case "go":
		binary := filepath.Base(proc.Command)
		return runtime{
			Language:   "go",
			BuildImage: "golang:" + version + "-bookworm",
			Setup:      []string{"RUN go build -trimpath -o " + binary + " ."},
			Note: []string{
				"Go binaries are self-contained; only the compiled binary is copied",
				"from the bookworm build stage to the jammy run image.",
			},
			NormalizeCommand: func(cmd string) string {
				if strings.HasPrefix(cmd, "/layers/") {
					return filepath.Base(cmd)
				}
				return cmd
			},
			PathPrepend: true,
		}, nil
	}
	return runtime{}, fmt.Errorf("no template for language %q yet", language)
}

// Render produces the Dockerfile.
func Render(in Input) ([]byte, error) {
	if in.Image == nil {
		return nil, fmt.Errorf("nil image")
	}
	if in.Image.DefaultProcess == nil {
		return nil, fmt.Errorf("image %q has no default process", in.Image.Name)
	}
	if in.RuntimeVersion == "" {
		return nil, fmt.Errorf("missing runtime version")
	}
	if in.Image.RunImage == "" {
		return nil, fmt.Errorf("image %q has no run image recorded", in.Image.Name)
	}
	language := ""
	if len(in.Image.Languages) == 1 {
		language = in.Image.Languages[0]
	}
	rt, err := runtimeFor(language, in.RuntimeVersion, in.Image.DefaultProcess)
	if err != nil {
		return nil, err
	}

	workdir := in.Image.WorkingDir
	if workdir == "" {
		workdir = "/workspace"
	}
	entrypoint, err := json.Marshal(entrypointFor(in.Image.DefaultProcess, rt))
	if err != nil {
		return nil, err
	}
	user := in.Image.User
	if user == "" {
		user = "1002:1000"
	}

	env := append([]Env(nil), in.LaunchEnv...)
	if rt.Translate != nil {
		env = rt.Translate(env)
	}
	if rt.PathPrepend {
		env = append(env, Env{Key: "PATH", Value: workdir + ":$PATH"})
	}
	sort.Slice(env, func(i, j int) bool { return env[i].Key < env[j].Key })

	var b bytes.Buffer
	err = tpl.Execute(&b, struct {
		Version    string
		Image      *imageinfo.Image
		Runtime    runtime
		Builder    string
		Entrypoint string
		Workdir    string
		User       string
		Env        []Env
		Processes  []processLine
	}{
		Version:    Version,
		Image:      in.Image,
		Runtime:    rt,
		Builder:    in.Builder,
		Entrypoint: string(entrypoint),
		Workdir:    workdir,
		User:       user,
		Env:        env,
		Processes:  processLines(in.Image),
	})
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// entrypointFor mirrors the CNB process: exec-form becomes ENTRYPOINT argv,
// shell-form becomes ["/bin/<shell>","-c",command] (CNB defaults to bash).
func entrypointFor(p *imageinfo.Process, rt runtime) []string {
	command := p.Command
	if rt.NormalizeCommand != nil {
		command = rt.NormalizeCommand(command)
	}
	argv := append([]string{command}, p.Args...)
	if p.Direct {
		return argv
	}
	shell := p.Shell
	if shell == "" {
		shell = "bash"
	}
	return []string{"/bin/" + shell, "-c", strings.Join(argv, " ")}
}

// nodePrefix is where the build stage installs the official Node distribution
// (/usr/local in the official image), i.e. the generated image's NODE_HOME.
const nodePrefix = "/usr/local"

// translateNodePaths rewrites values that point inside the CNB node layer
// (/layers/paketo-buildpacks_node-engine/node) to the generated image's Node
// installation. NODE_HOME itself becomes /usr/local.
func translateNodePaths(env []Env) []Env {
	home := ""
	for _, e := range env {
		if e.Key == "NODE_HOME" {
			home = e.Value
		}
	}
	if home == "" {
		return env
	}
	for i, e := range env {
		switch {
		case e.Value == home:
			env[i].Value = nodePrefix
		case strings.HasPrefix(e.Value, home+"/"):
			env[i].Value = nodePrefix + strings.TrimPrefix(e.Value, home)
		}
	}
	return env
}

// pythonSitePackages matches the pip-install layer path in PYTHONPATH.
var pythonSitePackages = regexp.MustCompile(`/layers/[^:]*/site-packages`)

// translatePythonPaths points PYTHONPATH at the official image layout:
// the CPython layer becomes /usr/local and its site-packages becomes the
// default site-packages promoted by the build stage's pip install.
func translatePythonPaths(env []Env, version string) []Env {
	site := "/usr/local/lib/python" + majorMinor(version) + "/site-packages"
	for i, e := range env {
		v := strings.ReplaceAll(e.Value, "/layers/paketo-buildpacks_cpython/cpython", "/usr/local")
		env[i].Value = pythonSitePackages.ReplaceAllString(v, site)
	}
	return env
}

// majorMinor turns "3.11.18" into "3.11"; other versions pass through.
func majorMinor(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

type processLine struct {
	Type    string
	Command string
	Example string
	Default bool
}

func processLines(img *imageinfo.Image) []processLine {
	out := make([]processLine, 0, len(img.Processes))
	for _, p := range img.Processes {
		cmd := p.Command
		if strings.HasPrefix(cmd, "/layers/") {
			cmd = filepath.Base(cmd)
		}
		argv := append([]string{cmd}, p.Args...)
		var example string
		if p.Direct {
			example = "docker run --rm -it --entrypoint " + cmd + " " + img.Name
			if len(p.Args) > 0 {
				example += " " + strings.Join(p.Args, " ")
			}
		} else {
			shell := p.Shell
			if shell == "" {
				shell = "bash"
			}
			example = "docker run --rm -it --entrypoint /bin/" + shell + " " + img.Name + " -c \"" + strings.Join(argv, " ") + "\""
		}
		out = append(out, processLine{
			Type:    p.Type,
			Command: strings.Join(argv, " "),
			Example: example,
			Default: p.Default,
		})
	}
	return out
}

func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

var tpl = template.Must(template.New("dockerfile").Funcs(template.FuncMap{"q": quote}).Parse(`# Generated by p2df (pack2dockerfile) {{.Version}} — review before use.
# Original image: {{.Image.Name}} {{.Image.ImageID}}
# Builder: {{if .Builder}}{{.Builder}}{{else}}not recorded in the image (pass --builder to annotate){{end}}
# Buildpacks:{{range .Image.Buildpacks}} {{.ID}} {{.Version}};{{end}}
# Runtime: {{.Runtime.BuildImage}} (from image SBOM)
# Stack: {{.Image.Stack}}
#
# Processes:
{{- range .Processes}}
#   {{.Type}}{{if .Default}} (default){{end}}: {{.Command}}
{{- end}}
#
# Run another process (this Dockerfile only restores the default one):
{{- range .Processes}}
#   {{.Example}}
{{- end}}
#
{{- range .Runtime.Note}}
# {{.}}
{{- end}}
FROM {{.Runtime.BuildImage}} AS build
WORKDIR {{.Workdir}}
COPY . .
{{- range .Runtime.Setup}}
{{.}}
{{- end}}

FROM {{.Image.RunImage}}
USER {{.User}}
WORKDIR {{.Workdir}}
COPY --from=build --chown={{.User}} {{.Workdir}} {{.Workdir}}
{{- range .Runtime.Copy}}
COPY --from=build {{.}} {{.}}
{{- end}}
{{- range .Env}}
ENV {{.Key}}={{q .Value}}
{{- end}}
# The run image ships its own CMD (/bin/bash); clear it so the entrypoint gets no extra args.
CMD []
ENTRYPOINT {{.Entrypoint}}
`))
