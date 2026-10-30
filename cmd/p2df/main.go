package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

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
	var output string
	cmd := &cobra.Command{
		Use:           "p2df <image>",
		Short:         "Turn a Cloud Native Buildpacks image into a Dockerfile",
		Long:          "p2df reads a built Cloud Native Buildpacks (CNB) image and emits a human-readable Dockerfile.\nDockerfile is the artifact, not the source of truth.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			img, err := imageinfo.Load(cmd.Context(), imageinfo.ExecRunner{}, args[0])
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(img, "", "  ")
			if err != nil {
				return err
			}
			data = append(data, '\n')
			if output != "" {
				return os.WriteFile(output, data, 0o644)
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the output to FILE instead of stdout")
	return cmd
}
