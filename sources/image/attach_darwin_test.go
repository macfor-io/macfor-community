//go:build darwin

package image

import (
	"context"
	"errors"
	"testing"
)

// TestAttachCancelled exercises the production entry point without running a
// real hdiutil attach: a done context stops before any command runs.
func TestAttachCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := attach(ctx, "/nonexistent.dmg", FormatDMG); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
