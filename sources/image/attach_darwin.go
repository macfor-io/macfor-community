//go:build darwin

package image

import (
	"context"
	"os"
	"os/exec"
)

// attach attaches the disk image at path read-only with hdiutil and classifies
// its mounted volumes. The caller must call detach on the returned attachment.
func attach(ctx context.Context, path string, format Format) (*attachment, error) {
	return attachWith(ctx, execRunner{}, path, format)
}

// attachEWF mounts the E01/Ex01 image at path with the ewfmount found on PATH
// and attaches the exposed raw device with hdiutil. The caller must call detach
// on the returned attachment, which also unmounts the ewf mount.
func attachEWF(ctx context.Context, path string) (*attachment, error) {
	return attachEWFWith(ctx, defaultEWFTools(), path)
}

// defaultEWFTools returns the production tools backed by os/exec and os.
func defaultEWFTools() ewfTools {
	return ewfTools{
		runner:    execRunner{},
		lookPath:  exec.LookPath,
		removeAll: os.RemoveAll,
	}
}
