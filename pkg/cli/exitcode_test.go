package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, ExitOK},
		{"partial collection", ErrPartialCollection, ExitPartial},
		{"wrapped partial collection", fmt.Errorf("collect: %w", ErrPartialCollection), ExitPartial},
		{"license refused", fmt.Errorf("%w: license expired", ErrLicense), ExitLicense},
		{"fatal error", errors.New("--output is required"), ExitFailure},
		{"cancelled", context.Canceled, ExitFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
