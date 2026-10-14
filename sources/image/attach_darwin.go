//go:build darwin

package image

import "context"

// attach attaches the disk image at path read-only with hdiutil and classifies
// its mounted volumes. The caller must call detach on the returned attachment.
func attach(ctx context.Context, path string, format Format) (*attachment, error) {
	return attachWith(ctx, execRunner{}, path, format)
}
