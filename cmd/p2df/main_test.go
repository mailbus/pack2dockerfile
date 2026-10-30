package main

import (
	"errors"
	"testing"

	"github.com/mailbus/pack2dockerfile/internal/imageinfo"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"unsupported image", &imageinfo.UnsupportedError{}, 2},
		{"ambiguous image", &imageinfo.AmbiguousError{}, 2},
		{"other error", errors.New("boom"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err); got != tc.want {
				t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
