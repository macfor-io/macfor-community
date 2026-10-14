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

func TestAttachEWFUnsupported(t *testing.T) {
	if _, err := attachImage(context.Background(), "/e/x.E01", FormatE01); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("err = %v, want ErrUnsupportedPlatform", err)
	}
}
