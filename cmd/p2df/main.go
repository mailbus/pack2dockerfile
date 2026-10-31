package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mailbus/pack2dockerfile/internal/dockerfile"
	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "p2df:", err)
		os.Exit(exitCode(err))
	}
}

// exitCode maps errors to CLI exit codes: 2 for unsupported/ambiguous images.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var unsupported *imageinfo.UnsupportedError
	var ambiguous *imageinfo.AmbiguousError
	if errors.As(err, &unsupported) || errors.As(err, &ambiguous) {
		return 2
	}
	return 1
}

func newRootCmd() *cobra.Command {
	var output, builder string
	cmd := &cobra.Command{
		Use:           "p2df <image>",
		Short:         "Turn a Cloud Native Buildpacks image into a Dockerfile",
		Long:          "p2df reads a built Cloud Native Buildpacks (CNB) image and emits a human-readable Dockerfile.\nDockerfile is the artifact, not the source of truth.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			runner := imageinfo.ExecRunner{}
			image := args[0]

			img, err := imageinfo.Load(ctx, runner, image)
			if err != nil {
				return err
			}
			if len(img.Languages) != 1 {
				return fmt.Errorf("no template for language %v yet", img.Languages)
			}
			var runtimeVersion string
			switch img.Languages[0] {
			case "node":
				runtimeVersion, err = imageinfo.NodeEngineVersion(ctx, runner, image)
			case "python":
				runtimeVersion, err = imageinfo.CPythonVersion(ctx, runner, image)
			default:
				return fmt.Errorf("no template for language %q yet", img.Languages[0])
			}
			if err != nil {
				return err
			}
			launchEnv, err := dockerfile.ReadLaunchEnv(ctx, runner, image)
			if err != nil {
				return err
			}
			out, err := dockerfile.Render(dockerfile.Input{
				Image:          img,
				RuntimeVersion: runtimeVersion,
				LaunchEnv:      launchEnv,
				Builder:        builder,
			})
			if err != nil {
				return err
			}
			if output != "" {
				return os.WriteFile(output, out, 0o644)
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the Dockerfile to FILE instead of stdout")
	cmd.Flags().StringVar(&builder, "builder", "", "builder name to annotate in the header (not recorded in the image)")
	return cmd
}
