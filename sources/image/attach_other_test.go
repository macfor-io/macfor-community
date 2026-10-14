//go:build !darwin

package image

import (
	"context"
	"errors"
	"testing"
)

func TestAttachUnsupported(t *testing.T) {
	if _, err := attach(context.Background(), "/e/x.dmg", FormatDMG); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("err = %v, want ErrUnsupportedPlatform", err)
	}
}
