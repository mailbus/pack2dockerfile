// Package imageinfo turns a built Cloud Native Buildpacks image into the
// model that templates and the behavior-equivalence harness consume.
package imageinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes an external command and returns its stdout.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs real commands on this machine.
type ExecRunner struct{}

// Output implements Runner via os/exec.
func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// Process is one named launch entry from the image metadata.
type Process struct {
	Type       string   `json:"type"`
	Command    string   `json:"command"`
	Args       []string `json:"args,omitempty"`
	Shell      string   `json:"shell,omitempty"`
	Direct     bool     `json:"direct"`
	Default    bool     `json:"default"`
	WorkingDir string   `json:"working_dir,omitempty"`
}

// Buildpack is one buildpack recorded in the image metadata.
type Buildpack struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

// Image is the normalized view of a built CNB image.
type Image struct {
	Name           string      `json:"name"`
	ImageID        string      `json:"image_id,omitempty"`
	Stack          string      `json:"stack,omitempty"`
	RunImage       string      `json:"run_image,omitempty"`
	User           string      `json:"user,omitempty"`
	WorkingDir     string      `json:"working_dir,omitempty"`
	Processes      []Process   `json:"processes"`
	DefaultProcess *Process    `json:"default_process,omitempty"`
	Buildpacks     []Buildpack `json:"buildpacks"`
	Languages      []string    `json:"languages"`
}

// UnsupportedError reports an image whose buildpacks match no known language.
type UnsupportedError struct {
	Buildpacks []string
}

func (e *UnsupportedError) Error() string {
	found := "none"
	if len(e.Buildpacks) > 0 {
		found = strings.Join(e.Buildpacks, ", ")
	}
	return fmt.Sprintf("unsupported image: no known language buildpack found (buildpacks: %s)", found)
}

// AmbiguousError reports an image containing more than one language family.
type AmbiguousError struct {
	Languages []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("ambiguous image: multiple language families detected (%s); p2df expects a single-language image", strings.Join(e.Languages, ", "))
}

// Load reads image metadata and returns the model. pack inspect-image is used
// when it works; the image labels are the fallback and the real requirement is
// that the image exists locally (docker image inspect).
func Load(ctx context.Context, r Runner, image string) (*Image, error) {
	image = withDefaultTag(image)

	// pack inspect-image also contacts the registry even for local images and
	// aborts when it is unreachable. Everything we need is in the image labels,
	// so treat pack as best-effort and synthesize the rest below.
	var inspect packInspect
	rawInspect, err := r.Output(ctx, "pack", "inspect-image", image, "--output", "json")
	if err == nil {
		if err := json.Unmarshal(rawInspect, &inspect); err != nil {
			return nil, fmt.Errorf("parse pack inspect output: %w", err)
		}
	}

	rawDocker, err := r.Output(ctx, "docker", "image", "inspect", image)
	if err != nil {
		return nil, fmt.Errorf("docker image inspect %s: %w", image, err)
	}
	var dockerImgs []dockerImage
	if err := json.Unmarshal(rawDocker, &dockerImgs); err != nil {
		return nil, fmt.Errorf("parse docker image inspect output: %w", err)
	}
	dockerImg := dockerImage{}
	if len(dockerImgs) > 0 {
		dockerImg = dockerImgs[0]
	}

	buildMeta, err := parseLabel[buildMetadata](dockerImg.Config.Labels[LabelBuildMetadata])
	if err != nil {
		return nil, fmt.Errorf("parse %s label: %w", LabelBuildMetadata, err)
	}
	lifecycleMeta, err := parseLabel[lifecycleMetadata](dockerImg.Config.Labels[LabelLifecycleMetadata])
	if err != nil {
		return nil, fmt.Errorf("parse %s label: %w", LabelLifecycleMetadata, err)
	}

	info := inspect.Local
	if info == nil {
		info = inspect.Remote
	}
	if info == nil {
		info = &packInfo{}
	}
	if info.Stack == "" {
		info.Stack = dockerImg.Config.Labels["io.buildpacks.stack.id"]
	}
	name := inspect.ImageName
	if name == "" {
		name = image
	}

	buildpacks := info.Buildpacks
	if len(buildpacks) == 0 && buildMeta != nil {
		buildpacks = buildMeta.Buildpacks
	}
	ids := make([]string, len(buildpacks))
	for i, bp := range buildpacks {
		ids[i] = bp.ID
	}
	languages := detectLanguages(ids)
	if len(languages) == 0 {
		return nil, &UnsupportedError{Buildpacks: ids}
	}
	if len(languages) > 1 {
		return nil, &AmbiguousError{Languages: languages}
	}

	processes := processesFromInspect(info)
	if len(processes) == 0 && buildMeta != nil {
		processes = processesFromBuildMetadata(buildMeta)
	}

	return &Image{
		Name:           name,
		ImageID:        dockerImg.ID,
		Stack:          info.Stack,
		RunImage:       runImageFrom(info, lifecycleMeta),
		User:           dockerImg.Config.User,
		WorkingDir:     dockerImg.Config.WorkingDir,
		Processes:      processes,
		DefaultProcess: defaultProcess(processes),
		Buildpacks:     buildpacks,
		Languages:      languages,
	}, nil
}

// withDefaultTag appends :latest when the reference has no tag or digest.
// pack's local lookup needs a full reference on Docker Desktop; docker itself
// accepts the short form, so normalize once for every command.
func withDefaultTag(image string) string {
	if strings.Contains(image, "@") {
		return image
	}
	lastSlash := strings.LastIndex(image, "/")
	if strings.Contains(image[lastSlash+1:], ":") {
		return image
	}
	return image + ":latest"
}

// LabelBuildMetadata is the CNB label carrying buildpack and process metadata.
const LabelBuildMetadata = "io.buildpacks.build.metadata"

// LabelLifecycleMetadata is the CNB label carrying lifecycle and run image metadata.
const LabelLifecycleMetadata = "io.buildpacks.lifecycle.metadata"

func parseLabel[T any](raw string) (*T, error) {
	if raw == "" {
		return nil, nil
	}
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func runImageFrom(info *packInfo, meta *lifecycleMetadata) string {
	if meta != nil {
		if meta.RunImage != nil && meta.RunImage.Image != "" {
			return meta.RunImage.Image
		}
		if meta.Stack != nil && meta.Stack.RunImage.Image != "" {
			return meta.Stack.RunImage.Image
		}
	}
	if len(info.RunImages) > 0 {
		return info.RunImages[0].Name
	}
	return ""
}

func processesFromInspect(info *packInfo) []Process {
	out := make([]Process, 0, len(info.Processes))
	for _, p := range info.Processes {
		out = append(out, Process{
			Type:       p.Type,
			Command:    p.Command,
			Args:       p.Args,
			Shell:      p.Shell,
			Direct:     p.Shell == "",
			Default:    p.Default,
			WorkingDir: p.WorkingDir,
		})
	}
	return out
}

func processesFromBuildMetadata(meta *buildMetadata) []Process {
	out := make([]Process, 0, len(meta.Processes))
	for _, p := range meta.Processes {
		cmd := ""
		var args []string
		if len(p.Command) > 0 {
			cmd = p.Command[0]
			args = append(args, p.Command[1:]...)
		}
		args = append(args, p.Args...)
		out = append(out, Process{
			Type:       p.Type,
			Command:    cmd,
			Args:       args,
			Direct:     p.Direct,
			Default:    p.Type == meta.DefaultProcessType,
			WorkingDir: p.WorkingDir,
		})
	}
	return out
}

func defaultProcess(processes []Process) *Process {
	for i := range processes {
		if processes[i].Default {
			return &processes[i]
		}
	}
	for i := range processes {
		if processes[i].Type == "web" {
			return &processes[i]
		}
	}
	if len(processes) > 0 {
		return &processes[0]
	}
	return nil
}

// packInspect mirrors `pack inspect-image --output json`.
type packInspect struct {
	ImageName string    `json:"image_name"`
	Local     *packInfo `json:"local_info"`
	Remote    *packInfo `json:"remote_info"`
}

type packInfo struct {
	Stack      string      `json:"stack"`
	RunImages  []runImage  `json:"run_images"`
	Buildpacks []Buildpack `json:"buildpacks"`
	Processes  []struct {
		Type       string   `json:"type"`
		Shell      string   `json:"shell"`
		Command    string   `json:"command"`
		Default    bool     `json:"default"`
		Args       []string `json:"args"`
		WorkingDir string   `json:"working-dir"`
	} `json:"processes"`
}

type runImage struct {
	Name string `json:"name"`
}

// buildMetadata mirrors the io.buildpacks.build.metadata label.
type buildMetadata struct {
	Processes []struct {
		Type       string   `json:"type"`
		Command    []string `json:"command"`
		Args       []string `json:"args"`
		Direct     bool     `json:"direct"`
		WorkingDir string   `json:"working-dir"`
	} `json:"processes"`
	Buildpacks         []Buildpack `json:"buildpacks"`
	DefaultProcessType string      `json:"buildpack-default-process-type"`
}

// lifecycleMetadata mirrors the io.buildpacks.lifecycle.metadata label.
type lifecycleMetadata struct {
	Buildpacks []struct {
		Key     string `json:"key"`
		Version string `json:"version"`
	} `json:"buildpacks"`
	RunImage *struct {
		Image     string   `json:"image"`
		Mirrors   []string `json:"mirrors"`
		TopLayer  string   `json:"topLayer"`
		Reference string   `json:"reference"`
	} `json:"runImage"`
	// Deprecated pre-Platform-API-0.9 fallback.
	Stack *struct {
		RunImage struct {
			Image string `json:"image"`
		} `json:"runImage"`
	} `json:"stack"`
}

type dockerImage struct {
	ID     string `json:"Id"`
	Config struct {
		User       string            `json:"User"`
		Env        []string          `json:"Env"`
		WorkingDir string            `json:"WorkingDir"`
		Labels     map[string]string `json:"Labels"`
	} `json:"Config"`
}
