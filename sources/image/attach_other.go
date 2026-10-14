//go:build !darwin

package image

import (
	"context"
	"fmt"
)

// attach always fails off macOS, because hdiutil is unavailable there.
func attach(_ context.Context, path string, _ Format) (*attachment, error) {
	return nil, fmt.Errorf("attach %s: %w", path, ErrUnsupportedPlatform)
}

// attachEWF always fails off macOS, because the raw device ewfmount exposes is
// attached with hdiutil.
func attachEWF(_ context.Context, path string) (*attachment, error) {
	return nil, fmt.Errorf("attach %s: %w", path, ErrUnsupportedPlatform)
}
